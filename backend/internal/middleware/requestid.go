package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestIDHeader is echoed to clients and written to the logs.
const RequestIDHeader = "X-Request-ID"

// RequestIDMiddleware assigns a correlation id to every request, reusing the
// inbound header when a gateway already provided one.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(ContextKeyRequestID, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}
