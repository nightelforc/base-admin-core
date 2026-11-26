// Package redis 把 Redis 连接池包成一个监控项。
//
// 它只读客户端内存里的计数，不向服务端发任何命令：采样成本是一次原子读，
// 所以可以按默认节拍（30 秒）跑，不影响主业务。
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
)

// Strategy 判定一次连接池快照。
//
// 用驱动自己的 *PoolStats 而不是自定义结构：v9 的 NewFailoverClient 也返回
// *Client，单机与哨兵拿到的是同一个类型，这里不需要为两种模式分叉。
type Strategy = core.Strategy[*goredis.PoolStats]

// Checks 把客户端装配成监控项。
//
// client 为 nil 时返回 nil：没配 Redis 就没有这一路，不算失败。不传策略时用
// DefaultStrategies。
func Checks(client *goredis.Client, ss ...Strategy) []core.Check {
	if client == nil {
		return nil
	}
	if len(ss) == 0 {
		ss = DefaultStrategies()
	}

	return []core.Check{
		core.NewPoll(
			"redis.pool",
			core.DefaultInterval,
			func(context.Context) (*goredis.PoolStats, error) {
				return client.PoolStats(), nil
			},
			poolMetrics,
			ss...,
		),
	}
}

// DefaultStrategies 是默认的判定顺序：先报真正在伤请求的信号，最后兜底。
func DefaultStrategies() []Strategy {
	return []Strategy{Timeouts, Starvation, Unusable, Healthy}
}

// Timeouts 表示区间内有请求等满了 pool_timeout。
//
// 这是最硬的信号：它说明排队已经越过了我们配的预算，请求大概率已经被拖慢。
func Timeouts(prev, cur *goredis.PoolStats) (core.Result, bool) {
	if core.Delta(int64(cur.Timeouts), int64(prev.Timeouts)) <= 0 {
		return core.Result{}, false
	}
	return core.Result{Msg: "redis pool timeout", Severity: core.SeverityWarn}, true
}

// Starvation 表示采样这一刻仍有请求在等连接。
//
// 它是瞬时值：30 秒一次的采样会漏掉短促的排队高峰，所以它回答的是"现在是否
// 还卡着"，而"卡过没有"要看 Timeouts 的增量。
func Starvation(_, cur *goredis.PoolStats) (core.Result, bool) {
	if cur.PendingRequests == 0 {
		return core.Result{}, false
	}
	return core.Result{Msg: "redis pool starving", Severity: core.SeverityWarn}, true
}

// Unusable 表示区间内取到过不可用的连接。
//
// 这些连接在取出的那一刻已经被对端关闭（或半开），请求要么重试、要么直接失败。
// 它的增量是"链路上有东西在丢连接"的直接证据，和 conn_max_idle_time 那节的
// 讨论是同一个现象。
func Unusable(prev, cur *goredis.PoolStats) (core.Result, bool) {
	if core.Delta(int64(cur.Unusable), int64(prev.Unusable)) <= 0 {
		return core.Result{}, false
	}
	return core.Result{Msg: "redis pool discarded unusable connections", Severity: core.SeverityNotice}, true
}

// Healthy 是兜底策略：前面都没认领时，这次采样就是健康的。
func Healthy(_, _ *goredis.PoolStats) (core.Result, bool) {
	return core.Result{Msg: "redis pool healthy", Severity: core.SeverityOK}, true
}

// poolMetrics 把一次快照变成指标。
//
// 累计量给区间增量（第一个采样只建基线，所以天然是 0），状态量给当前值。
// hits/misses 不产出：它们只描述快路径占比，没有可执行的结论。
func poolMetrics(prev, cur *goredis.PoolStats) []core.Metric {
	waited := core.DurationDelta(
		time.Duration(cur.WaitDurationNs),
		time.Duration(prev.WaitDurationNs),
	)

	return []core.Metric{
		{Name: "timeouts", Unit: core.UnitCount, Value: float64(core.Delta(int64(cur.Timeouts), int64(prev.Timeouts)))},
		{Name: "waits", Unit: core.UnitCount, Value: float64(core.Delta(int64(cur.WaitCount), int64(prev.WaitCount)))},
		{Name: "waited", Unit: core.UnitSeconds, Value: waited.Seconds()},
		{Name: "unusable", Unit: core.UnitCount, Value: float64(core.Delta(int64(cur.Unusable), int64(prev.Unusable)))},
		{Name: "total", Unit: core.UnitCount, Value: float64(cur.TotalConns)},
		{Name: "idle", Unit: core.UnitCount, Value: float64(cur.IdleConns)},
		{Name: "pending", Unit: core.UnitCount, Value: float64(cur.PendingRequests)},
	}
}
