package repository

import (
	"context"
	"time"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
)

// Count returns the number of accounts (soft-deleted rows excluded).
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.User{}).Count(&total).Error; err != nil {
		return 0, translate(err, nil)
	}
	return total, nil
}

// CountByStatus returns how many accounts are enabled or disabled.
func (r *UserRepository) CountByStatus(ctx context.Context, status int16) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.User{}).Where("status = ?", status).Count(&total).Error; err != nil {
		return 0, translate(err, nil)
	}
	return total, nil
}

// CountEnabledByRoleID counts enabled accounts holding a given role. It backs
// the "never remove the last super administrator" invariant.
func (r *UserRepository) CountEnabledByRoleID(ctx context.Context, roleID int64) (int64, error) {
	var total int64
	err := r.DB(ctx).
		Table(r.Tables().users+" AS u").
		Joins("JOIN "+r.Tables().userRoles+" AS ur ON ur.user_id = u.id").
		Where("ur.role_id = ? AND u.status = ? AND u.deleted_at IS NULL", roleID, model.StatusEnabled).
		Count(&total).Error
	if err != nil {
		return 0, translate(err, nil)
	}
	return total, nil
}

// Count returns the number of roles.
func (r *RoleRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.Role{}).Count(&total).Error; err != nil {
		return 0, translate(err, nil)
	}
	return total, nil
}

// Count returns the number of menu entries, buttons included.
func (r *MenuRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.Menu{}).Count(&total).Error; err != nil {
		return 0, translate(err, nil)
	}
	return total, nil
}

// AllPermissionCodes lists every distinct permission code the catalog defines.
// It is what a super administrator receives instead of an enumerated grant.
func (r *MenuRepository) AllPermissionCodes(ctx context.Context) ([]string, error) {
	codes := make([]string, 0, 32)
	err := r.DB(ctx).
		Model(&model.Menu{}).
		Where("permission <> '' AND status = ?", model.StatusEnabled).
		Distinct("permission").
		Order("permission ASC").
		Pluck("permission", &codes).Error
	if err != nil {
		return nil, translate(err, nil)
	}
	return codes, nil
}

// TrendPoint is one day of the login activity series.
type TrendPoint struct {
	Date    string `gorm:"column:date" json:"date"`
	Total   int64  `gorm:"column:total" json:"total"`
	Success int64  `gorm:"column:success" json:"success"`
	Failed  int64  `gorm:"column:failed" json:"failed"`
}

// LoginTrend aggregates login attempts per day since the given instant.
func (r *LogRepository) LoginTrend(ctx context.Context, since time.Time) ([]TrendPoint, error) {
	points := make([]TrendPoint, 0, 7)
	err := r.DB(ctx).
		Table(r.Tables().loginLogs).
		Select(`to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS date,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE success) AS success,
			COUNT(*) FILTER (WHERE NOT success) AS failed`).
		Where("created_at >= ?", since).
		Group("date").
		Order("date ASC").
		Scan(&points).Error
	if err != nil {
		return nil, translate(err, nil)
	}
	return points, nil
}
