# base-admin-core 后端 Kubernetes 配置

本目录是后端 K8s 部署清单；前端构建与部署在 `frontend/`，两者独立。

配置方式为“环境变量覆盖镜像内 `config.yaml`”：

- [`configmap.yaml`](configmap.yaml)：按需覆盖 L1/L2 非敏感项，只列需要覆盖的键。
- [`secret.yaml`](secret.yaml)：L3 敏感项，包括 `JWT_SECRET`、数据库/Redis 口令和首次管理员密码。

镜像必须包含 `config.yaml`（构建上下文为 `backend/` 时即 `backend/config.yaml`），或通过 `CONFIG_FILE` 指向挂载的配置文件。ConfigMap/Secret 只做覆盖，不替代 YAML。

## 1. 变量替换

清单中的 `${VAR}` 由云效流水线在 `kubectl apply` 前替换，变量值来自云效变量组。Kubernetes 本身不解析 `${VAR}`；直接执行 `kubectl apply` 会把字面量存入 ConfigMap/Secret。

使用要求：

- 云效变量组先定义下文列出的变量，不要保留未定义的变量。
- 本地验证时先用 `envsubst` 渲染，或手工替换后再 apply。
- Secret 未替换时，`APP_ENV=production` 会使 `validateProduction` 拒绝启动；ConfigMap 未替换通常表现为数据库/Redis 连接失败或 CORS 不匹配。
- 命令示例中的 `$(...)` 是 shell 命令替换，不是云效变量，也不是 K8s 语法。
- `<...>` 不是本目录的占位语法；配置校验仍会拒绝尖括号和 `${...}` 作为兜底。

## 2. 配置 ConfigMap

在云效变量组中定义：

| 变量 | 示例 | 说明 |
|---|---|---|
| `DB_HOST` | `postgres` | 数据库 Service 名或地址 |
| `DB_PORT` | `5432` | 数据库端口 |
| `DB_USER` | `base_admin` | 数据库用户 |
| `DB_NAME` | `base_admin` | 数据库名 |
| `DB_SSLMODE` | `require` | 集群内无 TLS 时可改为 `disable` |
| `REDIS_ADDRS` | `redis:6379` | Redis 地址，多个用逗号分隔 |
| `CORS_ALLOWED_ORIGINS` | `https://admin.example.com` | 前端 Origin，多个用逗号分隔 |

`APP_ENV=production`、`LOG_FORMAT=json` 固定写在清单中，不参与变量替换。

需要时再取消注释并定义：

| 变量 | 用途 |
|---|---|
| `DB_TABLE_PREFIX` | 多部署共用数据库时区分表前缀 |
| `REDIS_KEY_PREFIX` | 多部署共用 Redis 时区分 key 前缀 |
| `HTTP_TRUSTED_PROXIES` | Ingress / Pod 网段 |
| `DB_MIGRATE` | 多副本时设 `false`，迁移交给 Job |
| `CORS_ALLOW_CREDENTIALS` | 使用 Cookie 时设 `true`，且 Origin 必须明确 |
| `REDIS_MODE` / `REDIS_MASTER_NAME` | 哨兵模式 |
| `BOOTSTRAP_ADMIN_EMAIL` | 首次管理员邮箱 |

数据库和 Redis 与服务在同一命名空间时，`DB_HOST=postgres`、`REDIS_ADDRS=redis:6379` 即对应 Service 名。

## 3. 配置 Secret

在云效变量组中定义：

```text
JWT_SECRET
DB_PASSWORD
REDIS_PASSWORD
BOOTSTRAP_ADMIN_PASSWORD
```

由流水线 apply [`secret.yaml`](secret.yaml)，流水线会把 `${JWT_SECRET}` 等替换为变量值。

也可以直接生成 Secret，避免明文留在仓库：

```bash
kubectl -n base-admin create secret generic base-admin-backend-secret \
  --from-literal=JWT_SECRET="$(openssl rand -base64 48)" \
  --from-literal=DB_PASSWORD='请替换为数据库口令' \
  --from-literal=REDIS_PASSWORD='请替换为 Redis 口令' \
  --from-literal=BOOTSTRAP_ADMIN_PASSWORD='请替换为首次管理员强密码' \
  --dry-run=client -o yaml > /tmp/secret.yaml
```

首次管理员创建完成后，可以移除 `BOOTSTRAP_ADMIN_PASSWORD`；留空时进程会随机生成密码，并在启动日志中打印一次。

## 4. 应用

以下命令假设 `base-admin` 命名空间已存在；未创建时先执行：

```bash
kubectl create namespace base-admin
```

云效流水线直接 apply 两个清单即可，变量替换由流水线完成。

本地验证时，在仓库根目录先导出变量，再用 `envsubst` 渲染：

```bash
export DB_HOST=postgres DB_PORT=5432 DB_USER=base_admin DB_NAME=base_admin
export DB_SSLMODE=require REDIS_ADDRS=redis:6379
export CORS_ALLOWED_ORIGINS=https://admin.example.com

envsubst < backend/deploy/k8s/configmap.yaml | kubectl -n base-admin apply -f -
```

`envsubst` 会把未定义的变量替换为空字符串，执行前确认变量都已导出。

## 5. 在 Deployment 中注入

```yaml
spec:
  containers:
    - name: backend
      image: base-admin-core/backend:latest
      envFrom:
        - configMapRef:
            name: base-admin-backend-config
        - secretRef:
            name: base-admin-backend-secret
```

`envFrom` 的 key 必须与环境变量名完全一致；清单已按 `APP_*`、`DB_*`、`REDIS_*` 等实际名称编写。列表字段（如 `CORS_ALLOWED_ORIGINS`、`HTTP_TRUSTED_PROXIES`）用逗号分隔。

## 6. 运维注意

- 只有 `APP_ENV=production` 才会启用 `validateProduction` 的额外检查；漏设会跳过占位密钥、通配 CORS + 凭证等规则。
- ConfigMap/Secret 不会热更新；修改后执行 `kubectl -n base-admin rollout restart deployment/<name>`。
- 多副本同时启动可能并发执行迁移和首次管理员创建。生产建议用 Job 执行迁移与 bootstrap，普通副本设置 `DB_MIGRATE: "false"`。
- 镜像内没有 `config.yaml` 时，需挂载配置文件并设置 `CONFIG_FILE`，否则启动报 `config file not found`。
