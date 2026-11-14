package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/model"
)

// UserRepository persists administrator accounts and their role assignments.
type UserRepository struct {
	Base
}

// NewUserRepository builds a user repository.
func NewUserRepository(base Base) *UserRepository { return &UserRepository{Base: base} }

// UserFilter narrows a user listing.
type UserFilter struct {
	Keyword string
	Status  *int16
	RoleID  int64
	Page    Page
}

// FindByID loads an account by primary key.
func (r *UserRepository) FindByID(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := r.DB(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		return nil, translate(err, apperr.ErrUserNotFound)
	}
	return &user, nil
}

// FindByUsername loads an account by its login name.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.DB(ctx).First(&user, "username = ?", username).Error
	if err != nil {
		return nil, translate(err, apperr.ErrUserNotFound)
	}
	return &user, nil
}

// ExistsByUsername reports whether the login name is taken, ignoring excludeID.
func (r *UserRepository) ExistsByUsername(ctx context.Context, username string, excludeID int64) (bool, error) {
	q := r.DB(ctx).Model(&model.User{}).Where("username = ?", username)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, apperr.ErrInternal.Wrap(err)
	}
	return count > 0, nil
}

// Create inserts an account.
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	if err := r.DB(ctx).Create(user).Error; err != nil {
		if isUniqueViolation(err) {
			return apperr.ErrUsernameTaken
		}
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// UpdateFields writes the given columns of one account.
func (r *UserRepository) UpdateFields(ctx context.Context, id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	result := r.DB(ctx).Model(&model.User{}).Where("id = ?", id).Updates(fields)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return apperr.ErrUsernameTaken
		}
		return apperr.ErrInternal.Wrap(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.ErrUserNotFound
	}
	return nil
}

// Delete soft-deletes an account and drops its role assignments.
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	return r.Transaction(ctx, func(tx Base) error {
		if err := tx.DB(ctx).Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		result := tx.DB(ctx).Delete(&model.User{}, id)
		if result.Error != nil {
			return apperr.ErrInternal.Wrap(result.Error)
		}
		if result.RowsAffected == 0 {
			return apperr.ErrUserNotFound
		}
		return nil
	})
}

