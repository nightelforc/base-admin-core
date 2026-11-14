package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/model"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/internal/security"
)

// usernamePattern keeps account names URL- and log-friendly.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

// UserListItem is one row of the account list, roles resolved so the table can
// render them without N+1 requests.
type UserListItem struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Nickname    string     `json:"nickname"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	Avatar      string     `json:"avatar"`
	Remark      string     `json:"remark"`
	Status      int16      `json:"status"`
	RoleIDs     []int64    `json:"roleIds"`
	RoleCodes   []string   `json:"roleCodes"`
	RoleNames   []string   `json:"roleNames"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
	LastLoginIP string     `json:"lastLoginIp"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// ListUsersInput is the account list query.
type ListUsersInput struct {
	Keyword  string
	Status   *int16
	RoleID   int64
	Page     int
	PageSize int
}

// CreateUserInput describes a new account.
type CreateUserInput struct {
	Username string
	Password string
	Nickname string
	Email    string
	Phone    string
	Remark   string
	Status   int16
	RoleIDs  []int64
}

// UpdateUserInput describes an account edit. Usernames are immutable so audit
// records stay meaningful.
type UpdateUserInput struct {
	Nickname string
	Email    string
	Phone    string
	Remark   string
	Status   int16
	RoleIDs  []int64
}

// UserService owns the account lifecycle.
type UserService struct {
	users    *repository.UserRepository
	roles    *repository.RoleRepository
	hasher   *security.PasswordHasher
	sessions *security.SessionStore
}

// NewUserService wires the account use cases.
func NewUserService(users *repository.UserRepository, roles *repository.RoleRepository, hasher *security.PasswordHasher, sessions *security.SessionStore) *UserService {
	return &UserService{users: users, roles: roles, hasher: hasher, sessions: sessions}
}

// List returns one page of accounts with their roles.
func (s *UserService) List(ctx context.Context, in ListUsersInput) ([]UserListItem, int64, error) {
	page := repository.Page{Offset: (in.Page - 1) * in.PageSize, Limit: in.PageSize}
	users, total, err := s.users.List(ctx, repository.UserFilter{
		Keyword: in.Keyword,
		Status:  in.Status,
		RoleID:  in.RoleID,
		Page:    page,
	})
	if err != nil {
		return nil, 0, err
	}

	ids := make([]int64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	rolesByUser, err := s.users.RolesByUserIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}

	items := make([]UserListItem, 0, len(users))
	for _, user := range users {
		item := UserListItem{
			ID:          user.ID,
			Username:    user.Username,
			Nickname:    user.Nickname,
			Email:       user.Email,
			Phone:       user.Phone,
			Avatar:      user.Avatar,
			Remark:      user.Remark,
			Status:      user.Status,
			LastLoginAt: user.LastLoginAt,
			LastLoginIP: user.LastLoginIP,
			CreatedAt:   user.CreatedAt,
			RoleIDs:     []int64{},
			RoleCodes:   []string{},
			RoleNames:   []string{},
		}
		for _, role := range rolesByUser[user.ID] {
			item.RoleIDs = append(item.RoleIDs, role.ID)
			item.RoleCodes = append(item.RoleCodes, role.Code)
			item.RoleNames = append(item.RoleNames, role.Name)
		}
		items = append(items, item)
	}
	return items, total, nil
}

// Create registers a new account and assigns its roles.
func (s *UserService) Create(ctx context.Context, in CreateUserInput) (int64, error) {
	username := strings.TrimSpace(in.Username)
	if !usernamePattern.MatchString(username) {
		return 0, apperr.ErrInvalidParams.WithMessage("用户名需为 3-32 位字母、数字、下划线、点或中划线")
	}
	if err := s.hasher.Validate(in.Password); err != nil {
		return 0, err
	}
	if err := s.assertRoles(ctx, in.RoleIDs, true); err != nil {
		return 0, err
	}

	exists, err := s.users.ExistsByUsername(ctx, username, 0)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, apperr.ErrUsernameTaken
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return 0, err
	}

	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		Nickname:     strings.TrimSpace(in.Nickname),
		Email:        strings.TrimSpace(in.Email),
		Phone:        strings.TrimSpace(in.Phone),
		Remark:       strings.TrimSpace(in.Remark),
		Status:       statusOrDefault(in.Status),
	}
	if err := s.users.Create(ctx, user); err != nil {
		return 0, err
	}
	if err := s.users.ReplaceRoles(ctx, user.ID, in.RoleIDs); err != nil {
		return 0, err
	}
	return user.ID, nil
}

// Update edits an existing account.
func (s *UserService) Update(ctx context.Context, actorID, id int64, in UpdateUserInput) error {
	status := normalizeStatus(in.Status)
	if actorID == id && status == model.StatusDisabled {
		return apperr.ErrCannotDisableSelf
	}
	if status == model.StatusDisabled {
		if err := s.assertSuperAdminSurvives(ctx, id); err != nil {
			return err
		}
	}
	if err := s.assertRoles(ctx, in.RoleIDs, true); err != nil {
		return err
	}

	if err := s.users.UpdateFields(ctx, id, map[string]any{
		"nickname": strings.TrimSpace(in.Nickname),
		"email":    strings.TrimSpace(in.Email),
		"phone":    strings.TrimSpace(in.Phone),
		"remark":   strings.TrimSpace(in.Remark),
		"status":   status,
	}); err != nil {
		return err
	}
	return s.users.ReplaceRoles(ctx, id, in.RoleIDs)
}

// Delete soft-deletes an account.
func (s *UserService) Delete(ctx context.Context, actorID, id int64) error {
	if actorID == id {
		return apperr.ErrCannotDeleteSelf
	}
	if err := s.assertSuperAdminSurvives(ctx, id); err != nil {
		return err
	}
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}
	// 账号删除后立刻让它已签发的会话失效，避免旧令牌继续在 TTL 内可用。
	return s.sessions.DeleteAllForUser(ctx, id)
}

// ResetPassword sets a new password chosen by an administrator.
func (s *UserService) ResetPassword(ctx context.Context, id int64, password string) error {
	if err := s.hasher.Validate(password); err != nil {
		return err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err := s.users.UpdateFields(ctx, id, map[string]any{"password_hash": hash}); err != nil {
		return err
	}
	// Force a re-login everywhere with the new credentials.
	return s.sessions.DeleteAllForUser(ctx, id)
}

// SetStatus enables or disables an account.
func (s *UserService) SetStatus(ctx context.Context, actorID, id int64, status int16) error {
	status = normalizeStatus(status)
	if actorID == id && status == model.StatusDisabled {
		return apperr.ErrCannotDisableSelf
	}
	if status == model.StatusDisabled {
		if err := s.assertSuperAdminSurvives(ctx, id); err != nil {
			return err
		}
	}

	if err := s.users.UpdateFields(ctx, id, map[string]any{"status": status}); err != nil {
		return err
	}
	if status == model.StatusDisabled {
		return s.sessions.DeleteAllForUser(ctx, id)
	}
	return nil
}

// assertRoles verifies the referenced roles exist; requireOne also rejects an
// empty selection, which would leave the account without any access.
func (s *UserService) assertRoles(ctx context.Context, roleIDs []int64, requireOne bool) error {
	unique := make([]int64, 0, len(roleIDs))
	seen := make(map[int64]struct{}, len(roleIDs))
	for _, id := range roleIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		if requireOne {
			return apperr.ErrInvalidParams.WithMessage("请至少选择一个角色")
		}
		return nil
	}

	for _, id := range unique {
		if _, err := s.roles.FindByID(ctx, id); err != nil {
			return apperr.ErrInvalidParams.WithMessage("角色不存在或已被删除")
		}
	}
	return nil
}

// assertSuperAdminSurvives refuses an operation that would leave the system
// without an enabled super administrator.
func (s *UserService) assertSuperAdminSurvives(ctx context.Context, userID int64) error {
	superAdmin, err := s.users.IsSuperAdmin(ctx, userID)
	if err != nil || !superAdmin {
		return err
	}

	role, err := s.roles.FindByCode(ctx, model.RoleCodeSuperAdmin)
	if err != nil {
		return nil
	}
	remaining, err := s.users.CountEnabledByRoleID(ctx, role.ID)
	if err != nil {
		return err
	}
	if remaining <= 1 {
		return apperr.ErrProtectedRole.WithMessage("系统需要保留至少一个启用的超级管理员")
	}
	return nil
}

func normalizeStatus(status int16) int16 {
	if status == model.StatusEnabled {
		return model.StatusEnabled
	}
	return model.StatusDisabled
}

// statusOrDefault treats the zero value as "enabled" so a caller that omits the
// field while creating an account does not accidentally create a locked one.
func statusOrDefault(status int16) int16 {
	if status == model.StatusDisabled {
		return model.StatusDisabled
	}
	return model.StatusEnabled
}
