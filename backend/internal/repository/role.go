package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// RoleRepository persists roles and their menu grants.
type RoleRepository struct {
	Base
}

// NewRoleRepository builds a role repository.
func NewRoleRepository(base Base) *RoleRepository { return &RoleRepository{Base: base} }

// RoleFilter narrows a role listing.
type RoleFilter struct {
	Keyword string
	Status  *int16
	Page    Page
}

// FindByID loads a role by primary key.
func (r *RoleRepository) FindByID(ctx context.Context, id int64) (*model.Role, error) {
	var role model.Role
	if err := r.DB(ctx).First(&role, "id = ?", id).Error; err != nil {
		return nil, translate(err, apperr.ErrRoleNotFound)
	}
	return &role, nil
}

// FindByCode loads a role by its stable identifier.
func (r *RoleRepository) FindByCode(ctx context.Context, code string) (*model.Role, error) {
	var role model.Role
	if err := r.DB(ctx).First(&role, "code = ?", code).Error; err != nil {
		return nil, translate(err, apperr.ErrRoleNotFound)
	}
	return &role, nil
}

// ExistsByCode reports whether the code is taken, ignoring excludeID.
func (r *RoleRepository) ExistsByCode(ctx context.Context, code string, excludeID int64) (bool, error) {
	q := r.DB(ctx).Model(&model.Role{}).Where("code = ?", code)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, apperr.ErrInternal.Wrap(err)
	}
	return count > 0, nil
}

// Create inserts a role.
func (r *RoleRepository) Create(ctx context.Context, role *model.Role) error {
	if err := r.DB(ctx).Create(role).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.ErrRoleCodeTaken
		}
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// UpdateFields writes the given columns of one role.
func (r *RoleRepository) UpdateFields(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	result := r.DB(ctx).Model(&model.Role{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return apperr.ErrRoleCodeTaken
		}
		return apperr.ErrInternal.Wrap(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.ErrRoleNotFound
	}
	return nil
}

// Delete soft-deletes a role and drops its menu grants atomically.
func (r *RoleRepository) Delete(ctx context.Context, id int64) error {
	return r.Transaction(ctx, func(tx Base) error {
		if err := tx.DB(ctx).Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		result := tx.DB(ctx).Delete(&model.Role{}, id)
		if result.Error != nil {
			return apperr.ErrInternal.Wrap(result.Error)
		}
		if result.RowsAffected == 0 {
			return apperr.ErrRoleNotFound
		}
		return nil
	})
}

// List returns one page of roles and the total count.
func (r *RoleRepository) List(ctx context.Context, filter RoleFilter) ([]*model.Role, int64, error) {
	page := filter.Page.Normalize()

	build := func() *gorm.DB {
		q := r.DB(ctx).Model(&model.Role{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			q = q.Where("code ILIKE ? OR name ILIKE ?", like, like)
		}
		if filter.Status != nil {
			q = q.Where("status = ?", *filter.Status)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	roles := make([]*model.Role, 0, page.Limit)
	if err := build().Order("sort ASC, id ASC").Offset(page.Offset).Limit(page.Limit).Find(&roles).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return roles, total, nil
}

// AllEnabled returns every active role, used to populate select options.
func (r *RoleRepository) AllEnabled(ctx context.Context) ([]*model.Role, error) {
	roles := make([]*model.Role, 0, 8)
	err := r.DB(ctx).Where("status = ?", model.StatusEnabled).Order("sort ASC, id ASC").Find(&roles).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return roles, nil
}

// MenuIDsOfRole returns the granted menu identifiers.
func (r *RoleRepository) MenuIDsOfRole(ctx context.Context, roleID int64) ([]int64, error) {
	ids := make([]int64, 0, 16)
	err := r.DB(ctx).Table(r.Tables().roleMenus).Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return ids, nil
}

// ReplaceMenus overwrites the menu grants of a role atomically.
func (r *RoleRepository) ReplaceMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	return r.Transaction(ctx, func(tx Base) error {
		if err := tx.DB(ctx).Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		if len(menuIDs) == 0 {
			return nil
		}
		now := time.Now().UTC()
		links := make([]model.RoleMenu, 0, len(menuIDs))
		for _, menuID := range menuIDs {
			links = append(links, model.RoleMenu{RoleID: roleID, MenuID: menuID, CreatedAt: now})
		}
		if err := tx.DB(ctx).Create(&links).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		return nil
	})
}

// DeleteRoleMenus removes every menu grant of a role, used when seeding.
func (r *RoleRepository) DeleteRoleMenus(ctx context.Context, roleID int64) error {
	if err := r.DB(ctx).Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// GrantMenus by code inserts missing grants for a role without touching the
// existing ones. Seeds use it to stay idempotent.
func (r *RoleRepository) GrantMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	if len(menuIDs) == 0 {
		return nil
	}
	existing := make(map[int64]struct{})
	current, err := r.MenuIDsOfRole(ctx, roleID)
	if err != nil {
		return err
	}
	for _, id := range current {
		existing[id] = struct{}{}
	}
	now := time.Now().UTC()
	links := make([]model.RoleMenu, 0, len(menuIDs))
	for _, menuID := range menuIDs {
		if _, ok := existing[menuID]; ok {
			continue
		}
		links = append(links, model.RoleMenu{RoleID: roleID, MenuID: menuID, CreatedAt: now})
	}
	if len(links) == 0 {
		return nil
	}
	if err := r.DB(ctx).Create(&links).Error; err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// CountUsers counts accounts currently holding the role.
func (r *RoleRepository) CountUsers(ctx context.Context, roleID int64) (int64, error) {
	var count int64
	if err := r.DB(ctx).Table(r.Tables().userRoles).Where("role_id = ?", roleID).Count(&count).Error; err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return count, nil
}

// CodesOfUser returns the codes of the accounts' active roles.
func (r *RoleRepository) CodesOfUser(ctx context.Context, userID int64) ([]string, error) {
	codes := make([]string, 0, 4)
	err := r.DB(ctx).
		Table(r.Tables().roles+" AS r").
		Select("DISTINCT r.code").
		Joins("JOIN "+r.Tables().userRoles+" AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ?", userID).
		Where("r.deleted_at IS NULL").
		Where("r.status = ?", model.StatusEnabled).
		Pluck("r.code", &codes).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return codes, nil
}
