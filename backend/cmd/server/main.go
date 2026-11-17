// Command server 启动管理后台 API 服务。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/bootstrap"
	"github.com/nightelforc/base-admin-core/backend/internal/config"
	"github.com/nightelforc/base-admin-core/backend/internal/handler"
	"github.com/nightelforc/base-admin-core/backend/internal/middleware"
	"github.com/nightelforc/base-admin-core/backend/internal/repository"
	"github.com/nightelforc/base-admin-core/backend/internal/server"
	"github.com/nightelforc/base-admin-core/backend/internal/service"
	"github.com/nightelforc/base-admin-core/backend/internal/throttle"
	"github.com/nightelforc/base-admin-core/backend/migrations"
	"github.com/nightelforc/base-admin-core/backend/pkg/jwt"
	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
	"github.com/nightelforc/base-admin-core/backend/pkg/migrate"
	"github.com/nightelforc/base-admin-core/backend/pkg/password"
	"github.com/nightelforc/base-admin-core/backend/pkg/postgres"
	"github.com/nightelforc/base-admin-core/backend/pkg/redis"
	"github.com/nightelforc/base-admin-core/backend/pkg/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// run 是整个进程的装配流程，严格按依赖顺序执行，任何一步失败都立即返回：
//
//  1. 配置   环境变量 → config.Config，此时还没有日志，错误只能由 main 打到 stderr
//  2. 日志   全进程唯一的 *slog.Logger，之后所有组件共用它
//  3. 信号   可取消的 ctx，SIGINT/SIGTERM 触发优雅关闭
//  4. 数据库 连接池 + GORM 语句日志，所有仓储的基础
//  5. Redis  缓存、会话、登录限流共用同一个客户端（连接池只有一份）
//  6. 迁移   建表改表必须早于任何仓储查询
//  7. 装配   仓储 → 基础设施客户端 → 领域服务 → handler，具体见 build
//  8. 路由   把装配结果交给 gin，就绪探针同时检查数据库与 Redis
//  9. 运行   阻塞直到 ctx 取消，再按 defer 栈逆序关闭 Redis、数据库
//
// 顺序不能随意调换：日志要早于一切会打日志的组件，数据库要早于仓储，仓储要早于
// 数据库连接关闭（defer 在函数返回时才执行），迁移要早于 bootstrap 和服务查询。
func run() error {
	// 1. 配置：.env 只是本地方便，真实部署直接设环境变量，这里找不到文件也无妨。
	_ = godotenv.Load(".env", "backend/.env")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 日志：先建日志，后面每一步的失败与耗时才有地方可记。
	log, err := logger.New(logger.Options{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
	})
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	log.Info("starting", "app", cfg.App.Name, "env", cfg.App.Env, "config", cfg.Summary())

	// 3. 信号：ctx 取消即开始关闭，server.Run 会在摘完流量后返回。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 4. 数据库：连接参数由 config 推导，运行时依赖（logger）在这里注入。
	dbOpts := cfg.Database.Options()
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

	// 5. Redis：会话与登录限流都从这里取，所以只建一个客户端、一个连接池。
	rdb, err := redis.Open(ctx, cfg.Redis.Options())
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if closeErr := redis.Close(rdb); closeErr != nil {
			log.Error("close redis", "error", closeErr)
		}
	}()

	// 6. 迁移：独立连接执行（pkg/migrate 自己开一次 DSN 连接），先于服务查询建好表。
	if cfg.Database.Migrate {
		applied, err := migrate.Run(ctx, migrate.Options{
			FS:     migrations.FS,
			DSN:    cfg.Database.DSN(),
			Prefix: cfg.Database.TablePrefix,
			Log:    log,
		})
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if applied > 0 {
			log.Info("migrations applied", "count", applied, "table_prefix", cfg.Database.TablePrefix)
		}
	}

	// 7. 装配：仓储、服务、handler 的构造顺序与依赖关系都在 build 里。
	app, operations, err := build(cfg, log, db, rdb)
	if err != nil {
		return err
	}

	// 8. 路由：依赖以结构体整体传入，新增组件时只改 server.Dependencies 和这里的赋值。
	api := server.NewRouter(server.Dependencies{
		Config:        cfg,
		Log:           log,
		DB:            db,
		Cache:         rdb,
		Handler:       app.handler,
		Authenticator: app.authenticator,
		Operations:    operations,
	})

	// 9. 运行：阻塞至收到信号；返回后 run 的 defer 依次关闭 Redis 和数据库。
	return server.New(cfg, api, log, operations).Run(ctx)
}

