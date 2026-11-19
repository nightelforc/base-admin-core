// Package response 渲染前端消费的统一 JSON 响应信封。
//
// 成功载荷为 `{"code":0,"message":"ok","data":...}`；失败保持同一形状，业务码非零。
// 码 0 是 Vben 请求客户端视为成功的约定（defaultResponseInterceptor.successCode）。
package response

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
)

// Envelope 是每个 API 响应的传输形态。
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK 写出 HTTP 200 的成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: apperr.CodeSuccess, Message: "ok", Data: data})
}

// Created 写出 HTTP 201 的成功响应。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Code: apperr.CodeSuccess, Message: "ok", Data: data})
}

// NoContent 写出载荷为空的成功响应。
func NoContent(c *gin.Context) {
	c.JSON(http.StatusOK, Envelope{Code: apperr.CodeSuccess, Message: "ok"})
}

// Fail 按 *apperr.Error 携带的状态与业务码渲染 err。
// 意外错误会连同其原因写入日志，并向外报告为通用内部错误，使实现细节不会到达客户端。
//
// 下游（数据库、Redis、出站 HTTP）因请求上下文到期而失败时，语义是「请求超时」
// 而不是服务器故障，因此这里统一折算成 504 / 50400，避免前端把慢查询当成 500。
func Fail(c *gin.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		err = apperr.ErrRequestTimeout.Wrap(err)
	}

	appErr := apperr.From(err)
	if appErr == nil {
		NoContent(c)
		return
	}

	if appErr.HTTPStatus >= http.StatusInternalServerError {
		attrs := []any{
			"code", appErr.Code,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", appErr.HTTPStatus,
		}
		if appErr.Cause != nil {
			attrs = append(attrs, "error", appErr.Cause.Error())
		}
		logger.From(c.Request.Context()).Error("request failed", attrs...)
	}

	message := appErr.Message
	if message == "" {
		message = http.StatusText(appErr.HTTPStatus)
	}

	c.AbortWithStatusJSON(appErr.HTTPStatus, Envelope{Code: appErr.Code, Message: message})
}

// Page 是分页列表载荷。
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewPage 构建分页载荷，把 nil 切片规范为空切片，使客户端始终收到数组。
func NewPage[T any](list []T, total int64, page, pageSize int) Page[T] {
	if list == nil {
		list = []T{}
	}
	return Page[T]{List: list, Total: total, Page: page, PageSize: pageSize}
}
