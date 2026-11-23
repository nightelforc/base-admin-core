// Command migrate 执行 SQL 迁移并创建首个管理员，供集群 Job 在服务启动前调用。
//
// 应用进程（cmd/server）不再执行迁移或 bootstrap：DDL 与引导数据只在 Job 中运行，
// 因此运行期的连接池不会被迁移占用，也不会出现多副本并发迁移。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/nightelforc/base-admin-core/backend/internal/bootstrap"
	"github.com/nightelforc/base-admin-core/backend/internal/config"
	"github.com/nightelforc/base-admin-core/backend/internal/repository"
	"github.com/nightelforc/base-admin-core/backend/migrations"
	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
	"github.com/nightelforc/base-admin-core/backend/pkg/migrate"
	"github.com/nightelforc/base-admin-core/backend/pkg/password"
	"github.com/nightelforc/base-admin-core/backend/pkg/postgres"
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

	// Job 被取消时尽快退出；迁移连接断开后 advisory lock 会随之释放。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	applied, err := migrate.Run(ctx, migrate.Options{
		FS:     migrations.FS,
		DSN:    cfg.Database.DSN(),
		Prefix: cfg.Database.TablePrefix,
		Log:    log,
	})
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	log.Info("migrations up to date", "applied", applied, "table_prefix", cfg.Database.TablePrefix)

	// 引导数据：Job 只开 2 条连接，跑完即退，运行期不占任何连接。
	// 表结构已由上面的迁移建好，这里复用应用连接串执行 DML。
	dbOpts := cfg.Database.Options()
	dbOpts.MaxOpenConns = 2
	dbOpts.MaxIdleConns = 1
	dbOpts.Logger = log

	db, err := postgres.Open(ctx, dbOpts)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer func() {
		if closeErr := postgres.Close(db); closeErr != nil {
			log.Error("close postgres", "error", closeErr)
		}
	}()

	base := repository.NewBase(db)
	hasher := password.NewHasher(password.Options{
		Cost:      cfg.Security.BcryptCost,
		MinLength: cfg.Security.PasswordMinLength,
	})

	if err := bootstrap.Run(ctx, bootstrap.Deps{
		Users:  repository.NewUserRepository(base),
		Roles:  repository.NewRoleRepository(base),
		Hasher: hasher,
		Config: cfg.Bootstrap,
		Log:    log,
	}); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	log.Info("migrate job finished", "table_prefix", cfg.Database.TablePrefix)
	return nil
}
