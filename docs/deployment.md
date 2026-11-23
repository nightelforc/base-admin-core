# 部署说明

前后端可以独立构建、独立部署：后端是 Go 二进制，前端是纯静态文件；两者不共享构建产物，
也不要求部署在同一台机器或同一个进程里。

## 1. 构建产物

```bash
# 后端（Linux amd64 交叉编译示例）
cd backend
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/server ./cmd/server

# 前端（产物为纯静态文件）
cd frontend
pnpm install --frozen-lockfile
pnpm build          # 产物：frontend/apps/web/dist
```

> 后端二进制不包含业务配置。全量配置在 `backend/config.yaml`，部署时需与二进制一起提供
> （或用 `CONFIG_FILE` 指向它）；环境变量只用于覆盖需要按环境变化的项和注入密钥。
> SQL 迁移与 `migrations/*.sql` 已用 `go:embed` 编进二进制，部署时无需额外拷贝。

## 2. 运行后端

```bash
# 方式一：config.yaml + 环境变量覆盖 + systemd
sudo install -m 0755 bin/server /usr/local/bin/base-admin
sudo install -d -o www-data -g www-data /etc/base-admin
sudo install -m 0640 -o root -g www-data config.yaml /etc/base-admin/config.yaml
sudo install -m 0640 -o root -g www-data backend.env /etc/base-admin/base-admin.env
sudo systemctl daemon-reload && sudo systemctl enable --now base-admin
```

`/etc/systemd/system/base-admin.service`：

```ini
[Unit]
Description=base-admin API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=www-data
Group=www-data
WorkingDirectory=/opt/base-admin
Environment=CONFIG_FILE=/etc/base-admin/config.yaml
EnvironmentFile=/etc/base-admin/base-admin.env
ExecStart=/usr/local/bin/base-admin
Restart=on-failure
RestartSec=3
# 优雅退出：程序收到 SIGTERM 后会等待在途请求，最多 HTTP_SHUTDOWN_TIMEOUT
TimeoutStopSec=30

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/base-admin

[Install]
WantedBy=multi-user.target
```

数据库迁移与首个管理员由独立命令完成，发布流水线在服务启动前执行：

```bash
set -a; . /etc/base-admin/base-admin.env; set +a
CONFIG_FILE=/etc/base-admin/config.yaml /usr/local/bin/base-admin-migrate
```

命令可重复执行（迁移有 checksum 与 advisory lock，bootstrap 幂等）；多副本部署时只运行一次。
应用进程不再执行迁移，`DB_MIGRATE` 已移除。

## 3. Nginx 反向代理

```nginx
server {
    listen 80;
    server_name admin.example.com;

    root /var/www/base-admin;      # frontend/apps/web/dist 的内容
    index index.html;

    # 前端使用 hash 路由时该段可省略；history 路由需要回退到 index.html
    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
        client_max_body_size 8m;
    }

    location = /healthz { proxy_pass http://127.0.0.1:8080/healthz; access_log off; }
}
```

生产环境要同时调整：

- `HTTP_TRUSTED_PROXIES` 设为反向代理的网段（如 `127.0.0.1/32,10.0.0.0/8`），否则审计日志里记录的是代理 IP。
- `CORS_ALLOWED_ORIGINS` 仅在前后端分离不同源时才需要配置，并建议 `CORS_ALLOW_CREDENTIALS=false`。
- 前端构建前修改 `frontend/apps/web/.env.production` 的 `VITE_GLOB_API_URL`（同源部署保持 `/api/v1`）。

## 4. 配置与覆盖

全量配置及默认值见 [`backend/config.yaml`](../backend/config.yaml)，按 L1 基础运行 / L2 基础设施 /
L3 安全敏感分级；每项都标注了对应层级。加载顺序为“config.yaml < 环境变量”，
config.yaml 同时是全量配置和默认值的唯一来源，环境变量只写需要覆盖的项，示例见 [`backend/.env.example`](../backend/.env.example)。

Kubernetes 清单见 [`backend/deploy/k8s/configmap.yaml`](../backend/deploy/k8s/configmap.yaml) 和
[`backend/deploy/k8s/secret.yaml`](../backend/deploy/k8s/secret.yaml)：ConfigMap 只列需要覆盖的项，未列出的键
继续使用 `config.yaml`；ConfigMap / Secret 通过 `envFrom` 以环境变量注入。清单中的 `${...}` 由云效流水线在
`kubectl apply` 前替换，不是 Kubernetes 语法；变量需先在云效变量组中定义。使用说明见
[`backend/deploy/k8s/README.md`](../backend/deploy/k8s/README.md)。如果改用挂载 ConfigMap 的方式，
则设置 `CONFIG_FILE` 指向挂载的配置文件。

生产环境必须覆盖或确认：

