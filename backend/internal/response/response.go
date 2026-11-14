// Package response renders the uniform JSON envelope consumed by the frontend.
//
// Success payloads are `{"code":0,"message":"ok","data":...}`; failures keep the
// same shape with a non-zero business code. Code 0 is the contract the Vben
// request client treats as success (defaultResponseInterceptor.successCode).
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/logger"
)

// Envelope is the transport shape of every API response.
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// OK writes a successful response with HTTP 200.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: apperr.CodeSuccess, Message: "ok", Data: data})
}

// Created writes a successful response with HTTP 201.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Code: apperr.CodeSuccess, Message: "ok", Data: data})
}

// NoContent writes a successful response with an empty payload.
func NoContent(c *gin.Context) {
	c.JSON(http.StatusOK, Envelope{Code: apperr.CodeSuccess, Message: "ok"})
}

// Fail renders err using the status and business code carried by *apperr.Error.
// Unexpected errors are logged with their cause and reported as a generic
// internal error so implementation details never reach the client.
func Fail(c *gin.Context, err error) {
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

// Page is the paginated list payload.
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewPage builds a paginated payload, normalising a nil slice to an empty one so
// the client always receives an array.
func NewPage[T any](list []T, total int64, page, pageSize int) Page[T] {
	if list == nil {
		list = []T{}
	}
	return Page[T]{List: list, Total: total, Page: page, PageSize: pageSize}
}
