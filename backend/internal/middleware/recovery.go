package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
	"github.com/base-admin/base-admin-core/backend/pkg/response"
)

// Recovery converts panics into a logged 500 envelope instead of a dropped
// connection, keeping the audit middleware able to record the request.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			log.Error("panic recovered",
				"panic", recovered,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"request_id", RequestID(c),
				"stack", string(debug.Stack()),
			)
			if c.Writer.Written() {
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, response.Envelope{
				Code:    apperr.ErrInternal.Code,
				Message: "服务器内部错误",
			})
		}()

		c.Next()
	}
}
