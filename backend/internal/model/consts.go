// Package model contains the persistence models of the application.
//
// Naming note: no model implements gorm's Tabler interface. Table names are
// produced by GORM's naming strategy, which prepends the configurable
// DB_TABLE_PREFIX, so a deployment can namespace every table it owns (default
// `base_`) inside a shared schema.
package model

// Record status values shared by users, roles and menus.
const (
	StatusDisabled int16 = 0
	StatusEnabled  int16 = 1
)

// Built-in role codes.
const (
	RoleCodeSuperAdmin = "super_admin"
	RoleCodeAdmin      = "admin"
	RoleCodeUser       = "user"
)

// Menu type values. The type drives how the frontend renders the record.
const (
	MenuTypeCatalog  = "catalog"  // layout grouping node (component: BasicLayout)
	MenuTypeMenu     = "menu"     // regular page inside the layout
	MenuTypeButton   = "button"   // permission-only node, never routed
	MenuTypeEmbedded = "embedded" // page rendered inside an iframe (component: IFrameView)
	MenuTypeLink     = "link"     // external link opened in a new tab
)

// Permission codes checked by the API layer. The database stores the same
// strings on button menus, so a role granted the button also gets the API
// permission.
const (
	PermUserList          = "system:user:list"
	PermUserCreate        = "system:user:create"
	PermUserUpdate        = "system:user:update"
	PermUserDelete        = "system:user:delete"
	PermUserResetPassword = "system:user:reset-password"

	PermRoleList   = "system:role:list"
	PermRoleCreate = "system:role:create"
	PermRoleUpdate = "system:role:update"
	PermRoleDelete = "system:role:delete"
	PermRoleAssign = "system:role:assign"

	PermMenuList   = "system:menu:list"
	PermMenuCreate = "system:menu:create"
	PermMenuUpdate = "system:menu:update"
	PermMenuDelete = "system:menu:delete"

	PermLogList = "system:log:list"
)

// IsValidMenuType reports whether t is a supported menu type.
func IsValidMenuType(t string) bool {
	switch t {
	case MenuTypeCatalog, MenuTypeMenu, MenuTypeButton, MenuTypeEmbedded, MenuTypeLink:
		return true
	default:
		return false
	}
}
