// Package pg 提供 PostgreSQL 连接池的监控项。
//
// 采集的是 database/sql 在进程内的计数，不产生数据库往返；健康时输出 debug，
// 默认日志级别下只有需要关注的情况才会出现。
package pg

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
)

// Strategy 是连接池快照的判定函数。
type Strategy = core.Strategy[sql.DBStats]

// Checks 返回连接池监控项。db 为 nil 时返回空；不传策略时用 DefaultStrategies()。
//
// 收 *gorm.DB 而不是 *sql.DB：gorm → database/sql 的转换是 pg 自己的知识，
// 不该出现在 monitor 的开关表里（Redis、ES 都没有这一步）。
func Checks(db *gorm.DB, ss ...Strategy) ([]core.Check, error) {
	if db == nil {
		return nil, nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if len(ss) == 0 {
		ss = DefaultStrategies()
	}

	take := func(context.Context) (sql.DBStats, error) { return sqlDB.Stats(), nil }
	return []core.Check{core.NewPoll("postgres.pool", core.DefaultInterval, take, ss...)}, nil
}

// DefaultStrategies 是默认判定顺序：从重到轻，Healthy 兜底。
func DefaultStrategies() []Strategy {
	return []Strategy{Waiting, Exhaustion(0.8), Recycling, Healthy}
}

// Waiting 认领「本区间内有请求等过连接」。
func Waiting(prev, cur sql.DBStats) (core.Result, bool) {
	waits := core.Delta(cur.WaitCount, prev.WaitCount)
	if waits == 0 {
		return core.Result{}, false
	}

	waited := core.DurationDelta(cur.WaitDuration, prev.WaitDuration)
	return core.Result{
		Msg:   "postgres pool waiting",
		Level: slog.LevelWarn,
		Attrs: append([]any{
			"waits", waits,
			"waited", waited,
			"avg_wait", waited / time.Duration(waits),
		}, poolAttrs(cur)...),
	}, true
}

// Exhaustion 认领使用率达到 max_open 的 ratio；连接池不限量（max_open = 0）时不生效。
//
// 用除法比较而不是 ratio*max_open：0.8*15 会算出 12.000000000000002，边界上判不中。
// 1e-9 的容差吸收 2/3 这类除不尽的比率。
func Exhaustion(ratio float64) Strategy {
	return func(_ sql.DBStats, cur sql.DBStats) (core.Result, bool) {
		maxOpen := cur.MaxOpenConnections
		if maxOpen <= 0 {
			return core.Result{}, false
		}
		if float64(cur.InUse)/float64(maxOpen)+1e-9 < ratio {
			return core.Result{}, false
		}
		return core.Result{
			Msg:   "postgres pool nearly exhausted",
			Level: slog.LevelWarn,
			Attrs: poolAttrs(cur),
		}, true
	}
}

// Recycling 认领「本区间内有连接被回收」。
func Recycling(prev, cur sql.DBStats) (core.Result, bool) {
	idleClosed := core.Delta(cur.MaxIdleClosed, prev.MaxIdleClosed)
	lifetimeClosed := core.Delta(cur.MaxLifetimeClosed, prev.MaxLifetimeClosed)
	if idleClosed == 0 && lifetimeClosed == 0 {
		return core.Result{}, false
	}

	return core.Result{
		Msg:   "postgres pool recycling connections",
		Level: slog.LevelInfo,
		Attrs: append([]any{
			"idle_closed", idleClosed,
			"lifetime_closed", lifetimeClosed,
		}, poolAttrs(cur)...),
	}, true
}

// Healthy 总是认领，作为兜底输出 debug；放在默认顺序的最后。
func Healthy(_, cur sql.DBStats) (core.Result, bool) {
	return core.Result{
		Msg:   "postgres pool healthy",
		Level: slog.LevelDebug,
		Attrs: poolAttrs(cur),
	}, true
}

func poolAttrs(s sql.DBStats) []any {
	return []any{
		"open", s.OpenConnections,
		"in_use", s.InUse,
		"idle", s.Idle,
		"max_open", s.MaxOpenConnections,
	}
}
