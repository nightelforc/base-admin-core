// Package server wires the HTTP layer: routing, middleware order and the
// lifecycle of the underlying http.Server.
package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/config"
	"github.com/nightelforc/base-admin-core/backend/internal/handler"
	"github.com/nightelforc/base-admin-core/backend/internal/middleware"
	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/pkg/redis"
)

// Dependencies is everything the router needs.
type Dependencies struct {
	Config        *config.Config
	Log           *slog.Logger
	DB            *gorm.DB
	Cache         *goredis.Client
	Handler       *handler.Handler
	Authenticator *middleware.Authenticator
	Operations    *middleware.OperationLogger
}

// NewRouter builds the gin engine with the documented middleware order:
// request id, access log, recovery, CORS, body limit, then per-group auth.
func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(ginMode(deps.Config.App.Env))
	gin.DefaultWriter = &logWriter{log: deps.Log}
	gin.DefaultErrorWriter = &logWriter{log: deps.Log, errorLevel: true}

	router := gin.New()
	// An empty list means "trust no proxy", so ClientIP() reports the socket
	// address unless a deployment explicitly names its load balancers.
	_ = router.SetTrustedProxies(deps.Config.HTTP.TrustedProxies)

	router.Use(
		middleware.RequestIDMiddleware(),
		middleware.AccessLog(deps.Log),
		middleware.Recovery(),
		middleware.CORS(deps.Config.CORS),
		middleware.BodyLimit(deps.Config.HTTP.MaxBodyBytes),
	)

	router.GET("/healthz", deps.Handler.Health)
	router.GET("/readyz", handler.Readiness(deps.DB, redis.NewHealthChecker(deps.Cache)))

	api := router.Group("/api/v1")

	// Public endpoints.
	api.POST("/auth/login", deps.Handler.Login)
	api.POST("/auth/refresh", deps.Handler.Refresh)

	// Everything below requires a valid access token.
	secured := api.Group("")
	secured.Use(deps.Authenticator.Middleware(), deps.Operations.Middleware())

	secured.POST("/auth/logout", deps.Handler.Logout)
	secured.GET("/auth/profile", deps.Handler.Profile)
	secured.PUT("/auth/profile", deps.Handler.UpdateProfile)
	secured.PUT("/auth/password", deps.Handler.ChangePassword)
	secured.GET("/auth/codes", deps.Handler.AccessCodes)
	secured.GET("/auth/menus", deps.Handler.RouteMenus)

	secured.GET("/dashboard/overview", deps.Handler.DashboardOverview)

	users := secured.Group("/users")
	{
		users.GET("", middleware.RequirePermission(model.PermUserList), deps.Handler.ListUsers)
		users.POST("", middleware.RequirePermission(model.PermUserCreate), deps.Handler.CreateUser)
		users.PUT("/:id", middleware.RequirePermission(model.PermUserUpdate), deps.Handler.UpdateUser)
		users.DELETE("/:id", middleware.RequirePermission(model.PermUserDelete), deps.Handler.DeleteUser)
		users.PUT("/:id/password", middleware.RequirePermission(model.PermUserResetPassword), deps.Handler.ResetUserPassword)
		users.PUT("/:id/status", middleware.RequirePermission(model.PermUserUpdate), deps.Handler.SetUserStatus)
	}

	roles := secured.Group("/roles")
	{
		roles.GET("", middleware.RequirePermission(model.PermRoleList), deps.Handler.ListRoles)
		// Options feeds the user form, so it only needs the user permissions.
		roles.GET("/options", middleware.RequirePermission(model.PermUserList), deps.Handler.RoleOptions)
		roles.POST("", middleware.RequirePermission(model.PermRoleCreate), deps.Handler.CreateRole)
		roles.PUT("/:id", middleware.RequirePermission(model.PermRoleUpdate), deps.Handler.UpdateRole)
		roles.DELETE("/:id", middleware.RequirePermission(model.PermRoleDelete), deps.Handler.DeleteRole)
		roles.GET("/:id/menus", middleware.RequirePermission(model.PermRoleAssign), deps.Handler.RoleMenus)
		roles.PUT("/:id/menus", middleware.RequirePermission(model.PermRoleAssign), deps.Handler.AssignRoleMenus)
	}

	menus := secured.Group("/menus")
	{
		menus.GET("/tree", middleware.RequirePermission(model.PermMenuList), deps.Handler.MenuTree)
		menus.POST("", middleware.RequirePermission(model.PermMenuCreate), deps.Handler.CreateMenu)
		menus.PUT("/:id", middleware.RequirePermission(model.PermMenuUpdate), deps.Handler.UpdateMenu)
		menus.DELETE("/:id", middleware.RequirePermission(model.PermMenuDelete), deps.Handler.DeleteMenu)
	}

	logs := secured.Group("/logs", middleware.RequirePermission(model.PermLogList))
	{
		logs.GET("/login", deps.Handler.ListLoginLogs)
		logs.GET("/operation", deps.Handler.ListOperationLogs)
	}

	return router
}

func ginMode(env string) string {
	switch env {
	case config.EnvProduction:
		return gin.ReleaseMode
	case config.EnvTest:
		return gin.TestMode
	default:
		return gin.DebugMode
	}
}

// logWriter forwards gin's own diagnostics (route dumps, panics) to slog so
// every line of the process reaches the same sink in the same format.
type logWriter struct {
	log        *slog.Logger
	errorLevel bool
}

func (w *logWriter) Write(p []byte) (int, error) {
	message := string(p)
	if w.errorLevel {
		w.log.Error(trimNewline(message))
	} else {
		w.log.Debug(trimNewline(message))
	}
	return len(p), nil
}

func trimNewline(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}
