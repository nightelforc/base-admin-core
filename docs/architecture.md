# 架构说明

## 1. 总体结构

`backend/` 是可复用的后端服务基座，不与“管理后台”这一种形态绑定：认证与会话、RBAC、审计、配置装载、日志、PostgreSQL/Redis 接入、超时预算与优雅退出都是通用能力，下面这套分层对管理后台和对外服务同样适用。当前用管理后台的业务驱动这些基础能力落地，所以图中的装配是这一形态的默认样子；换成对外服务时，复用 `pkg/` 与分层约定，替换 `internal/` 中的业务实现即可。

```
浏览器 (Vue 3 + Naive UI, Vben 5)
        │  /api/v1  (JSON, Bearer Token)
        ▼
http.Server(RequestTimeout) ── Gin 路由 ── middleware(RequestID → AccessLog → Recovery → CORS → BodyLimit)
        │              ↘ secured: Auth → 权限校验 → OperationLog
        ▼
handler ──► service ──► repository ──► GORM ──► PostgreSQL
                │
                ├─► Redis（会话、登录限流）
                ├─► pkg/password（bcrypt）
                └─► pkg/jwt（签发 / 校验）
```

后端严格分层，依赖方向单向（handler → service → repository → pkg）：

| 层 | 职责 | 禁止 |
| --- | --- | --- |
| `handler` | 绑定/校验参数、调用 service、写响应 | 写业务规则、直接访问 gorm |
| `service` | 业务规则、事务边界、跨仓储编排 | 出现 `gin.Context`、拼 SQL |
| `repository` | 数据访问，唯一出现 SQL 的层 | 返回 HTTP 语义、依赖 service |
| `pkg` | 无业务语义的可复用基础设施，按**依赖本体**命名（`postgres`、`redis`、`jwt`、`password`、`session`、`logger`、`apperr`、`response`、`migrate`） | 依赖 `internal/`、出现业务规则 |

`internal/` 是应用私有代码（Go 编译期禁止外部模块 import），`pkg/` 是与业务无关、可被其他项目直接 import 的基础设施。
`pkg/*` 一律通过 `Options` 结构体接收配置，不 import `internal/config`；由 `cmd/server`、`cmd/migrate` 负责把配置翻译成 `Options`。
迁移文件的 `fs.FS` 也由调用方注入（`migrations.FS`），因此 `pkg/migrate` 不绑定本项目的 SQL 目录；`pkg/postgres` 的表前缀同样由 `Options.TablePrefix` 注入。

pkg 子包按「用哪个依赖就报哪个依赖的名字」命名：`pkg/redis` 而不是 `pkg/cache`，`pkg/postgres` 而不是 `pkg/database`，`pkg/jwt` / `pkg/password` 而不是笼统的 `pkg/security`，所以读 import 就能看出连的是谁。
登录限流 `internal/throttle` 刻意留在 `internal/`：它编码的是本项目的账号锁定策略，不是可复用基础设施。
观测同理：`internal/monitor/core` 只有四个概念——`Check`（监控项：自己定间隔、自己采样、自己判定）、`Registry`（节拍、panic 隔离、状态缓存与日志输出）、`Poll[T]`（把「取快照 → 差分 → 按策略链判定」这一类同形状的监控项一次写完，连接池就是它的第一个用户）、`Snapshot`/`CheckState`（每个监控项最近一次采样：严重度、指标、采样时间、是否陈旧，供读取方消费）；具体监控项放在子包（如 `monitor/pg` 的连接池），由 `monitor.Run` 统一注册——`cmd/server` 只递运行时依赖，开哪些检查是 monitor 自己的策略，不散落成装配代码或全局配置；因此它不进 `pkg`，也没有任何驱动反向依赖它。将来真要跨 module 复用，再整体迁移到 `pkg`。细节（调度语义、判定策略、日志契约、扩展方式、已知缺口）见 [backend/monitor.md](backend/monitor.md)。

## 2. 配置

配置按“`config.yaml` < 环境变量”两层装配，环境变量只用于覆盖：

| 层级 | 内容 | 典型来源 |
| --- | --- | --- |
| L1 基础运行 | 应用标识、日志、HTTP 监听与超时、首次启动引导（管理员密码除外） | `config.yaml` |
| L2 基础设施 | PostgreSQL、Redis、CORS 等按环境变化的地址与容量 | `config.yaml` + ConfigMap |
| L3 安全敏感 | JWT、密码策略、登录限流、审计，以及数据库/Redis 口令 | Secret / 环境变量 |

`backend/config.yaml` 是全量配置的唯一声明处，也是默认值的唯一来源；`CONFIG_FILE` 可指定路径，未设置时依次查找
`./config.yaml`、`./backend/config.yaml`。YAML 使用严格字段校验，拼错的键会直接启动失败。

启动期还会做跨字段校验，且 tag 校验与跨字段校验的错误会合并成一条（报错里用环境变量名，如 `LOG_LEVEL`，不是 Go 字段名）：

