package model

import (
	"time"

	"gorm.io/gorm"
)

// Menu is a node of the navigation tree. It doubles as the permission registry:
// rows of type button carry a Permission code and are never routed.
type Menu struct {
	ID         int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ParentID   int64          `gorm:"column:parent_id;not null;default:0" json:"parentId"`
	Type       string         `gorm:"column:type;size:16;not null;default:'menu'" json:"type"`
	Name       string         `gorm:"column:name;size:64;not null" json:"name"`
	Title      string         `gorm:"column:title;size:64;not null" json:"title"`
	Path       string         `gorm:"column:path;size:255;not null;default:''" json:"path"`
	Component  string         `gorm:"column:component;size:255;not null;default:''" json:"component"`
	Redirect   string         `gorm:"column:redirect;size:255;not null;default:''" json:"redirect"`
	Icon       string         `gorm:"column:icon;size:64;not null;default:''" json:"icon"`
	Link       string         `gorm:"column:link;size:512;not null;default:''" json:"link"`
	Permission string         `gorm:"column:permission;size:128;not null;default:''" json:"permission"`
	Sort       int            `gorm:"column:sort;not null;default:0" json:"sort"`
	Visible    bool           `gorm:"column:visible;not null;default:true" json:"visible"`
	KeepAlive  bool           `gorm:"column:keep_alive;not null;default:false" json:"keepAlive"`
	AffixTab   bool           `gorm:"column:affix_tab;not null;default:false" json:"affixTab"`
	Status     int16          `gorm:"column:status;not null;default:1" json:"status"`
	CreatedAt  time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt  time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt  gorm.DeletedAt `gorm:"column:deleted_at" json:"-"`
}

// IsRoutable reports whether the node produces a route in the frontend.
func (m *Menu) IsRoutable() bool {
	return m.Type == MenuTypeCatalog || m.Type == MenuTypeMenu || m.Type == MenuTypeEmbedded || m.Type == MenuTypeLink
}

// IsEnabled reports whether the node is active.
func (m *Menu) IsEnabled() bool { return m.Status == StatusEnabled }
