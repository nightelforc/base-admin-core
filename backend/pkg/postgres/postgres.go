// Package postgres opens the PostgreSQL connection used by the application.
//
// The package owns connection lifetime, pool tuning and GORM's naming strategy;
// callers only see a *gorm.DB. Configuration arrives as Options instead of being
// read from the application's config package, so the package stays reusable.
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

	"github.com/base-admin/base-admin-core/backend/pkg/logger"
)

// Options describes a PostgreSQL connection.
type Options struct {
	// DSN is the connection string handed to pgx.
	DSN string
	// TablePrefix is applied to every model by GORM's naming strategy.
	TablePrefix string
	// MaxOpenConns / MaxIdleConns bound the pool.
	MaxOpenConns int
	MaxIdleConns int
	// ConnMaxLifetime / ConnMaxIdleTime recycle pooled connections.
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	// LogLevel is one of silent, error, warn, info (GORM statement logging).
	LogLevel string
	// SlowQueryThreshold flags statements slower than this as warnings.
	SlowQueryThreshold time.Duration
	// Logger receives GORM's statement logs; nil falls back to slog.Default.
	Logger *slog.Logger
}

// Open connects to PostgreSQL through GORM, applying pool limits, the table
// prefix and the slog-backed statement logger.
func Open(ctx context.Context, opts Options) (*gorm.DB, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	gormDB, err := gorm.Open(postgres.Open(opts.DSN), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: opts.TablePrefix,
		},
		Logger:                 logger.NewGormLogger(log, opts.LogLevel, opts.SlowQueryThreshold),
		PrepareStmt:            true,
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
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return gormDB, nil
}

// Close releases the connection pool.
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

// SQLDB exposes the underlying *sql.DB (used by health checks).
func SQLDB(gormDB *gorm.DB) (*sql.DB, error) {
	if gormDB == nil {
		return nil, fmt.Errorf("database handle is nil")
	}
	return gormDB.DB()
}