- 生产环境（`APP_ENV=production`）额外拒绝：示例/默认密钥、`CORS_ALLOWED_ORIGINS=*` 同时开启凭证、`APP_DEBUG=true`。
- `DB_TABLE_PREFIX` 只允许 `^[a-z][a-z0-9_]*$`（或空），避免拼接出非法表名。
- HTTP / Redis 的超时以及登录窗口、封禁时长必须为正数；`HTTP_IDLE_TIMEOUT`、`CORS_MAX_AGE`、`HTTP_REQUEST_TIMEOUT` 不得为负，`HTTP_WRITE_TIMEOUT` 允许为 `0`（服务长连接时必须为 0）。
- `HTTP_REQUEST_TIMEOUT` 是注入请求 context 的处理预算，到点后下游调用随上下文取消而中断、处理器返回 504。`0` 表示不注入；启用且设置了 `HTTP_WRITE_TIMEOUT` 时必须更短，否则连接会先被写超时掐断。
- `DB_STATEMENT_TIMEOUT` 是数据库侧的止损点，必须短于 `HTTP_REQUEST_TIMEOUT`。请求超时只会让调用方放弃，服务端的语句仍在跑；只有语句超时能真正终止它。它只作用于应用连接池，迁移走不带该参数的 `MigrateDSN()`，因为大表的 DDL 可能合法地跑得更久。
- `JWT_SECRET` 至少 32 字符，缺失或过短直接启动失败。
- 日志启动时输出 `config.Summary()`，敏感字段已脱敏。

配置项、默认值与分级注释见 [`backend/config.yaml`](../backend/config.yaml)；需要按环境覆盖的示例见 [`backend/.env.example`](../backend/.env.example)。
装配顺序、配置文件查找与扩展方式见 [`docs/backend/config.md`](backend/config.md)。

### 超时预算与长连接

超时只有一种做法：**注入截止时间，由下游各自兑现**。`internal/server/deadline.go` 把 `HTTP_REQUEST_TIMEOUT` 写进请求 context，然后什么都不做——不缓冲响应、不替处理器写超时响应、不碰 `Flush`/`Hijack`。因此 SSE、WebSocket、流式下载与普通 CRUD 走同一条路径，没有前缀特判。

缓冲区换来的「硬 504」代价太大：响应必须全量驻留内存，长连接在第一次 `Flush` 之前一条数据都发不出去，`Hijack` 之后包装层还要小心不写坏握手。业内（k8s apiserver 只对非长请求计时、Hystrix 的每依赖超时预算）都是让超时沿着调用链逐层收紧。

预算逐层变短，避免下游比上游活得久：

```
反向代理 45s  >  HTTP_REQUEST_TIMEOUT 30s  >  DB_STATEMENT_TIMEOUT 5s
```

各层怎么兑现自己的预算：

| 层 | 机制 | 到点后的表现 |
| --- | --- | --- |
| HTTP 请求 | context 截止时间 | 下游调用返回 `context.DeadlineExceeded`，处理器经 `response.Fail` 输出 504 / 50400 |
| PostgreSQL | `DB_STATEMENT_TIMEOUT`（连接级 `statement_timeout`） | 服务端主动终止语句，连接可用 |
| Redis | 连接级读写超时 | 单个命令返回超时错误 |
| 审计写入 | 独立 `context.Background()` + 5s | 不随请求取消，避免丢审计 |
| 出站 HTTP | 每次调用自带 `context.WithTimeout` | 调用方按需设置，沿 context 取消 |

长连接部署有三个前提：

- `HTTP_WRITE_TIMEOUT` 必须为 `0`（也是默认值）。它是 Go 的绝对写 deadline，从读完请求头开始计时，非 0 时 SSE 会在该时刻被拦腰截断；不提供长连接的服务可以设为 60s 来限制慢客户端。
- 反向代理要关闭缓冲并放宽空闲超时：`proxy_buffering off;`、`X-Accel-Buffering: no`、`proxy_read_timeout 3600s;`；WebSocket 还需透传 `Upgrade` / `Connection`。
- 响应以一条注释行（如 `: connected`）开场并立即 `Flush`，让代理与客户端尽早确认连接可用，也便于排查「事件没到」是不是卡在缓冲里。

## 3. 表前缀与迁移

- 表前缀通过 GORM `NamingStrategy{TablePrefix}` 生效，**模型不实现 `TableName()`**（实现后会绕过前缀）。
- 需要在原生 SQL 中引用表名时，统一走 `repository.Base.Tables()`，由 `stmt.Parse(&model.X{})` 解析出真实表名。
- 迁移文件是带 `{{prefix}}` 占位符的普通 SQL（`backend/migrations/*.sql`），由 `pkg/migrate` 执行：
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

`pkg/apperr` 定义带 HTTP 状态与业务码的错误，`response.Fail` 统一渲染：

```json
{ "code": 40901, "message": "用户名已存在", "data": null }
```

- `code = 0` 成功；`4xxxx` 客户端错误；`5xxxx` 服务端错误。
- 5xx 才记录 `error` 级日志与 cause，避免正常业务失败刷屏。
- 参数校验失败统一 `40000`，validator 信息按环境变量名给出，一次列全所有问题。

## 8. 审计日志

`middleware.OperationLogger` 拦截 `POST/PUT/PATCH/DELETE`：

- 读取并还原 body（`io.MultiReader`），对 `password/secret/token/authorization/credential` 等字段脱敏为 `***`，超过 `SECURITY_OPERATION_LOG_LIMIT` 截断；
- 通过带缓冲的 channel 异步写库（256 容量），不阻塞请求；
- 记录方法、路径、状态码、耗时、IP、UA、操作人。

## 9. 前端要点

- `apps/web/src/api/request.ts`：`requestClient` 走 Vben 拦截器链（解包 `{code,message,data}`、401 自动刷新、错误提示）；刷新逻辑改为读取本地 refreshToken 并回写两个新 token。
- `apps/web/src/preferences.ts`：`accessMode: 'backend'`、`defaultHomePath: '/dashboard/workspace'`、`enableRefreshToken: true`。
- 页面统一使用 `Page`（`@vben/common-ui`）+ Naive UI 组件；表格用 `NDataTable` 远程分页，表单用 `NForm` + `FormRules`。
