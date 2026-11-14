package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog writes one structured line per request and skips the noisy probes.
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
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
			"request_id", RequestID(c),
		}
		if principal, ok := CurrentPrincipal(c); ok {
			attrs = append(attrs, "user_id", principal.UserID, "username", principal.Username)
		}
		if errors := c.Errors.ByType(gin.ErrorTypePrivate).String(); errors != "" {
			attrs = append(attrs, "errors", errors)
		}

		switch status := c.Writer.Status(); {
		case status >= http.StatusInternalServerError:
			log.Error("http request", attrs...)
		case status >= http.StatusBadRequest:
			log.Warn("http request", attrs...)
		default:
			log.Info("http request", attrs...)
		}
	}
}
