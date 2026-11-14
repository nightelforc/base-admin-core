package model

import (
	"time"

	"gorm.io/gorm"
)

// Role is a named permission bundle assigned to users and menus.
type Role struct {
	ID          int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Code        string         `gorm:"column:code;size:64;not null" json:"code"`
	Name        string         `gorm:"column:name;size:64;not null" json:"name"`
	Description string         `gorm:"column:description;size:255;not null;default:''" json:"description"`
	Sort        int            `gorm:"column:sort;not null;default:0" json:"sort"`
	Status      int16          `gorm:"column:status;not null;default:1" json:"status"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at" json:"-"`
}

// IsBuiltIn reports whether the role is protected from deletion. Built-in roles
// anchor the permission model, so removing them would strand accounts.
func (r *Role) IsBuiltIn() bool {
	switch r.Code {
	case RoleCodeSuperAdmin, RoleCodeAdmin, RoleCodeUser:
		return true
	default:
		return false
	}
}

// IsSuperAdmin reports whether the role bypasses permission checks.
func (r *Role) IsSuperAdmin() bool { return r.Code == RoleCodeSuperAdmin }

// UserRole links accounts to roles.
type UserRole struct {
	UserID    int64     `gorm:"column:user_id;primaryKey" json:"userId"`
	RoleID    int64     `gorm:"column:role_id;primaryKey" json:"roleId"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}

// RoleMenu links roles to menus (pages and permission buttons).
type RoleMenu struct {
	RoleID    int64     `gorm:"column:role_id;primaryKey" json:"roleId"`
	MenuID    int64     `gorm:"column:menu_id;primaryKey" json:"menuId"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}
