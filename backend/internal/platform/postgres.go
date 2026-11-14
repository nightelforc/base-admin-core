// Package platform wires the process to its infrastructure dependencies
// (PostgreSQL, Redis). It owns connection lifetimes; the rest of the code only
// sees the resulting clients.
package platform

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/internal/logger"
)

// OpenPostgres connects to PostgreSQL through GORM, applying pool limits, the
// configured table prefix and the slog-backed statement logger.
func OpenPostgres(ctx context.Context, cfg config.Database, log *slog.Logger) (*gorm.DB, error) {
	gormDB, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: cfg.TablePrefix,
		},
		Logger:                 logger.NewGormLogger(log, cfg.LogLevel, cfg.SlowQueryThreshold),
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
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return gormDB, nil
}

// ClosePostgres releases the connection pool.
func ClosePostgres(gormDB *gorm.DB) error {
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
