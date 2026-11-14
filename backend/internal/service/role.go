package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/model"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
)

// roleCodePattern constrains role codes to what can be embedded in permission
// codes and JWT claims safely.
var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// RoleListItem is one row of the role table.
type RoleListItem struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Sort        int       `json:"sort"`
	Status      int16     `json:"status"`
	BuiltIn     bool      `json:"builtIn"`
	UserCount   int64     `json:"userCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RoleOption is the compact shape used by selects and assignment dialogs.
type RoleOption struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// RoleInput is the create/update payload.
type RoleInput struct {
	Code        string
	Name        string
	Description string
	Sort        int
	Status      int16
}

// RoleService owns roles and their menu grants.
type RoleService struct {
	roles *repository.RoleRepository
	users *repository.UserRepository
	menus *repository.MenuRepository
}

// NewRoleService wires the role use cases.
func NewRoleService(roles *repository.RoleRepository, users *repository.UserRepository, menus *repository.MenuRepository) *RoleService {
	return &RoleService{roles: roles, users: users, menus: menus}
}

// List returns one page of roles with their user counts.
func (s *RoleService) List(ctx context.Context, keyword string, status *int16, page, pageSize int) ([]RoleListItem, int64, error) {
	roles, total, err := s.roles.List(ctx, repository.RoleFilter{
		Keyword: keyword,
		Status:  status,
		Page:    repository.Page{Offset: (page - 1) * pageSize, Limit: pageSize},
	})
	if err != nil {
		return nil, 0, err
	}

	items := make([]RoleListItem, 0, len(roles))
	for _, role := range roles {
		count, err := s.roles.CountUsers(ctx, role.ID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, RoleListItem{
			ID:          role.ID,
			Code:        role.Code,
			Name:        role.Name,
			Description: role.Description,
			Sort:        role.Sort,
			Status:      role.Status,
			BuiltIn:     role.IsBuiltIn(),
			UserCount:   count,
			CreatedAt:   role.CreatedAt,
		})
	}
	return items, total, nil
}

// Options lists enabled roles for select inputs.
func (s *RoleService) Options(ctx context.Context) ([]RoleOption, error) {
	roles, err := s.roles.AllEnabled(ctx)
	if err != nil {
		return nil, err
	}

	options := make([]RoleOption, 0, len(roles))
	for _, role := range roles {
		options = append(options, RoleOption{ID: role.ID, Code: role.Code, Name: role.Name})
	}
	return options, nil
}

// Create registers a role.
func (s *RoleService) Create(ctx context.Context, in RoleInput) (int64, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.Name)
	if !roleCodePattern.MatchString(code) {
		return 0, apperr.ErrInvalidParams.WithMessage("角色标识需为 2-64 位小写字母、数字或下划线，且以字母开头")
	}
	if name == "" {
		return 0, apperr.ErrInvalidParams.WithMessage("请填写角色名称")
	}
	if isReservedRoleCode(code) {
		return 0, apperr.ErrInvalidParams.WithMessage("角色标识 %s 为系统保留值", code)
	}

	exists, err := s.roles.ExistsByCode(ctx, code, 0)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, apperr.ErrRoleCodeTaken
	}

	role := &model.Role{
		Code:        code,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Sort:        in.Sort,
		Status:      normalizeStatus(in.Status),
	}
	if err := s.roles.Create(ctx, role); err != nil {
		return 0, err
	}
	return role.ID, nil
}

// Update edits a role. Built-in roles keep their code and stay enabled so the
// permission model cannot be broken from the UI.
func (s *RoleService) Update(ctx context.Context, id int64, in RoleInput) error {
	role, err := s.roles.FindByID(ctx, id)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return apperr.ErrInvalidParams.WithMessage("请填写角色名称")
	}

	fields := map[string]any{
		"name":        name,
		"description": strings.TrimSpace(in.Description),
		"sort":        in.Sort,
	}

	if !role.IsBuiltIn() {
		code := strings.TrimSpace(in.Code)
		if !roleCodePattern.MatchString(code) {
			return apperr.ErrInvalidParams.WithMessage("角色标识需为 2-64 位小写字母、数字或下划线，且以字母开头")
		}
		exists, err := s.roles.ExistsByCode(ctx, code, id)
		if err != nil {
			return err
		}
		if exists {
			return apperr.ErrRoleCodeTaken
		}
		fields["code"] = code
		fields["status"] = normalizeStatus(in.Status)
	}

	return s.roles.UpdateFields(ctx, id, fields)
}

// Delete removes a role that is neither built in nor assigned to anybody.
func (s *RoleService) Delete(ctx context.Context, id int64) error {
	role, err := s.roles.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if role.IsBuiltIn() {
		return apperr.ErrProtectedRole
	}

	count, err := s.roles.CountUsers(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperr.ErrRoleInUse
	}
	return s.roles.Delete(ctx, id)
}

// MenuIDs returns the menu ids granted to a role.
func (s *RoleService) MenuIDs(ctx context.Context, id int64) ([]int64, error) {
	if _, err := s.roles.FindByID(ctx, id); err != nil {
		return nil, err
	}
	return s.roles.MenuIDsOfRole(ctx, id)
}

// AssignMenus replaces the menu grants of a role.
func (s *RoleService) AssignMenus(ctx context.Context, id int64, menuIDs []int64) error {
	role, err := s.roles.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if role.IsSuperAdmin() {
		return apperr.ErrProtectedRole.WithMessage("超级管理员角色默认拥有全部权限，无需分配")
	}

	unique := make([]int64, 0, len(menuIDs))
	seen := make(map[int64]struct{}, len(menuIDs))
	for _, menuID := range menuIDs {
		if menuID <= 0 {
			continue
		}
		if _, ok := seen[menuID]; ok {
			continue
		}
		seen[menuID] = struct{}{}
		unique = append(unique, menuID)
	}

	if len(unique) > 0 {
		found, err := s.menus.ListByIDs(ctx, unique)
		if err != nil {
			return err
		}
		if len(found) != len(unique) {
			return apperr.ErrInvalidParams.WithMessage("存在已被删除的菜单，请刷新后重试")
		}
	}

	return s.roles.ReplaceMenus(ctx, id, unique)
}

func isReservedRoleCode(code string) bool {
	switch code {
	case model.RoleCodeSuperAdmin, model.RoleCodeAdmin, model.RoleCodeUser:
		return true
	default:
		return false
	}
}
