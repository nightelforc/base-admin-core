-- 0002_seed_rbac.sql — baseline roles, menus and grants.
--
-- The rows use fixed identifiers so grants can reference them literally. The
-- sequences are advanced afterwards so later inserts do not collide.
-- The administrator account itself is created by the application on first
-- start (BOOTSTRAP_ADMIN_USERNAME / BOOTSTRAP_ADMIN_PASSWORD).

INSERT INTO {{prefix}}roles (id, code, name, description, sort, status) VALUES
    (1, 'super_admin', '超级管理员', '内置角色，拥有全部权限', 1, 1),
    (2, 'admin',       '管理员',     '内置角色，拥有系统管理权限', 2, 1),
    (3, 'user',        '普通用户',   '内置角色，只能访问工作台', 3, 1)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Navigation tree
-- ---------------------------------------------------------------------------
INSERT INTO {{prefix}}menus
    (id, parent_id, type, name, title, path, component, redirect, icon, link, permission, sort, visible, keep_alive, affix_tab, status)
VALUES
    (10, 0, 'catalog', 'Dashboard', '概览', '/dashboard', '', '/dashboard/workspace', 'lucide:layout-dashboard', '', '', 1, true, false, false, 1),
    (11, 10, 'menu', 'DashboardWorkspace', '工作台', '/dashboard/workspace', '/dashboard/index', '', 'lucide:home', '', '', 1, true, true, true, 1),

    (20, 0, 'catalog', 'System', '系统管理', '/system', '', '/system/user', 'lucide:settings', '', '', 10, true, false, false, 1),
    (21, 20, 'menu', 'SystemUser', '用户管理', '/system/user', '/system/user/index', '', 'lucide:users', '', 'system:user:list', 1, true, true, false, 1),
    (22, 20, 'menu', 'SystemRole', '角色管理', '/system/role', '/system/role/index', '', 'lucide:shield-check', '', 'system:role:list', 2, true, true, false, 1),
    (23, 20, 'menu', 'SystemMenu', '菜单管理', '/system/menu', '/system/menu/index', '', 'lucide:list-tree', '', 'system:menu:list', 3, true, true, false, 1),
    (24, 20, 'menu', 'SystemLoginLog', '登录日志', '/system/login-log', '/system/log/login/index', '', 'lucide:log-in', '', 'system:log:list', 4, true, true, false, 1),
    (25, 20, 'menu', 'SystemOperationLog', '操作日志', '/system/operation-log', '/system/log/operation/index', '', 'lucide:activity', '', 'system:log:list', 5, true, true, false, 1),

    (2101, 21, 'button', 'SystemUserCreate', '新增用户', '', '', '', '', '', 'system:user:create', 1, true, false, false, 1),
    (2102, 21, 'button', 'SystemUserUpdate', '编辑用户', '', '', '', '', '', 'system:user:update', 2, true, false, false, 1),
    (2103, 21, 'button', 'SystemUserDelete', '删除用户', '', '', '', '', '', 'system:user:delete', 3, true, false, false, 1),
    (2104, 21, 'button', 'SystemUserResetPassword', '重置密码', '', '', '', '', '', 'system:user:reset-password', 4, true, false, false, 1),

    (2201, 22, 'button', 'SystemRoleCreate', '新增角色', '', '', '', '', '', 'system:role:create', 1, true, false, false, 1),
    (2202, 22, 'button', 'SystemRoleUpdate', '编辑角色', '', '', '', '', '', 'system:role:update', 2, true, false, false, 1),
    (2203, 22, 'button', 'SystemRoleDelete', '删除角色', '', '', '', '', '', 'system:role:delete', 3, true, false, false, 1),
    (2204, 22, 'button', 'SystemRoleAssign', '分配权限', '', '', '', '', '', 'system:role:assign', 4, true, false, false, 1),

    (2301, 23, 'button', 'SystemMenuCreate', '新增菜单', '', '', '', '', '', 'system:menu:create', 1, true, false, false, 1),
    (2302, 23, 'button', 'SystemMenuUpdate', '编辑菜单', '', '', '', '', '', 'system:menu:update', 2, true, false, false, 1),
    (2303, 23, 'button', 'SystemMenuDelete', '删除菜单', '', '', '', '', '', 'system:menu:delete', 3, true, false, false, 1)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Grants: super_admin gets everything, admin gets the system pages (menu
-- management excluded), user only gets the workspace.
-- ---------------------------------------------------------------------------
INSERT INTO {{prefix}}role_menus (role_id, menu_id) VALUES
    (2, 10), (2, 11),
    (2, 20), (2, 21), (2, 22), (2, 24), (2, 25),
    (2, 2101), (2, 2102), (2, 2103), (2, 2104),
    (2, 2201), (2, 2202), (2, 2203), (2, 2204),
    (3, 10), (3, 11)
ON CONFLICT DO NOTHING;

INSERT INTO {{prefix}}role_menus (role_id, menu_id)
SELECT 1, id FROM {{prefix}}menus WHERE deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Advance sequences past the fixed identifiers.
-- ---------------------------------------------------------------------------
SELECT setval(pg_get_serial_sequence('{{prefix}}roles', 'id'),
              GREATEST((SELECT COALESCE(MAX(id), 1) FROM {{prefix}}roles), 1000));
SELECT setval(pg_get_serial_sequence('{{prefix}}menus', 'id'),
              GREATEST((SELECT COALESCE(MAX(id), 1) FROM {{prefix}}menus), 10000));
