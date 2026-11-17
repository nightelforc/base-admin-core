package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// LogRepository persists login and operation audit trails.
type LogRepository struct {
	Base
}

// NewLogRepository builds a log repository.
func NewLogRepository(base Base) *LogRepository { return &LogRepository{Base: base} }

// LogFilter narrows an audit-log listing.
type LogFilter struct {
	Keyword string
	UserID  int64
	Success *bool
	From    *time.Time
	To      *time.Time
	Page    Page
}

// CreateLogin inserts a login attempt record.
func (r *LogRepository) CreateLogin(ctx context.Context, entry *model.LoginLog) error {
	if err := r.DB(ctx).Create(entry).Error; err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// CreateOperation inserts an operation record.
func (r *LogRepository) CreateOperation(ctx context.Context, entry *model.OperationLog) error {
	if err := r.DB(ctx).Create(entry).Error; err != nil {
		return apperr.ErrInternal.Wrap(err)
	}
	return nil
}

// ListLogin returns one page of login attempts.
func (r *LogRepository) ListLogin(ctx context.Context, filter LogFilter) ([]*model.LoginLog, int64, error) {
	page := filter.Page.Normalize()

	build := func() *gorm.DB {
		q := r.DB(ctx).Model(&model.LoginLog{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			q = q.Where("username ILIKE ? OR ip ILIKE ?", like, like)
		}
		if filter.UserID > 0 {
			q = q.Where("user_id = ?", filter.UserID)
		}
		if filter.Success != nil {
			q = q.Where("success = ?", *filter.Success)
		}
		if filter.From != nil {
			q = q.Where("created_at >= ?", *filter.From)
		}
		if filter.To != nil {
			q = q.Where("created_at <= ?", *filter.To)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	entries := make([]*model.LoginLog, 0, page.Limit)
	if err := build().Order("id DESC").Offset(page.Offset).Limit(page.Limit).Find(&entries).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return entries, total, nil
}

// ListOperation returns one page of operation records.
func (r *LogRepository) ListOperation(ctx context.Context, filter LogFilter) ([]*model.OperationLog, int64, error) {
	page := filter.Page.Normalize()

	build := func() *gorm.DB {
		q := r.DB(ctx).Model(&model.OperationLog{})
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			q = q.Where("username ILIKE ? OR path ILIKE ?", like, like)
		}
		if filter.UserID > 0 {
			q = q.Where("user_id = ?", filter.UserID)
		}
		if filter.From != nil {
			q = q.Where("created_at >= ?", *filter.From)
		}
		if filter.To != nil {
			q = q.Where("created_at <= ?", *filter.To)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}

	entries := make([]*model.OperationLog, 0, page.Limit)
	if err := build().Order("id DESC").Offset(page.Offset).Limit(page.Limit).Find(&entries).Error; err != nil {
		return nil, 0, apperr.ErrInternal.Wrap(err)
	}
	return entries, total, nil
}

// CountLoginsSince counts login attempts newer than the given instant.
func (r *LogRepository) CountLoginsSince(ctx context.Context, since time.Time) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.LoginLog{}).Where("created_at >= ?", since).Count(&total).Error; err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return total, nil
}

// CountLoginFailuresSince counts failed login attempts newer than the instant.
func (r *LogRepository) CountLoginFailuresSince(ctx context.Context, since time.Time) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.LoginLog{}).
		Where("created_at >= ? AND success = ?", since, false).Count(&total).Error; err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return total, nil
}

// CountOperationsSince counts operation records newer than the instant.
func (r *LogRepository) CountOperationsSince(ctx context.Context, since time.Time) (int64, error) {
	var total int64
	if err := r.DB(ctx).Model(&model.OperationLog{}).Where("created_at >= ?", since).Count(&total).Error; err != nil {
		return 0, apperr.ErrInternal.Wrap(err)
	}
	return total, nil
}
