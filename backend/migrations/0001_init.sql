-- 0001_init.sql — core schema of the admin template.
--
-- Every identifier is templated with {{prefix}} (DB_TABLE_PREFIX) so several
-- deployments can live in one PostgreSQL schema. The placeholder is replaced by
-- the migration runner before the script is executed; do not write table names
-- without it.

CREATE TABLE IF NOT EXISTS {{prefix}}users (
    id            bigserial    PRIMARY KEY,
    username      varchar(64)  NOT NULL,
    password_hash varchar(255) NOT NULL,
    nickname      varchar(64)  NOT NULL DEFAULT '',
    email         varchar(128) NOT NULL DEFAULT '',
    phone         varchar(32)  NOT NULL DEFAULT '',
    avatar        varchar(512) NOT NULL DEFAULT '',
    remark        varchar(255) NOT NULL DEFAULT '',
    status        smallint     NOT NULL DEFAULT 1,
    last_login_at timestamptz,
    last_login_ip varchar(45)  NOT NULL DEFAULT '',
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

COMMENT ON TABLE {{prefix}}users IS 'administrator accounts';
COMMENT ON COLUMN {{prefix}}users.status IS '1 enabled, 0 disabled';
COMMENT ON COLUMN {{prefix}}users.deleted_at IS 'soft delete timestamp';

CREATE UNIQUE INDEX IF NOT EXISTS ux_{{prefix}}users_username
    ON {{prefix}}users (username) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_{{prefix}}users_deleted_at ON {{prefix}}users (deleted_at);
CREATE INDEX IF NOT EXISTS ix_{{prefix}}users_status ON {{prefix}}users (status);

CREATE TABLE IF NOT EXISTS {{prefix}}roles (
    id          bigserial    PRIMARY KEY,
    code        varchar(64)  NOT NULL,
    name        varchar(64)  NOT NULL,
    description varchar(255) NOT NULL DEFAULT '',
    sort        integer      NOT NULL DEFAULT 0,
    status      smallint     NOT NULL DEFAULT 1,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

COMMENT ON TABLE {{prefix}}roles IS 'role definitions; built-in codes are super_admin/admin/user';

CREATE UNIQUE INDEX IF NOT EXISTS ux_{{prefix}}roles_code
    ON {{prefix}}roles (code) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_{{prefix}}roles_deleted_at ON {{prefix}}roles (deleted_at);

CREATE TABLE IF NOT EXISTS {{prefix}}menus (
    id          bigserial    PRIMARY KEY,
    parent_id   bigint       NOT NULL DEFAULT 0,
    type        varchar(16)  NOT NULL DEFAULT 'menu',
    name        varchar(64)  NOT NULL,
    title       varchar(64)  NOT NULL,
    path        varchar(255) NOT NULL DEFAULT '',
    component   varchar(255) NOT NULL DEFAULT '',
    redirect    varchar(255) NOT NULL DEFAULT '',
    icon        varchar(64)  NOT NULL DEFAULT '',
    link        varchar(512) NOT NULL DEFAULT '',
    permission  varchar(128) NOT NULL DEFAULT '',
    sort        integer      NOT NULL DEFAULT 0,
    visible     boolean      NOT NULL DEFAULT true,
    keep_alive  boolean      NOT NULL DEFAULT false,
    affix_tab   boolean      NOT NULL DEFAULT false,
    status      smallint     NOT NULL DEFAULT 1,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

COMMENT ON TABLE {{prefix}}menus IS 'navigation tree and permission registry';
COMMENT ON COLUMN {{prefix}}menus.type IS 'catalog | menu | button | embedded | link';
COMMENT ON COLUMN {{prefix}}menus.component IS 'frontend view path, e.g. /system/user/index, resolved under src/views';
COMMENT ON COLUMN {{prefix}}menus.permission IS 'permission code checked by the API, e.g. system:user:create';

CREATE UNIQUE INDEX IF NOT EXISTS ux_{{prefix}}menus_name
    ON {{prefix}}menus (name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_{{prefix}}menus_parent_id ON {{prefix}}menus (parent_id);
CREATE INDEX IF NOT EXISTS ix_{{prefix}}menus_deleted_at ON {{prefix}}menus (deleted_at);

CREATE TABLE IF NOT EXISTS {{prefix}}user_roles (
    user_id    bigint      NOT NULL REFERENCES {{prefix}}users (id) ON DELETE CASCADE,
    role_id    bigint      NOT NULL REFERENCES {{prefix}}roles (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

COMMENT ON TABLE {{prefix}}user_roles IS 'account to role assignment';

CREATE INDEX IF NOT EXISTS ix_{{prefix}}user_roles_role_id ON {{prefix}}user_roles (role_id);

CREATE TABLE IF NOT EXISTS {{prefix}}role_menus (
    role_id    bigint      NOT NULL REFERENCES {{prefix}}roles (id) ON DELETE CASCADE,
    menu_id    bigint      NOT NULL REFERENCES {{prefix}}menus (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, menu_id)
);

COMMENT ON TABLE {{prefix}}role_menus IS 'role to menu (page and permission) grant';

CREATE INDEX IF NOT EXISTS ix_{{prefix}}role_menus_menu_id ON {{prefix}}role_menus (menu_id);

CREATE TABLE IF NOT EXISTS {{prefix}}login_logs (
    id         bigserial    PRIMARY KEY,
    user_id    bigint       NOT NULL DEFAULT 0,
    username   varchar(64)  NOT NULL DEFAULT '',
    ip         varchar(45)  NOT NULL DEFAULT '',
    user_agent varchar(512) NOT NULL DEFAULT '',
    success    boolean      NOT NULL DEFAULT false,
    message    varchar(255) NOT NULL DEFAULT '',
    created_at timestamptz  NOT NULL DEFAULT now()
);

COMMENT ON TABLE {{prefix}}login_logs IS 'authentication attempts audit trail';

CREATE INDEX IF NOT EXISTS ix_{{prefix}}login_logs_created_at ON {{prefix}}login_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS ix_{{prefix}}login_logs_username ON {{prefix}}login_logs (username);

CREATE TABLE IF NOT EXISTS {{prefix}}operation_logs (
    id           bigserial    PRIMARY KEY,
    user_id      bigint       NOT NULL DEFAULT 0,
    username     varchar(64)  NOT NULL DEFAULT '',
    method       varchar(16)  NOT NULL DEFAULT '',
    path         varchar(255) NOT NULL DEFAULT '',
    ip           varchar(45)  NOT NULL DEFAULT '',
    user_agent   varchar(512) NOT NULL DEFAULT '',
    status_code  integer      NOT NULL DEFAULT 0,
    latency_ms   bigint       NOT NULL DEFAULT 0,
    request_body varchar(4096) NOT NULL DEFAULT '',
    created_at   timestamptz  NOT NULL DEFAULT now()
);

COMMENT ON TABLE {{prefix}}operation_logs IS 'mutating API calls audit trail';

CREATE INDEX IF NOT EXISTS ix_{{prefix}}operation_logs_created_at ON {{prefix}}operation_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS ix_{{prefix}}operation_logs_user_id ON {{prefix}}operation_logs (user_id);
