// Package service holds the application's use cases. Handlers translate HTTP
// into these calls and render the result; repositories stay unaware of the
// transport. Business rules (RBAC, ownership, invariants) belong here.
package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/internal/model"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
	"github.com/base-admin/base-admin-core/backend/pkg/security"
)

// TokenPair is what the client stores after a successful login or refresh.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int64  `json:"expiresIn"`
}

// Profile is the authenticated identity, shaped after what the frontend store
// expects (userId/username/realName/roles/homePath).
type Profile struct {
	UserID   int64    `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Nickname string   `json:"nickname"`
	Email    string   `json:"email"`
	Phone    string   `json:"phone"`
	Avatar   string   `json:"avatar"`
	Roles    []string `json:"roles"`
	HomePath string   `json:"homePath"`
}

// HomePath is the landing route of the workspace dashboard.
const HomePath = "/dashboard/workspace"

// AuthService implements sign-in, token rotation and self-service account
// management.
type AuthService struct {
	users    *repository.UserRepository
	logs     *repository.LogRepository
	hasher   *security.PasswordHasher
	tokens   *security.TokenManager
	sessions *security.SessionStore
	throttle *security.LoginThrottle
	cfg      config.Security
	now      func() time.Time
}

// AuthDeps groups the collaborators of AuthService.
type AuthDeps struct {
	Users    *repository.UserRepository
	Logs     *repository.LogRepository
	Hasher   *security.PasswordHasher
	Tokens   *security.TokenManager
	Sessions *security.SessionStore
	Throttle *security.LoginThrottle
	Config   config.Security
}

// NewAuthService wires the authentication use cases.
func NewAuthService(deps AuthDeps) *AuthService {
	return &AuthService{
		users:    deps.Users,
		logs:     deps.Logs,
		hasher:   deps.Hasher,
		tokens:   deps.Tokens,
		sessions: deps.Sessions,
		throttle: deps.Throttle,
		cfg:      deps.Config,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// LoginInput carries the credentials plus the request context recorded in the
// audit trail.
type LoginInput struct {
	Username  string
	Password  string
	IP        string
	UserAgent string
}

// Login verifies credentials, opens a session and returns a token pair.
//
// Every attempt is written to the login audit log, successful or not, and
// failures feed the per username/IP throttle so online guessing is bounded.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" || in.Password == "" {
		return nil, apperr.ErrInvalidParams.WithMessage("请输入用户名和密码")
	}

	if blocked, wait, err := s.throttle.Blocked(ctx, username, in.IP); err != nil {
		return nil, err
	} else if blocked {
		s.recordLogin(ctx, 0, username, in, false, "登录失败次数过多，请稍后再试")
		return nil, apperr.ErrLoginBlocked.WithMessage("登录失败次数过多，请 %s 后再试", humanizeDuration(wait))
	}

	user, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if apperr.Is(err, apperr.ErrUserNotFound) {
			s.fail(ctx, 0, username, in, "用户名或密码错误")
			return nil, apperr.ErrBadCredentials
		}
		return nil, err
	}

	if !user.IsEnabled() {
		s.recordLogin(ctx, user.ID, username, in, false, "账号已被禁用")
		return nil, apperr.ErrAccountDisabled
	}

	if !s.hasher.Verify(user.PasswordHash, in.Password) {
		s.fail(ctx, user.ID, username, in, "用户名或密码错误")
		return nil, apperr.ErrBadCredentials
	}

	if err := s.throttle.Reset(ctx, username, in.IP); err != nil {
		return nil, err
	}

	sessionID, err := security.NewSessionID()
	if err != nil {
		return nil, err
	}

	session := security.Session{
		ID:        sessionID,
		UserID:    user.ID,
		Username:  user.Username,
		IP:        in.IP,
		UserAgent: truncate(in.UserAgent, 255),
		CreatedAt: s.now(),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}

	pair, err := s.issueTokenPair(user, sessionID)
	if err != nil {
		_ = s.sessions.Delete(ctx, sessionID)
		return nil, err
	}

	if err := s.users.TouchLogin(ctx, user.ID, in.IP, s.now()); err != nil {
		return nil, err
	}
	s.recordLogin(ctx, user.ID, username, in, true, "登录成功")

	return pair, nil
}

// Refresh rotates a valid refresh token into a new pair. The session record is
// authoritative: a revoked session cannot be refreshed even if the token itself
// has not expired yet.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.tokens.Parse(refreshToken, security.TokenTypeRefresh)
	if err != nil {
		return nil, err
	}

	session, err := s.sessions.Get(ctx, claims.SessionID)
	if err != nil {
		return nil, apperr.ErrTokenInvalid.WithMessage("登录状态已失效，请重新登录")
	}
	if session.UserID != claims.UserID {
		return nil, apperr.ErrTokenInvalid
	}

	user, err := s.users.FindByID(ctx, claims.UserID)
	if err != nil {
		_ = s.sessions.Delete(ctx, claims.SessionID)
		return nil, apperr.ErrTokenInvalid
	}
	if !user.IsEnabled() {
		_ = s.sessions.Delete(ctx, claims.SessionID)
		return nil, apperr.ErrAccountDisabled
	}

	if err := s.sessions.Touch(ctx, claims.SessionID); err != nil {
		return nil, err
	}
	return s.issueTokenPair(user, claims.SessionID)
}

// Logout revokes the session behind the presented access token.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionID)
}

// Profile returns the identity of the current user for the frontend store.
func (s *AuthService) Profile(ctx context.Context, userID int64) (*Profile, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	roles, err := s.users.RolesOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(roles))
	for _, role := range roles {
		codes = append(codes, role.Code)
	}

	return &Profile{
		UserID:   user.ID,
		Username: user.Username,
		RealName: user.DisplayName(),
		Nickname: user.Nickname,
		Email:    user.Email,
		Phone:    user.Phone,
		Avatar:   user.Avatar,
		Roles:    codes,
		HomePath: HomePath,
	}, nil
}

// UpdateProfile changes the self-service editable fields.
func (s *AuthService) UpdateProfile(ctx context.Context, userID int64, nickname, email, phone string) (*Profile, error) {
	if err := s.users.UpdateFields(ctx, userID, map[string]any{
		"nickname": strings.TrimSpace(nickname),
		"email":    strings.TrimSpace(email),
		"phone":    strings.TrimSpace(phone),
	}); err != nil {
		return nil, err
	}
	return s.Profile(ctx, userID)
}

// ChangePassword verifies the current password, stores the new hash and revokes
// every session so other devices are signed out.
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}

	if !s.hasher.Verify(user.PasswordHash, current) {
		return apperr.ErrPasswordMismatch
	}
	if err := s.hasher.Validate(next); err != nil {
		return err
	}
	if current == next {
		return apperr.ErrSamePassword
	}

	hash, err := s.hasher.Hash(next)
	if err != nil {
		return err
	}
	if err := s.users.UpdateFields(ctx, userID, map[string]any{"password_hash": hash}); err != nil {
		return err
	}
	return s.sessions.DeleteAllForUser(ctx, userID)
}

func (s *AuthService) issueTokenPair(user *model.User, sessionID string) (*TokenPair, error) {
	access, _, err := s.tokens.Issue(user.ID, user.Username, sessionID, security.TokenTypeAccess)
	if err != nil {
		return nil, err
	}
	refresh, _, err := s.tokens.Issue(user.ID, user.Username, sessionID, security.TokenTypeRefresh)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
	}, nil
}

func (s *AuthService) fail(ctx context.Context, userID int64, username string, in LoginInput, message string) {
	if err := s.throttle.Fail(ctx, username, in.IP); err != nil {
		// A throttle outage must not turn a wrong password into a 500.
		_ = err
	}
	s.recordLogin(ctx, userID, username, in, false, message)
}

func (s *AuthService) recordLogin(ctx context.Context, userID int64, username string, in LoginInput, success bool, message string) {
	entry := &model.LoginLog{
		UserID:    userID,
		Username:  username,
		IP:        in.IP,
		UserAgent: truncate(in.UserAgent, 512),
		Success:   success,
		Message:   message,
		CreatedAt: s.now(),
	}
	// Auditing must never break the request path.
	_ = s.logs.CreateLogin(ctx, entry)
}

func humanizeDuration(d time.Duration) string {
	if d <= 0 {
		return "稍后"
	}
	minutes := int(d.Round(time.Minute).Minutes())
	if minutes <= 1 {
		return "1 分钟"
	}
	return strconv.Itoa(minutes) + " 分钟"
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
