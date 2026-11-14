package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/internal/response"
	"github.com/base-admin/base-admin-core/backend/internal/security"
)

// Authenticator resolves the bearer token of a request into a Principal.
type Authenticator struct {
	tokens   *security.TokenManager
	sessions *security.SessionStore
	users    *repository.UserRepository
}

// NewAuthenticator wires the token, session and account lookups.
func NewAuthenticator(tokens *security.TokenManager, sessions *security.SessionStore, users *repository.UserRepository) *Authenticator {
	return &Authenticator{tokens: tokens, sessions: sessions, users: users}
}

// Middleware rejects unauthenticated requests and publishes the Principal.
//
// The session record is the authority, not the token: a token whose session was
// revoked (logout, password change, account disabled) stops working
// immediately, even though its signature is still valid.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c)
		if !ok {
			response.Fail(c, apperr.ErrUnauthorized)
			c.Abort()
			return
		}

		claims, err := a.tokens.Parse(token, security.TokenTypeAccess)
		if err != nil {
			response.Fail(c, err)
			c.Abort()
			return
		}

		session, err := a.sessions.Get(c.Request.Context(), claims.SessionID)
		if err != nil {
			response.Fail(c, apperr.ErrTokenInvalid.WithMessage("登录状态已失效，请重新登录"))
			c.Abort()
			return
		}
		if session.UserID != claims.UserID {
			response.Fail(c, apperr.ErrTokenInvalid)
			c.Abort()
			return
		}

		user, err := a.users.FindByID(c.Request.Context(), claims.UserID)
		if err != nil {
			// 账号已被删除（软删）时，令牌不能再当作"已禁用"处理：
			// 返回 401 让前端走重新登录，而不是提示一条会让人误解的禁用文案。
			if apperr.Is(err, apperr.ErrUserNotFound) {
				response.Fail(c, apperr.ErrTokenInvalid.WithMessage("账号不存在或已被删除，请重新登录"))
			} else {
				response.Fail(c, err)
			}
			c.Abort()
			return
		}
		if !user.IsEnabled() {
			response.Fail(c, apperr.ErrAccountDisabled)
			c.Abort()
			return
		}

		principal := &Principal{
			UserID:    user.ID,
			Username:  user.Username,
			SessionID: session.ID,
		}

		roles, err := a.users.RolesOfUser(c.Request.Context(), user.ID)
		if err != nil {
			response.Fail(c, err)
			c.Abort()
			return
		}
		for _, role := range roles {
			principal.Roles = append(principal.Roles, role.Code)
			if role.IsSuperAdmin() {
				principal.SuperAdmin = true
			}
		}

		if !principal.SuperAdmin {
			codes, err := a.users.PermissionCodes(c.Request.Context(), user.ID)
			if err != nil {
				response.Fail(c, err)
				c.Abort()
				return
			}
			principal.Permissions = codes
		}

		c.Set(ContextKeyPrincipal, principal)
		c.Next()
	}
}

func bearerToken(c *gin.Context) (string, bool) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// RequirePermission refuses the request unless the caller holds a code.
func RequirePermission(code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := CurrentPrincipal(c)
		if !ok {
			response.Fail(c, apperr.ErrUnauthorized)
			c.Abort()
			return
		}
		if !principal.Can(code) {
			response.Fail(c, apperr.ErrForbidden.WithMessage("没有权限执行该操作"))
			c.Abort()
			return
		}
		c.Next()
	}
}