// application 聚合装配好的最外层依赖，让 run 不必逐个持有它们。
type application struct {
	handler       *handler.Handler
	authenticator *middleware.Authenticator
}

// build 装配业务层，顺序即依赖方向，下层不反过来依赖上层：
//
//	仓储层    Base 提供查询构造辅助，四个仓储共用它
//	基础设施  password / jwt / session / throttle，参数全部来自配置
//	引导数据  bootstrap 创建首个管理员，必须早于任何登录请求
//	领域服务  每个 New* 只接收自己真正用到的协作者，签名即依赖清单
//	最外层    handler 聚合全部服务；authenticator 只需令牌、会话、用户仓储
//
// operations 单独返回而不放进 application：它挂在路由上，与 handler 平级，
// 且 server.New 与 NewRouter 都要用。
func build(cfg *config.Config, log *slog.Logger, db *gorm.DB, rdb *goredis.Client) (*application, *middleware.OperationLogger, error) {
	// 仓储层：共用一个 Base，而不是每个仓储各自持有 *gorm.DB。
	base := repository.NewBase(db)

	users := repository.NewUserRepository(base)
	roles := repository.NewRoleRepository(base)
	menus := repository.NewMenuRepository(base)
	logs := repository.NewLogRepository(base)

	// 基础设施客户端：与数据库无关，密码哈希、令牌签发、会话、登录限流。
	hasher := password.NewHasher(password.Options{
		Cost:      cfg.Security.BcryptCost,
		MinLength: cfg.Security.PasswordMinLength,
	})
	tokens := jwt.NewManager(jwt.Options{
		Secret:     cfg.JWT.Secret,
		Issuer:     cfg.JWT.Issuer,
		Audience:   cfg.JWT.Audience,
		AccessTTL:  cfg.JWT.AccessTTL,
		RefreshTTL: cfg.JWT.RefreshTTL,
	})
	sessions := session.NewStore(rdb, cfg.Redis.KeyPrefix, cfg.JWT.RefreshTTL)
	loginThrottle := throttle.NewLimiter(rdb, cfg.Redis.KeyPrefix, cfg.Security.LoginMaxAttempts, cfg.Security.LoginWindow, cfg.Security.LoginBlockDuration)

	// 引导数据：首个管理员账号，服务对外提供登录能力之前必须就绪。
	if err := bootstrap.Run(context.Background(), bootstrap.Deps{
		Users:  users,
		Roles:  roles,
		Hasher: hasher,
		Config: cfg.Bootstrap,
		Log:    log,
	}); err != nil {
		return nil, nil, fmt.Errorf("bootstrap: %w", err)
	}

	// 领域服务：构造函数的参数就是该服务实际用到的依赖，超出范围的一律不传。
	authService := service.NewAuthService(service.AuthDeps{
		Users:    users,
		Logs:     logs,
		Hasher:   hasher,
		Tokens:   tokens,
		Sessions: sessions,
		Throttle: loginThrottle,
		Config:   cfg.Security,
	})
	userService := service.NewUserService(users, roles, hasher, sessions)
	roleService := service.NewRoleService(roles, users, menus)
	menuService := service.NewMenuService(menus, users)
	logService := service.NewLogService(logs)
	dashboardService := service.NewDashboardService(users, roles, menus, logs, cfg.Location())

	// 最外层：操作日志中间件与 handler 平级，都只依赖服务层，不直接碰数据库。
	operations := middleware.NewOperationLogger(logs, log, cfg.Security.OperationLogEnabled, cfg.Security.OperationLogLimit)

	return &application{
		handler:       handler.New(authService, userService, roleService, menuService, logService, dashboardService, log),
		authenticator: middleware.NewAuthenticator(tokens, sessions, users),
	}, operations, nil
}
