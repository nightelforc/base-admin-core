package repository

import (
	"context"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// MenuRepository persists the navigation tree and its permission nodes.
type MenuRepository struct {
	Base
}

// NewMenuRepository builds a menu repository.
func NewMenuRepository(base Base) *MenuRepository { return &MenuRepository{Base: base} }

// ListAll returns every menu ordered for tree assembly.
func (r *MenuRepository) ListAll(ctx context.Context) ([]*model.Menu, error) {
	menus := make([]*model.Menu, 0, 32)
	err := r.DB(ctx).Order("parent_id ASC, sort ASC, id ASC").Find(&menus).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return menus, nil
}

// ListForUser returns the enabled menus granted to an account through its roles.
func (r *MenuRepository) ListForUser(ctx context.Context, userID int64) ([]*model.Menu, error) {
	menus := make([]*model.Menu, 0, 32)
	err := r.DB(ctx).
		Table(r.Tables().menus+" AS m").
		Select("DISTINCT m.*").
		Joins("JOIN "+r.Tables().roleMenus+" AS rm ON rm.menu_id = m.id").
		Joins("JOIN "+r.Tables().userRoles+" AS ur ON ur.role_id = rm.role_id").
		Where("ur.user_id = ?", userID).
		Where("m.status = ?", model.StatusEnabled).
		Where("m.deleted_at IS NULL").
		Order("m.parent_id ASC, m.sort ASC, m.id ASC").
		Scan(&menus).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return menus, nil
}

// FindByID loads a menu by primary key.
func (r *MenuRepository) FindByID(ctx context.Context, id int64) (*model.Menu, error) {
	var menu model.Menu
	if err := r.DB(ctx).First(&menu, "id = ?", id).Error; err != nil {
		return nil, translate(err, apperr.ErrMenuNotFound)
	}
	return &menu, nil
}

// ExistsByName reports whether the route name is taken, ignoring excludeID.
func (r *MenuRepository) ExistsByName(ctx context.Context, name string, excludeID int64) (bool, error) {
	q := r.DB(ctx).Model(&model.Menu{}).Where("name = ?", name)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, apperr.ErrInternal.Wrap(err)
	}
	return count > 0, nil
}

// Create inserts a menu.
func (r *MenuRepository) Create(ctx context.Context, menu *model.Menu) error {
	if err := r.DB(ctx).Create(menu).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.ErrMenuNameTaken
		}
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// UpdateFields writes the given columns of one menu.
func (r *MenuRepository) UpdateFields(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	result := r.DB(ctx).Model(&model.Menu{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return apperr.ErrMenuNameTaken
		}
		return apperr.ErrInternal.Wrap(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.ErrMenuNotFound
	}
	return nil
}

// Delete soft-deletes a menu and revokes its grants atomically.
func (r *MenuRepository) Delete(ctx context.Context, id int64) error {
	return r.Transaction(ctx, func(tx Base) error {
		if err := tx.DB(ctx).Where("menu_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		result := tx.DB(ctx).Delete(&model.Menu{}, id)
		if result.Error != nil {
			return apperr.ErrInternal.Wrap(result.Error)
		}
		if result.RowsAffected == 0 {
			return apperr.ErrMenuNotFound
		}
		return nil
	})
}

// CountChildren counts the direct children of a menu.
func (r *MenuRepository) CountChildren(ctx context.Context, parentID int64) (int64, error) {
	var count int64
	if err := r.DB(ctx).Model(&model.Menu{}).Where("parent_id = ?", parentID).Count(&count).Error; err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return count, nil
}

// ListByIDs loads menus by primary key in one query.
func (r *MenuRepository) ListByIDs(ctx context.Context, ids []int64) ([]*model.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	menus := make([]*model.Menu, 0, len(ids))
	if err := r.DB(ctx).Where("id IN ?", ids).Find(&menus).Error; err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return menus, nil
}
