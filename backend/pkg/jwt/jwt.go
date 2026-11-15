// Package jwt issues and verifies the application's HS256 access and refresh
// tokens.
//
// Tokens are signed with a single symmetric secret, so the package deliberately
// pins HS256 and never trusts the algorithm named in the token header.
package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// Type distinguishes access tokens from refresh tokens so a refresh token can
// never be replayed as an access token.
type Type string

const (
	// TypeAccess authorises API calls.
	TypeAccess Type = "access"
	// TypeRefresh exchanges for a new token pair.
	TypeRefresh Type = "refresh"
)

// Claims is the JWT payload.
//
// SessionID ties the token to a server-side session record, which is what makes
// logout and forced revocation effective for otherwise stateless JWTs.
type Claims struct {
	UserID    int64  `json:"uid"`
	Username  string `json:"uname"`
	SessionID string `json:"sid"`
	Type      Type   `json:"typ"`
	jwtlib.RegisteredClaims
}

// Manager issues and verifies tokens.
type Manager struct {
	secret     []byte
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// Options configures a Manager.
type Options struct {
	// Secret signs and verifies HS256 tokens; must stay server-side only.
	Secret string
	// Issuer is written to (and verified from) the iss claim.
	Issuer string
	// Audience is written to (and verified from) the aud claim.
	Audience string
	// AccessTTL bounds an access token's lifetime.
	AccessTTL time.Duration
	// RefreshTTL bounds a refresh token's lifetime.
	RefreshTTL time.Duration
}

// NewManager builds a manager from configuration.
func NewManager(opts Options) *Manager {
	return &Manager{
		secret:     []byte(opts.Secret),
		issuer:     opts.Issuer,
		audience:   opts.Audience,
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
	}
}

// AccessTTL exposes the access token lifetime.
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL exposes the refresh token lifetime.
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// Issue signs a token of the requested type and returns it with its expiry.
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

// Parse verifies a token and checks that it is of the expected type.
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
