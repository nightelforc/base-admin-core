package handler

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/internal/service"
	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
	"github.com/nightelforc/base-admin-core/backend/pkg/response"
)

// logQuery builds a log filter from the request query string.
func (h *Handler) logQuery(c *gin.Context) (service.LogQuery, error) {
	page, pageSize := pagination(c)

	query := service.LogQuery{
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	}

	if raw := c.Query("userId"); raw != "" {
		userID, err := parseID(raw)
		if err != nil {
			return query, err
		}
		query.UserID = userID
	}

	if raw := c.Query("success"); raw == "0" {
		failed := false
		query.Success = &failed
	} else if raw == "1" {
		succeeded := true
		query.Success = &succeeded
	}

	if raw := c.Query("from"); raw != "" {
		from, err := parseDate(raw)
		if err != nil {
			return query, err
		}
		query.From = &from
	}
	if raw := c.Query("to"); raw != "" {
		to, err := parseDate(raw)
		if err != nil {
			return query, err
		}
		query.To = &to
	}

	return query, nil
}

// ListLoginLogs returns a page of authentication attempts.
func (h *Handler) ListLoginLogs(c *gin.Context) {
	query, err := h.logQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	items, total, err := h.Logs.ListLogin(c.Request.Context(), query)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPage(items, total, query.Page, query.PageSize))
}

// ListOperationLogs returns a page of audited API calls.
func (h *Handler) ListOperationLogs(c *gin.Context) {
	query, err := h.logQuery(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	items, total, err := h.Logs.ListOperation(c.Request.Context(), query)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPage(items, total, query.Page, query.PageSize))
}

// DashboardOverview returns the counters and seven-day trend of the console.
func (h *Handler) DashboardOverview(c *gin.Context) {
	overview, err := h.Dashboard.Overview(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, overview)
}

// parseDate accepts RFC3339 or a plain calendar date, interpreted as local
// time so date-only filters behave the way an operator expects.
func parseDate(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed, nil
	}
	return time.Time{}, apperr.ErrInvalidParams.WithMessage("日期格式不正确，应为 2006-01-02 或 RFC3339")
}
