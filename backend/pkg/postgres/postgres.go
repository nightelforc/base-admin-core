// Package postgres 打开应用使用的 PostgreSQL 连接。
//
// 本包负责连接生命周期、连接池调优以及 GORM 的命名策略；调用方只看到 *gorm.DB。
// 配置以 Options 传入，而不是从应用的 config 包读取，因此本包保持可复用。
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
)

// Options 描述一个 PostgreSQL 连接。
type Options struct {
	// DSN 是交给 pgx 的连接串。
	DSN string
	// TablePrefix 由 GORM 的命名策略应用到每个模型。
	TablePrefix string
	// MaxOpenConns / MaxIdleConns 限制连接池。
	MaxOpenConns int
	MaxIdleConns int
	// ConnMaxLifetime / ConnMaxIdleTime 回收连接池中的连接。
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	// LogLevel 取 silent、error、warn、info 之一（GORM 语句日志）。
	LogLevel string
	// SlowQueryThreshold 把慢于该阈值的语句标为警告。
	SlowQueryThreshold time.Duration
	// Logger 接收 GORM 的语句日志；为 nil 时回落到 slog.Default。
	Logger *slog.Logger
}

// Open 通过 GORM 连接 PostgreSQL，并应用连接池限制、表前缀以及基于 slog 的语句日志器。
func Open(ctx context.Context, opts Options) (*gorm.DB, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	gormDB, err := gorm.Open(postgres.Open(opts.DSN), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: opts.TablePrefix,
		},
		Logger: logger.NewGormLogger(log, opts.LogLevel, opts.SlowQueryThreshold),
		// 不开启 PrepareStmt：pgx 的 stdlib 驱动默认以 cache_statement 模式自动
		// prepare 并缓存语句，执行时仍会走这层缓存；再叠一层 database/sql 的语句
		// 缓存，只会让同一条 SQL 在每个连接上被 prepare 两次（stmt_<hash> 与
		// stmtcache_<hash>），往返次数却不变。要接 PgBouncer 事务模式，该改的是
		// 连接串里的 default_query_exec_mode，而不是这个开关。
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("access postgres pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(opts.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logConnectionCapacity(pingCtx, sqlDB, log, opts)

	return gormDB, nil
}

// logConnectionCapacity 记录单进程连接池与服务器上限，便于按副本数规划容量。
// 它只读取当前会话可见的 GUC，不改变任何服务器设置。
func logConnectionCapacity(ctx context.Context, sqlDB *sql.DB, log *slog.Logger, opts Options) {
	var serverMax, reserved int
	err := sqlDB.QueryRowContext(ctx,
		"SELECT current_setting('max_connections')::int, "+
			"current_setting('superuser_reserved_connections')::int",
	).Scan(&serverMax, &reserved)
	if err != nil {
		log.Debug("read postgres connection limits", "error", err)
		return
	}

	available := serverMax - reserved
	log.Info("postgres pool",
		"max_open", opts.MaxOpenConns,
		"max_idle", opts.MaxIdleConns,
		"server_max_connections", serverMax,
		"server_reserved", reserved,
		"server_available", available,
	)
	if opts.MaxOpenConns > available {
		log.Warn("postgres pool alone can exhaust server connections",
			"max_open", opts.MaxOpenConns,
			"server_available", available,
		)
	}
}

// Close 释放连接池。
func Close(gormDB *gorm.DB) error {
	if gormDB == nil {
		return nil
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SQLDB 暴露底层 *sql.DB（供健康检查使用）。
func SQLDB(gormDB *gorm.DB) (*sql.DB, error) {
	if gormDB == nil {
		return nil, fmt.Errorf("database handle is nil")
	}
	return gormDB.DB()
}
