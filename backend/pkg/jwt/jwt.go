// Package jwt 签发并验证应用的 HS256 访问令牌与刷新令牌。
//
// 令牌使用单一对称密钥签名，因此本包刻意固定 HS256，绝不信任令牌头中声明的算法。
package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// Type 区分访问令牌与刷新令牌，使刷新令牌永远不能被重放为访问令牌。
type Type string

const (
	// TypeAccess 授权 API 调用。
	TypeAccess Type = "access"
	// TypeRefresh 用于换取新的令牌对。
	TypeRefresh Type = "refresh"
)

// Claims 是 JWT 载荷。
//
// SessionID 把令牌绑定到服务端会话记录，从而使登出和强制吊销对原本无状态的 JWT 立即生效。
type Claims struct {
	UserID    int64  `json:"uid"`
	Username  string `json:"uname"`
	SessionID string `json:"sid"`
	Type      Type   `json:"typ"`
	jwtlib.RegisteredClaims
}

// Manager 签发并验证令牌。
type Manager struct {
	secret     []byte
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// Options 配置 Manager。
type Options struct {
	// Secret 用于签发并验证 HS256 令牌；必须只保留在服务端。
	Secret string
	// Issuer 写入 iss 声明，并在验证时核对。
	Issuer string
	// Audience 写入 aud 声明，并在验证时核对。
	Audience string
	// AccessTTL 限制访问令牌的有效期。
	AccessTTL time.Duration
	// RefreshTTL 限制刷新令牌的有效期。
	RefreshTTL time.Duration
}

// NewManager 根据配置构建管理器。
func NewManager(opts Options) *Manager {
	return &Manager{
		secret:     []byte(opts.Secret),
		issuer:     opts.Issuer,
		audience:   opts.Audience,
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
	}
}

// AccessTTL 暴露访问令牌的有效期。
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL 暴露刷新令牌的有效期。
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// Issue 签发指定类型的令牌，并连同过期时间一并返回。
func (m *Manager) Issue(userID int64, username, sessionID string, tokenType Type) (string, time.Time, error) {
	ttl := m.accessTTL
	if tokenType == TypeRefresh {
		ttl = m.refreshTTL
	}
	now := time.Now()
	expiresAt := now.Add(ttl)

	claims := Claims{
		UserID:    userID,
		Username:  username,
		SessionID: sessionID,
		Type:      tokenType,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.issuer,
			Audience:  jwtlib.ClaimStrings{m.audience},
			Subject:   fmt.Sprintf("%d", userID),
			ID:        uuid.NewString(),
			IssuedAt:  jwtlib.NewNumericDate(now),
			NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse 验证令牌，并检查其类型是否符合预期。
func (m *Manager) Parse(tokenString string, expected Type) (*Claims, error) {
	claims := &Claims{}
	_, err := jwtlib.ParseWithClaims(tokenString, claims, func(_ *jwtlib.Token) (any, error) {
		return m.secret, nil
	},
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}),
		jwtlib.WithIssuer(m.issuer),
		jwtlib.WithAudience(m.audience),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithLeeway(30*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwtlib.ErrTokenExpired) {
			return nil, apperr.ErrTokenExpired.Wrap(err)
		}
		return nil, apperr.ErrTokenInvalid.Wrap(err)
	}
	if claims.Type != expected {
		return nil, apperr.ErrTokenInvalid.WithMessage("凭证类型不正确")
	}
	if claims.UserID <= 0 || claims.SessionID == "" {
		return nil, apperr.ErrTokenInvalid
	}
	return claims, nil
}
