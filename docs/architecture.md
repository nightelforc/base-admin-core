# 架构说明

## 1. 总体结构

```
浏览器 (Vue 3 + Naive UI, Vben 5)
        │  /api/v1  (JSON, Bearer Token)
        ▼
Gin 路由 ── middleware(RequestID → AccessLog → Recovery → CORS → BodyLimit)
        │              ↘ secured: Auth → 权限校验 → OperationLog
        ▼
handler ──► service ──► repository ──► GORM ──► PostgreSQL
                │
                ├─► Redis（会话、登录限流）
                └─► security（bcrypt / JWT）
```

后端严格分层，依赖方向单向（handler → service → repository → platform）：

| 层 | 职责 | 禁止 |
| --- | --- | --- |
| `handler` | 绑定/校验参数、调用 service、写响应 | 写业务规则、直接访问 gorm |
| `service` | 业务规则、事务边界、跨仓储编排 | 出现 `gin.Context`、拼 SQL |
| `repository` | 数据访问，唯一出现 SQL 的层 | 返回 HTTP 语义、依赖 service |
| `platform` | 连接池初始化 | 业务逻辑 |

## 2. 配置

`internal/config` 使用 `caarlos0/env` 从环境变量装配嵌套配置，并做启动期校验：

- 生产环境（`APP_ENV=production`）额外拒绝：示例/默认密钥、`CORS_ALLOWED_ORIGINS=*` 同时开启凭证、`APP_DEBUG=true`。
- `DB_TABLE_PREFIX` 只允许 `^[a-z][a-z0-9_]*$`（或空），避免拼接出非法表名。
- `JWT_SECRET` 至少 32 字符，缺失或过短直接启动失败。
- 日志启动时输出 `config.Summary()`，敏感字段已脱敏。

配置项与默认值见 `backend/.env.example`。

## 3. 表前缀与迁移

- 表前缀通过 GORM `NamingStrategy{TablePrefix}` 生效，**模型不实现 `TableName()`**（实现后会绕过前缀）。
- 需要在原生 SQL 中引用表名时，统一走 `repository.Base.Tables()`，由 `stmt.Parse(&model.X{})` 解析出真实表名。
- 迁移文件是带 `{{prefix}}` 占位符的普通 SQL（`backend/migrations/*.sql`），由 `internal/migrate` 执行：
  1. 以 `QueryExecModeSimpleProtocol` 连接（支持单文件多语句）；
  2. 对 `{{prefix}}` 做白名单校验后替换（不做字符串插值拼表名的其它形式）；
  3. 先取 `pg_advisory_lock(hashtextextended(<schema_migrations 表名>, 0))`，避免多实例并发迁移；
  4. 按文件名排序，每个文件一个事务，成功后记录 `filename / checksum / applied_at / duration_ms`；
  5. 若已应用文件的 checksum 变化则直接报错，要求新增迁移而不是改历史文件。

因此：**修改表结构请新增 `0003_xxx.sql`，不要改已发布的文件。**

`0001_init.sql` 建表，`0002_seed_rbac.sql` 初始化角色、菜单与授权（固定 ID + `ON CONFLICT DO NOTHING`，可重复执行）。

数据表（均带前缀）：

| 表 | 说明 |
| --- | --- |
| `users` | 用户，用户名唯一（`deleted_at IS NULL` 的部分唯一索引），软删除 |
| `roles` | 角色，`code` 唯一，`built_in` 标记内置角色 |
| `menus` | 菜单/按钮，树形（`parent_id`），`type` = catalog / menu / button / embedded / link |
| `user_roles` | 用户-角色关联 |
| `role_menus` | 角色-菜单关联 |
| `login_logs` | 登录日志（成功与失败都记） |
| `operation_logs` | 写操作审计日志 |
| `schema_migrations` | 迁移记录（由迁移器创建） |

## 4. 认证与会话

