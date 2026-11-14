// Package handler adapts HTTP requests to service calls. It owns request
// binding and response shaping and nothing else: no SQL, no business rules.
package handler

import (
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/middleware"
	"github.com/base-admin/base-admin-core/backend/internal/service"
)

// Handler groups the HTTP endpoints of the application.
type Handler struct {
	Auth      *service.AuthService
	Users     *service.UserService
	Roles     *service.RoleService
	Menus     *service.MenuService
	Logs      *service.LogService
	Dashboard *service.DashboardService
	Log       *slog.Logger
}

// New builds the handler set.
func New(
	auth *service.AuthService,
	users *service.UserService,
	roles *service.RoleService,
	menus *service.MenuService,
	logs *service.LogService,
	dashboard *service.DashboardService,
	log *slog.Logger,
) *Handler {
	return &Handler{
		Auth:      auth,
		Users:     users,
		Roles:     roles,
		Menus:     menus,
		Logs:      logs,
		Dashboard: dashboard,
		Log:       log,
	}
}

// idParam parses the ":id" path parameter.
func idParam(c *gin.Context) (int64, error) {
	return parseID(c.Param("id"))
}

// parseID converts a positive decimal identifier.
func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.ErrInvalidParams.WithMessage("无效的资源标识")
	}
	return id, nil
}

// pagination reads page/pageSize with safe defaults.
func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

// statusFilter reads the optional ?status=0|1 filter.
func statusFilter(c *gin.Context) *int16 {
	raw := c.Query("status")
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	status := int16(value)
	return &status
}

// actor returns the authenticated caller, which the router guarantees.
func actor(c *gin.Context) (*middleware.Principal, error) {
	principal, ok := middleware.CurrentPrincipal(c)
	if !ok {
		return nil, apperr.ErrUnauthorized
	}
	return principal, nil
}
