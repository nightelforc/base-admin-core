// Package middleware contains the cross-cutting HTTP concerns: request ids,
// structured access logs, panic recovery, CORS, authentication, permission
// checks and the operation audit trail.
package middleware

import (
	"github.com/gin-gonic/gin"
)

// Context keys used to publish per-request values to handlers.
const (
	ContextKeyRequestID = "request_id"
	ContextKeyPrincipal = "principal"
)

// Principal is the authenticated caller of the current request. It is resolved
// once in the authentication middleware and read-only afterwards.
type Principal struct {
	UserID      int64
	Username    string
	SessionID   string
	Roles       []string
	Permissions []string
	SuperAdmin  bool
}

// Can reports whether the caller holds a permission code.
func (p *Principal) Can(code string) bool {
	if p == nil {
		return false
	}
	if p.SuperAdmin {
		return true
	}
	for _, permission := range p.Permissions {
		if permission == code {
			return true
		}
	}
	return false
}

// CurrentPrincipal returns the caller stored by the authentication middleware.
func CurrentPrincipal(c *gin.Context) (*Principal, bool) {
	value, ok := c.Get(ContextKeyPrincipal)
	if !ok {
		return nil, false
	}
	principal, ok := value.(*Principal)
	return principal, ok
}

// RequestID returns the correlation id of the current request.
func RequestID(c *gin.Context) string {
	return c.GetString(ContextKeyRequestID)
}
