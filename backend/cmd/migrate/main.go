// Command migrate applies the embedded SQL migrations and exits.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/migrations"
	"github.com/base-admin/base-admin-core/backend/pkg/logger"
	"github.com/base-admin/base-admin-core/backend/pkg/migrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load(".env", "backend/.env")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := logger.New(logger.Options{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
	})
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	applied, err := migrate.Run(context.Background(), migrate.Options{
		FS:     migrations.FS,
		DSN:    cfg.Database.DSN(),
		Prefix: cfg.Database.TablePrefix,
		Log:    log,
	})
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	log.Info("migrations up to date", "applied", applied, "table_prefix", cfg.Database.TablePrefix)
	return nil
}
