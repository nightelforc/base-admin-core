package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/config"
)

// CORS implements a strict, explicit allow-list based policy. A wildcard
// origin cannot be combined with credentials, which the configuration package
// rejects in production.
func CORS(cfg config.CORS) gin.HandlerFunc {
	allowAll := hasWildcard(cfg.AllowedOrigins)
	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		origins[strings.TrimSpace(origin)] = struct{}{}
	}

	allowedMethods := strings.Join(cfg.AllowedMethods, ", ")
	allowedHeaders := strings.Join(cfg.AllowedHeaders, ", ")
	maxAge := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if allowAll && !cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Origin", "*")
			} else if _, ok := origins[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				if cfg.AllowCredentials {
					c.Header("Access-Control-Allow-Credentials", "true")
				}
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", allowedMethods)
			c.Header("Access-Control-Allow-Headers", allowedHeaders)
			c.Header("Access-Control-Max-Age", maxAge)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposeHeaders, ", "))
		c.Next()
	}
}

func hasWildcard(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}
