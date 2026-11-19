package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
	"github.com/nightelforc/base-admin-core/backend/pkg/response"
)

// Recovery converts panics into a logged 500 envelope instead of a dropped
// connection, keeping the audit middleware able to record the request. The
// panic line goes through the request-scoped logger installed by AccessLog, so
// it carries the same request_id as the access line.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			logger.From(c.Request.Context()).Error("panic recovered",
				"panic", recovered,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
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