// List returns one page of accounts and the total count.
func (r *UserRepository) List(ctx context.Context, filter UserFilter) ([]*model.User, int64, error) {
	page := filter.Page.Normalize()

	build := func() *gorm.DB {
		q := r.DB(ctx).Model(&model.User{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			q = q.Where("username ILIKE ? OR nickname ILIKE ? OR email ILIKE ?", like, like, like)
		}
		if filter.Status != nil {
			q = q.Where("status = ?", *filter.Status)
		}
		if filter.RoleID > 0 {
			sub := r.DB(ctx).Table(r.Tables().userRoles).Select("user_id").Where("role_id = ?", filter.RoleID)
			q = q.Where("id IN (?)", sub)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	users := make([]*model.User, 0, page.Limit)
	if err := build().Order("id DESC").Offset(page.Offset).Limit(page.Limit).Find(&users).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return users, total, nil
}

// ListByIDs loads accounts by primary key in one query.
func (r *UserRepository) ListByIDs(ctx context.Context, ids []int64) ([]*model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	users := make([]*model.User, 0, len(ids))
	if err := r.DB(ctx).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return users, nil
}

// TouchLogin records the successful login metadata.
func (r *UserRepository) TouchLogin(ctx context.Context, id int64, ip string, at time.Time) error {
	err := r.DB(ctx).Model(&model.User{}).Where("id = ?", id).Updates(map[string]any{
		"last_login_at": at,
		"last_login_ip": ip,
	}).Error
	if err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// CountByRoleID counts accounts currently holding a role.
func (r *UserRepository) CountByRoleID(ctx context.Context, roleID int64) (int64, error) {
	var count int64
	err := r.DB(ctx).Table(r.Tables().userRoles).Where("role_id = ?", roleID).Count(&count).Error
	if err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return count, nil
}

// RolesOfUser returns the active roles of an account.
func (r *UserRepository) RolesOfUser(ctx context.Context, userID int64) ([]*model.Role, error) {
	roles := make([]*model.Role, 0, 4)
	err := r.DB(ctx).
		Table(r.Tables().roles+" AS r").
		Select("r.*").
		Joins("JOIN "+r.Tables().userRoles+" AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ?", userID).
		Where("r.deleted_at IS NULL").
		Order("r.sort ASC, r.id ASC").
		Scan(&roles).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return roles, nil
}

// RolesByUserIDs returns the roles of many accounts at once, keyed by user ID.
// It exists to keep list endpoints free of N+1 queries.
func (r *UserRepository) RolesByUserIDs(ctx context.Context, userIDs []int64) (map[int64][]*model.Role, error) {
	result := make(map[int64][]*model.Role, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	type row struct {
		UserID int64
		RoleID int64
		Code   string
		Name   string
		Sort   int
		Status int16
	}
	rows := make([]row, 0, len(userIDs))
	err := r.DB(ctx).
		Table(r.Tables().userRoles+" AS ur").
		Select("ur.user_id, r.id AS role_id, r.code, r.name, r.sort, r.status").
		Joins("JOIN "+r.Tables().roles+" AS r ON r.id = ur.role_id AND r.deleted_at IS NULL").
		Where("ur.user_id IN ?", userIDs).
		Order("r.sort ASC, r.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}

	for _, item := range rows {
		result[item.UserID] = append(result[item.UserID], &model.Role{
			ID:     item.RoleID,
			Code:   item.Code,
			Name:   item.Name,
			Sort:   item.Sort,
			Status: item.Status,
		})
	}
	return result, nil
}

// RoleIDsOfUser returns the assigned role identifiers.
func (r *UserRepository) RoleIDsOfUser(ctx context.Context, userID int64) ([]int64, error) {
	ids := make([]int64, 0, 4)
	err := r.DB(ctx).Table(r.Tables().userRoles).Where("user_id = ?", userID).Pluck("role_id", &ids).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return ids, nil
}

// ReplaceRoles overwrites the role assignments of an account atomically.
func (r *UserRepository) ReplaceRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	return r.Transaction(ctx, func(tx Base) error {
		if err := tx.DB(ctx).Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		if len(roleIDs) == 0 {
			return nil
		}
		now := time.Now().UTC()
		links := make([]model.UserRole, 0, len(roleIDs))
		for _, roleID := range roleIDs {
			links = append(links, model.UserRole{UserID: userID, RoleID: roleID, CreatedAt: now})
		}
		if err := tx.DB(ctx).Create(&links).Error; err != nil {
			return apperr.ErrInternal.Wrap(err)
		}
		return nil
	})
}

// PermissionCodes returns the distinct permission codes granted to an account
// through its roles.
func (r *UserRepository) PermissionCodes(ctx context.Context, userID int64) ([]string, error) {
	codes := make([]string, 0, 16)
	err := r.DB(ctx).
		Table(r.Tables().menus+" AS m").
		Select("DISTINCT m.permission").
		Joins("JOIN "+r.Tables().roleMenus+" AS rm ON rm.menu_id = m.id").
		Joins("JOIN "+r.Tables().userRoles+" AS ur ON ur.role_id = rm.role_id").
		Where("ur.user_id = ?", userID).
		Where("m.permission <> ''").
		Where("m.status = ?", model.StatusEnabled).
		Where("m.deleted_at IS NULL").
		Order("m.permission ASC").
		Pluck("m.permission", &codes).Error
	if err != nil {
		return nil, apperr.ErrInternal.Wrap(err)
	}
	return codes, nil
}

// IsSuperAdmin reports whether the account holds the built-in super admin role,
// which bypasses permission checks.
func (r *UserRepository) IsSuperAdmin(ctx context.Context, userID int64) (bool, error) {
	var count int64
	err := r.DB(ctx).
		Table(r.Tables().userRoles+" AS ur").
		Joins("JOIN "+r.Tables().roles+" AS r ON r.id = ur.role_id AND r.deleted_at IS NULL").
		Where("ur.user_id = ?", userID).
		Where("r.code = ?", model.RoleCodeSuperAdmin).
		Where("r.status = ?", model.StatusEnabled).
		Count(&count).Error
	if err != nil {
		return false, apperr.ErrInternal.Wrap(err)
	}
	return count > 0, nil
}

// EnsureRoleAssignment grants roleID to userID if it is not granted yet. It is
// used by the first-run bootstrap and is safe to call repeatedly.
func (r *UserRepository) EnsureRoleAssignment(ctx context.Context, userID, roleID int64) error {
	var count int64
	if err := r.DB(ctx).Table(r.Tables().userRoles).
		Where("user_id = ? AND role_id = ?", userID, roleID).Count(&count).Error; err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	if count > 0 {
		return nil
	}
	err := r.DB(ctx).Create(&model.UserRole{UserID: userID, RoleID: roleID, CreatedAt: time.Now().UTC()}).Error
	if err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}
