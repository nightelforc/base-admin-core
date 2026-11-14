package model

import (
	"time"

	"gorm.io/gorm"
)

// User is an administrator account.
//
// Deleted rows keep their username (the unique index is partial on
// deleted_at IS NULL), so a deleted account can be recreated with the same name.
type User struct {
	ID           int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Username     string         `gorm:"column:username;size:64;not null" json:"username"`
	PasswordHash string         `gorm:"column:password_hash;size:255;not null" json:"-"`
	Nickname     string         `gorm:"column:nickname;size:64;not null;default:''" json:"nickname"`
	Email        string         `gorm:"column:email;size:128;not null;default:''" json:"email"`
	Phone        string         `gorm:"column:phone;size:32;not null;default:''" json:"phone"`
	Avatar       string         `gorm:"column:avatar;size:512;not null;default:''" json:"avatar"`
	Remark       string         `gorm:"column:remark;size:255;not null;default:''" json:"remark"`
	Status       int16          `gorm:"column:status;not null;default:1" json:"status"`
	LastLoginAt  *time.Time     `gorm:"column:last_login_at" json:"lastLoginAt"`
	LastLoginIP  string         `gorm:"column:last_login_ip;size:45;not null;default:''" json:"lastLoginIp"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at" json:"-"`
}

// IsEnabled reports whether the account may sign in.
func (u *User) IsEnabled() bool { return u.Status == StatusEnabled }

// DisplayName returns the nickname, falling back to the username.
func (u *User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}