| 变量 | 生产要求 |
| --- | --- |
| `APP_ENV` | `production`（会开启额外校验：拒绝默认密钥、通配 CORS+凭证、`APP_DEBUG=true`） |
| `APP_DEBUG` | `false` |
| `JWT_SECRET` | 必填，≥32 字符，随机生成（`openssl rand -base64 48`） |
| `DB_PASSWORD` / `REDIS_PASSWORD` | 由 Secret 注入，不要写入 `config.yaml` |
| `DB_*` | 指向实际实例；`DB_SSLMODE` 公网/云数据库建议 `require` |
| `DB_TABLE_PREFIX` | 与其他系统共用库时必须唯一，例如 `base_` |
| `REDIS_*` | 指向实际实例；`REDIS_KEY_PREFIX` 同理需唯一 |
| `HTTP_REQUEST_TIMEOUT` | 单请求处理时限，默认 `30s`；返回 504 并取消下游调用，`0` 表示关闭 |
| `HTTP_TRUSTED_PROXIES` | 反向代理网段 |
| `CORS_ALLOWED_ORIGINS` | 明确的前端域名 |
| `LOG_FORMAT` / `LOG_LEVEL` | `json` / `info`（便于采集） |
| `SECURITY_LOGIN_*` | 按安全策略调整 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 首次部署设置强密码；创建完成后可从环境变量中移除 |

## 5. 与其它系统共用数据库

本模板支持多个部署共用一个 schema：

- 所有表名都带 `DB_TABLE_PREFIX`，建表、查询、原生 SQL 全部走同一前缀；
- 迁移记录表为 `<prefix>schema_migrations`，不会与别的系统的迁移表冲突；
- 迁移过程持有 `pg_advisory_lock`，锁的 key 由 `<prefix>schema_migrations` 计算，互不干扰；
- Redis 的 key 全部以 `REDIS_KEY_PREFIX` 开头（会话、登录限流）。

注意事项：

1. 前缀只允许 `^[a-z][a-z0-9_]*$`，避免 SQL 注入与大小写问题。
2. 应用启动/迁移**不会**影响其它前缀的表；不要在共享库上执行 `DROP SCHEMA` 之类的操作。
3. 修改已有前缀需要手工重命名表，并同步修改 `DB_TABLE_PREFIX`。

## 6. 升级流程

1. 新增迁移文件 `backend/migrations/000N_xxx.sql`，使用 `{{prefix}}` 占位符；**不要修改已应用的旧文件**（checksum 校验会拒绝）。
2. 发布前先备份数据库（见下）。
3. 部署新二进制 → 运行 migrate Job（迁移 + 引导管理员）→ 重启服务。
4. 回滚时使用旧二进制；若新迁移做了不兼容变更，按迁移文件中的注释手工回滚（建议采用"先加列/后删列"的兼容式演进）。

> 升级注意：本版本移除了 `database.migrate` / `DB_MIGRATE`。配置使用严格解析，升级前请删除 `config.yaml` 中的 `database.migrate` 键，并移除部署清单里的 `DB_MIGRATE` 环境变量；否则会因未知字段拒绝启动。

备份示例：

```bash
pg_dump --format=custom --file=base_admin_$(date +%F).dump "$DATABASE_URL"
```

## 7. 可观测性

- 访问日志：每条请求一行，含 `request_id / method / path / status / latency / ip`，`X-Request-ID` 会回写到响应头，便于串联。
- 应用日志：`slog`，生产用 `LOG_FORMAT=json`；5xx 才带 `error` 字段与堆栈。请求级日志统一带 `request_id`，GORM 的慢查询与失败语句同样继承它并附 `component=gorm`，可直接按请求串联 SQL。
- 审计日志：写操作入库（`<prefix>operation_logs`），在"系统管理 → 操作日志"中查询。
- 健康检查：`/healthz`（进程存活）、`/readyz`（依赖可用），可直接接入 K8s 探针或负载均衡健康检查。

## 8. 安全检查清单

- [ ] `JWT_SECRET`、数据库/Redis 口令已通过密钥管理下发，未写入镜像或 `config.yaml`。
- [ ] `backend/.env` 位于 `.gitignore` 中，仓库里只有 `.env.example`；`config.yaml` 不含任何真实口令。
- [ ] `APP_DEBUG=false`、`LOG_LEVEL=info`、`LOG_FORMAT=json`。
- [ ] 数据库、Redis 使用最小权限账号，且只对应用服务器开放。
- [ ] 反向代理启用 HTTPS，并设置 HSTS。
- [ ] 首次登录后修改默认管理员密码（或直接通过 `BOOTSTRAP_ADMIN_PASSWORD` 指定强密码）。
- [ ] 定期检查登录日志中的失败记录与限流命中情况。