1. `POST /auth/login`：校验账号密码 → 检查状态 → 生成 `session_id`（随机 24 字节）→ Redis 写入 `{REDIS_KEY_PREFIX}session:{session_id}`（TTL = refresh 有效期 + 1 分钟）→ 签发 access + refresh token。
2. 受保护接口：解析 Bearer token（HS256、校验 issuer/audience/exp）→ 从 Redis 读取会话 → 会话必须存在且 `userId` 一致 → 用户必须处于启用状态。
3. `POST /auth/refresh`：校验 refresh token → 会话存在 → 用户启用 → 续期会话并**轮换**两个 token。
4. `POST /auth/logout`：删除当前会话。
5. 修改/重置密码时按用户删除全部会话（`{prefix}user_sessions:{userId}` 索引集合），实现强制下线。

登录限流：同一 `username + IP` 在 `SECURITY_LOGIN_WINDOW` 内失败 `SECURITY_LOGIN_MAX_ATTEMPTS` 次后锁定 `SECURITY_LOGIN_BLOCK_DURATION`，返回 `429` 与剩余时间。

## 5. 权限模型

- 角色 → 菜单（含按钮），菜单上的 `permission` 字段即权限码（`system:user:create`）。
- 登录后 `GET /auth/codes` 返回权限码集合；前端 `v-access:code` 控制按钮显隐。
- 后端每个写接口用 `middleware.RequirePermission(model.PermXxx)` 校验，**不信任前端隐藏**。
- `super_admin` 拥有全部权限码，且可在未显式授权的情况下访问全部菜单。
- 保护规则：内置角色不可删除/改标识；不能删除或停用自己；不能删除最后一个启用的超级管理员；仍有用户绑定的角色不可删除。

## 6. 菜单即路由

`GET /auth/menus` 直接输出 Vben 后端模式所需的记录：

```json
{
  "path": "/system",
  "name": "System",
  "redirect": "/system/user",
  "meta": { "title": "系统管理", "icon": "lucide:settings", "order": 20 },
  "children": [
    {
      "path": "/system/user",
      "name": "SystemUser",
      "component": "/system/user/index",
      "meta": { "title": "用户管理", "icon": "lucide:users", "order": 1, "keepAlive": true }
    }
  ]
}
```

规则（与 `packages/utils/src/helpers/generate-routes-backend.ts` 对齐）：

- 目录（catalog）不输出 `component`，其子路由被挂到根布局下；`component` 为相对 `views` 的路径，不带 `.vue`。
- `embedded` → `component: "IFrameView"` + `meta.iframeSrc`；`link` → `meta.link`。
- `button` 不下发到路由，只出现在权限码里。

## 7. 错误处理与响应

`internal/apperr` 定义带 HTTP 状态与业务码的错误，`response.Fail` 统一渲染：

```json
{ "code": 40901, "message": "用户名已存在", "data": null }
```

- `code = 0` 成功；`4xxxx` 客户端错误；`5xxxx` 服务端错误。
- 5xx 才记录 `error` 级日志与 cause，避免正常业务失败刷屏。
- 参数校验失败统一 `40000`，validator 信息翻译成中文。

## 8. 审计日志

`middleware.OperationLogger` 拦截 `POST/PUT/PATCH/DELETE`：

- 读取并还原 body（`io.MultiReader`），对 `password/secret/token/authorization/credential` 等字段脱敏为 `***`，超过 `SECURITY_OPERATION_LOG_LIMIT` 截断；
- 通过带缓冲的 channel 异步写库（256 容量），不阻塞请求；
- 记录方法、路径、状态码、耗时、IP、UA、操作人。

## 9. 前端要点

- `apps/web/src/api/request.ts`：`requestClient` 走 Vben 拦截器链（解包 `{code,message,data}`、401 自动刷新、错误提示）；刷新逻辑改为读取本地 refreshToken 并回写两个新 token。
- `apps/web/src/preferences.ts`：`accessMode: 'backend'`、`defaultHomePath: '/dashboard/workspace'`、`enableRefreshToken: true`。
- 页面统一使用 `Page`（`@vben/common-ui`）+ Naive UI 组件；表格用 `NDataTable` 远程分页，表单用 `NForm` + `FormRules`。
