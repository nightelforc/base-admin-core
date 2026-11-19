package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
)

// AccessLog installs the request-scoped logger and writes one structured line
// per request, skipping the noisy probes.
//
// The scoped logger carries request_id and is stored in the request context, so
// every layer below (handlers, services, GORM) resolves the same logger through
// logger.From and its records can be correlated with this access line.
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		scoped := log.With("request_id", RequestID(c))
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), scoped))

		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}

		started := time.Now()
		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"query", c.Request.URL.RawQuery,
			"status", c.Writer.Status(),
			"bytes", c.Writer.Size(),
			"latency", time.Since(started).String(),
			"ip", c.ClientIP(),
		}
		if principal, ok := CurrentPrincipal(c); ok {
			attrs = append(attrs, "user_id", principal.UserID, "username", principal.Username)
		}
		if errors := c.Errors.ByType(gin.ErrorTypePrivate).String(); errors != "" {
			attrs = append(attrs, "errors", errors)
		}

		switch status := c.Writer.Status(); {
		case status >= http.StatusInternalServerError:
			scoped.Error("http request", attrs...)
		case status >= http.StatusBadRequest:
			scoped.Warn("http request", attrs...)
		default:
			scoped.Info("http request", attrs...)
		}
	}
}
