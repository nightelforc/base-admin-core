// Command server starts the admin API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/base-admin/base-admin-core/backend/internal/bootstrap"
	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/internal/handler"
	"github.com/base-admin/base-admin-core/backend/internal/logger"
	"github.com/base-admin/base-admin-core/backend/internal/middleware"
	"github.com/base-admin/base-admin-core/backend/internal/migrate"
	"github.com/base-admin/base-admin-core/backend/internal/platform"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/internal/security"
	"github.com/base-admin/base-admin-core/backend/internal/server"
	"github.com/base-admin/base-admin-core/backend/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// A .env file is a convenience for local development; real deployments set
	// real environment variables and this call simply finds no file.
	_ = godotenv.Load(".env", "backend/.env")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := logger.New(cfg.Log)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	log.Info("starting", "app", cfg.App.Name, "env", cfg.App.Env, "config", cfg.Summary())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := platform.OpenPostgres(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer func() {
		if closeErr := platform.ClosePostgres(db); closeErr != nil {
			log.Error("close postgres", "error", closeErr)
		}
	}()

	cache, err := platform.OpenRedis(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if closeErr := platform.CloseRedis(cache); closeErr != nil {
			log.Error("close redis", "error", closeErr)
		}
	}()

	if cfg.Database.Migrate {
		applied, err := migrate.Run(ctx, cfg.Database, log)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if applied > 0 {
			log.Info("migrations applied", "count", applied, "table_prefix", cfg.Database.TablePrefix)
		}
	}

	app, operations, err := build(cfg, log, db, cache)
	if err != nil {
		return err
	}

	api := server.NewRouter(server.Dependencies{
		Config:        cfg,
		Log:           log,
		DB:            db,
		Cache:         cache,
		Handler:       app.handler,
		Authenticator: app.authenticator,
		Operations:    operations,
	})

	return server.New(cfg, api, log, operations).Run(ctx)
}

// application bundles the wired dependencies so run() stays readable.
type application struct {
	handler       *handler.Handler
	authenticator *middleware.Authenticator
}

func build(cfg *config.Config, log *slog.Logger, db *gorm.DB, cache *redis.Client) (*application, *middleware.OperationLogger, error) {
	base := repository.NewBase(db)

	users := repository.NewUserRepository(base)
	roles := repository.NewRoleRepository(base)
	menus := repository.NewMenuRepository(base)
	logs := repository.NewLogRepository(base)

	hasher := security.NewPasswordHasher(cfg.Security.BcryptCost, cfg.Security.PasswordMinLength)
	tokens := security.NewTokenManager(cfg.JWT)
	sessions := security.NewSessionStore(cache, cfg.Redis.KeyPrefix, cfg.JWT.RefreshTTL)
	throttle := security.NewLoginThrottle(cache, cfg.Redis.KeyPrefix, cfg.Security.LoginMaxAttempts, cfg.Security.LoginWindow, cfg.Security.LoginBlockDuration)

	if err := bootstrap.Run(context.Background(), bootstrap.Deps{
		Users:  users,
		Roles:  roles,
		Hasher: hasher,
		Config: cfg.Bootstrap,
		Log:    log,
	}); err != nil {
		return nil, nil, fmt.Errorf("bootstrap: %w", err)
	}

	authService := service.NewAuthService(service.AuthDeps{
		Users:    users,
		Logs:     logs,
		Hasher:   hasher,
		Tokens:   tokens,
		Sessions: sessions,
		Throttle: throttle,
		Config:   cfg.Security,
	})
	userService := service.NewUserService(users, roles, hasher, sessions)
	roleService := service.NewRoleService(roles, users, menus)
	menuService := service.NewMenuService(menus, users)
	logService := service.NewLogService(logs)
	dashboardService := service.NewDashboardService(users, roles, menus, logs, cfg.Location())

	operations := middleware.NewOperationLogger(logs, log, cfg.Security.OperationLogEnabled, cfg.Security.OperationLogLimit)

	return &application{
		handler:       handler.New(authService, userService, roleService, menuService, logService, dashboardService, log),
		authenticator: middleware.NewAuthenticator(tokens, sessions, users),
	}, operations, nil
}
