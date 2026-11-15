package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// TokenType distinguishes access tokens from refresh tokens so a refresh token
// can never be replayed as an access token.
type TokenType string

const (
	// TokenTypeAccess authorises API calls.
	TokenTypeAccess TokenType = "access"
	// TokenTypeRefresh exchanges for a new token pair.
	TokenTypeRefresh TokenType = "refresh"
)

// Claims is the JWT payload.
//
// SessionID ties the token to a server-side session record in Redis, which is
// what makes logout and forced revocation effective for stateless JWTs.
type Claims struct {
	UserID    int64     `json:"uid"`
	Username  string    `json:"uname"`
	SessionID string    `json:"sid"`
	Type      TokenType `json:"typ"`
	jwt.RegisteredClaims
}

// TokenManager issues and verifies HS256 tokens.
type TokenManager struct {
	secret     []byte
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// TokenOptions configures a TokenManager.
type TokenOptions struct {
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

// NewTokenManager builds a manager from configuration.
func NewTokenManager(opts TokenOptions) *TokenManager {
	return &TokenManager{
		secret:     []byte(opts.Secret),
		issuer:     opts.Issuer,
		audience:   opts.Audience,
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
	}
}

// AccessTTL exposes the access token lifetime.
func (m *TokenManager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL exposes the refresh token lifetime.
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// Issue signs a token of the requested type and returns it with its expiry.
func (m *TokenManager) Issue(userID int64, username, sessionID string, tokenType TokenType) (string, time.Time, error) {
	ttl := m.accessTTL
	if tokenType == TokenTypeRefresh {
		ttl = m.refreshTTL
	}
	now := time.Now()
	expiresAt := now.Add(ttl)

	claims := Claims{
		UserID:    userID,
		Username:  username,
		SessionID: sessionID,
		Type:      tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			Subject:   fmt.Sprintf("%d", userID),
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse verifies a token and checks that it is of the expected type.
func (m *TokenManager) Parse(tokenString string, expected TokenType) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
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
