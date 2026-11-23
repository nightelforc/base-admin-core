# 配置说明

> 适用范围：`backend/` 后端服务。
>
> 配置代码：`backend/internal/config/`；全量模板：`backend/config.yaml`。
>
> 本文说明配置的装配顺序、文件格式、校验规则和扩展方式。配置项的完整清单以 `backend/config.yaml` 为准。

## 1. 设计原则

后端配置遵循五条约束：

1. **单一声明处**：`backend/config.yaml` 声明全部配置项，并且是默认值的唯一来源；代码里不保留 `envDefault`。
2. **两层覆盖**：`config.yaml` < 环境变量。环境变量只用于覆盖，不要求写全量；但 `config.yaml` 本身必须写全。
3. **启动期失败**：配置文件缺失、YAML 拼错、字段校验失败、生产环境使用占位密钥，都会让进程直接退出，不带病启动。
4. **显式注入**：`config.Load()` 只在 `cmd/server`、`cmd/migrate` 的启动路径中调用（测试除外）；数据库、Redis、JWT、路由等组件接收 `cfg` 或 `Options`，不自己读环境变量。
5. **敏感项不入库**：JWT 密钥、数据库/Redis 口令、初始管理员密码在 `config.yaml` 中只留空占位，由环境变量或 Secret 注入。

配置在进程启动时读取一次，**不支持热加载**。修改 `config.yaml` 或环境变量后需要重启进程。

## 2. 装配流程

### 2.1 启动期调用链

```text
cmd/server/main.go
  ├─ godotenv.Load(".env", "backend/.env")   可选，只负责把文件灌进进程环境
  ├─ config.Load()                           装配并校验配置
  ├─ logger.New(...)                         日志等级、格式、调用位置
  ├─ postgres.Open(ctx, cfg.Database.Options())
  ├─ redis.Open(ctx, cfg.Redis.Options())
  ├─ build(cfg, ...)                         service / handler 装配
  ├─ server.NewRouter(...)                   CORS、BodyLimit、鉴权、审计
  └─ server.New(...).Run(ctx)                监听、超时、优雅退出
```

迁移与首个管理员不在 server 的启动链里：它们由 `cmd/migrate`（集群中的 Job）在服务启动前完成，
应用进程启动时不再执行任何 DDL，也不额外占用数据库连接。

`config.Load()` 的返回值是 `*config.Config`；加载成功后，启动日志会输出一份脱敏摘要：

```go
log.Info("starting",
    "app", cfg.App.Name,
    "env", cfg.App.Env,
    "config", cfg.Summary(),
)
```

`Summary()` 不包含任何密码或密钥，只输出配置文件路径、应用标识、监听地址、数据库地址/用户、Redis 地址、JWT issuer/TTL 等排障所需字段。

### 2.2 `Load()` 的六个步骤

入口在 [`backend/internal/config/config.go`](../../backend/internal/config/config.go)：

| 步骤 | 行为 | 失败表现 |
| --- | --- | --- |
| 1 | `resolveConfigFile()` 确定配置文件路径 | 找不到文件、路径是目录、`CONFIG_FILE` 指向不存在的文件 |
| 2 | 用 `yaml.Decoder` 把文件解码到空白 `cfg` | 读取失败、空文件、YAML 语法错误、未知字段 |
| 3 | 用 `env.Parse` 应用显式设置的环境变量 | 环境变量类型转换失败，返回 `apply environment overrides` |
| 4 | `applySecretFiles()` 把 `X_FILE` 指向的文件内容读入对应字段 | `X` 与 `X_FILE` 同时非空、`X_FILE` 为空串、文件不存在/不可读/内容为空 |
| 5 | 记录实际加载的路径到 `cfg.source` | 不会失败 |
| 6 | 调用 `cfg.Validate()` | 结构体 tag 或跨字段校验不通过 |

可以简化为：

```go
cfg := &Config{}

// 1. 找文件 + 2. 读 YAML
path, err := resolveConfigFile()
loadConfigFile(path, cfg)

// 3. 应用显式环境变量
env.Parse(cfg)

// 4. 读取 *_FILE 指向的密钥文件
applySecretFiles(cfg)

// 5. 记录来源 + 6. 校验
cfg.source = path
cfg.Validate()
```

### 2.3 为什么先读 YAML、再读环境变量

如果先解析环境变量、再解码 YAML，YAML 会把环境变量覆盖掉，与“环境变量优先级最高”的约定相反。所以顺序固定为：

```text
config.yaml  ->  环境变量
```

`env.Parse` 只处理**显式设置**的环境变量，未设置的键保留 YAML 中的值：

- 设置了 `HTTP_REQUEST_TIMEOUT=5s`：覆盖 YAML 的 `30s`。
- 没设置 `HTTP_REQUEST_TIMEOUT`：保留 YAML 的值。
- 显式设置 `HTTP_REQUEST_TIMEOUT=0s`：`0` 生效，表示关闭请求超时。

代码没有 `envDefault`，YAML 里没写的字段会保持 Go 零值。因此 `config.yaml` 必须写全所有配置项；删键、漏键通常会在启动校验阶段失败，而不是悄悄回退到某个默认值。

## 3. 配置分层与优先级

### 3.1 三层分级

`config.yaml` 按重要性和来源分为三层：

| 层级 | 内容 | 典型来源 | 说明 |
| --- | --- | --- | --- |
| L1 基础运行 | `app`、`log`、`http`、`bootstrap` | `config.yaml` | 应用标识、日志、监听地址、超时、首次启动引导（管理员密码除外）；多数环境相同 |
| L2 基础设施 | `database`、`redis`、`cors` | `config.yaml` + ConfigMap | 地址、库名、连接池、跨域策略；按环境变化 |
| L3 安全敏感 | `jwt`、`security`，以及数据库/Redis/初始管理员口令 | Secret / 环境变量 / `*_FILE` 文件 | 密钥、密码策略、登录限流、审计；不写入版本库 |

分层的目的不是限制覆盖方式，而是明确“哪些值可以进 Git、哪些值必须由 Secret 提供”。任何一层都可以被环境变量覆盖。

### 3.2 覆盖优先级

优先级从低到高：

```text
config.yaml  <  进程环境变量
```

`config.yaml` 同时承担“全量声明”和“默认值”两个职责。没有代码内置默认值，所以模板里的键不能删；环境变量只在需要按环境改值时覆盖。

以 `http.request_timeout` 为例：

| 场景 | 最终值 | 原因 |
| --- | --- | --- |
| YAML 不写，环境变量不设置 | `0` | 没有代码默认值；该字段可能被校验拒绝，不要依赖缺键 |
| YAML 写 `30s`，环境变量不设置 | `30s` | 使用 `config.yaml` 的值 |
| YAML 写 `12s`，环境变量 `HTTP_REQUEST_TIMEOUT=5s` | `5s` | 环境变量覆盖 YAML |
| YAML 写 `12s`，环境变量 `HTTP_REQUEST_TIMEOUT=0s` | `0s` | 显式 `0` 也是有效覆盖，表示关闭超时 |

注意：**“未设置”和“显式设置为零值”是两件事**。`env.Parse` 只处理显式设置的环境变量，所以 `0`、空字符串等零值只有在明确写入环境变量时才生效。

### 3.3 `.env` 的定位

`main.go` 启动时会尝试加载：

```go
godotenv.Load(".env", "backend/.env")
```

它有三个特点：

- **不是配置层**：`.env` 只是把键值写进进程环境，真正的覆盖仍由 `config.Load()` 中的 `env.Parse` 完成。
- **不覆盖已有环境变量**：真实环境变量优先级高于 `.env`，适合本地开发时用 `.env` 提供本地值，CI/生产用真实环境变量覆盖。
- **调用方忽略了加载错误**：`.env` 不存在不会阻止启动；但 `godotenv` 在遇到第一个不存在的文件时会提前返回，所以不要依赖它一定回退到 `backend/.env`。最稳妥的做法是在启动目录放 `.env`，或直接注入真实环境变量。

生产环境完全可以不使用 `.env`。

### 3.4 配置文件查找规则

`config.Load()` 不接收路径参数，路径由 `CONFIG_FILE` 或约定位置决定：

1. `CONFIG_FILE` 非空：只认这个路径。
   - 文件必须存在。
   - 不能是目录。
   - 找不到直接报错，不回退到默认位置。
2. `CONFIG_FILE` 为空：依次查找
   - `./config.yaml`
   - `./backend/config.yaml`

因此从仓库根目录或 `backend/` 目录启动都能找到配置；从其他目录启动时，应显式设置：

```bash
CONFIG_FILE=/etc/base-admin/config.yaml ./base-admin
```

`CONFIG_FILE` 的相对路径是相对于**进程当前工作目录**解析的，不是相对于二进制所在目录。systemd 部署时建议使用绝对路径，并配合 `WorkingDirectory`。

两个位置都找不到时，启动失败：

```text
config file not found: set CONFIG_FILE or create one of: config.yaml, backend/config.yaml
```

### 3.5 严格解析

YAML 解码开启了 `KnownFields(true)`：

```go
decoder := yaml.NewDecoder(bytes.NewReader(data))
decoder.KnownFields(true)
decoder.Decode(cfg)
```

这意味着：

- `nmae:` 这类拼写错误会直接报错，不会被静默忽略。
- 多写一个不存在的配置项也会报错。
- 空文件会报 `parse config file "...": file is empty`，不会悄悄使用零值。

`config.yaml` 中的 `time.Duration` 字段可以直接写带单位的字符串：

```yaml
http:
  request_timeout: 30s
  shutdown_timeout: 20s
```

`yaml.v3` 会把它们解析成 `time.Duration`，不需要在代码里额外转换。

### 3.6 密钥文件（`*_FILE`）

敏感项除了直接写环境变量，还可以让环境变量只给出文件路径：

```bash
JWT_SECRET_FILE=/run/secrets/jwt_secret
DB_PASSWORD_FILE=/run/secrets/db_password
REDIS_PASSWORD_FILE=/run/secrets/redis_password
REDIS_SENTINEL_PASSWORD_FILE=/run/secrets/redis_sentinel_password
BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/admin_password
```

`Load()` 在 `env.Parse` 之后读取这些文件，把内容写回对应字段。因此文件与直接设置 `X` 完全等价：长度、占位符、生产环境策略等校验对两种来源一视同仁。

实现见 [`secrets.go`](../../backend/internal/config/secrets.go)：变量名逐条列在 `secretTargets` 表里（`env` 与 `fileEnv` 成对出现，便于按变量名搜索），`applySecretFiles()` 负责读取文件、冲突校验并写回字段。

这是 Docker Secret、Kubernetes Secret 卷挂载、Vault Agent 注入的通用形态——容器环境变量里只有路径，明文密码不会出现在 `Deployment` 清单、`docker inspect` 或 `/proc/1/environ` 里。

两个通道、多种来源，容易混淆，先固定一下口径：

| 通道 | 环境变量形态 | 常见来源 |
| --- | --- | --- |
| `X`（值内联） | `DB_PASSWORD=明文` | shell `export`、`.env`、systemd `Environment=` / `EnvironmentFile=`、`docker -e` / `--env-file`、K8s `env` / `envFrom.secretRef`、CI 变量组 |
| `X_FILE`（路径内联） | `DB_PASSWORD_FILE=/路径` | K8s Secret 卷挂载、docker compose `secrets:`、systemd `LoadCredential=`、手写 `chmod 600` 文件、Vault Agent 渲染 |

`.env` 与 Kubernetes Secret 都不是“第三种通道”，而是给上面两个通道供值的来源：`.env` 由 `godotenv` 读入进程环境，效果等同 `export`；Secret 用 `envFrom` 注入走 `X`，用卷挂载走 `X_FILE`。同一项在同一个进程里只能选一条通道——`X` 与 `X_FILE` 同时非空会被启动校验拒绝。

来源之间还有一条容易忽略的规则：`godotenv.Load` **不覆盖**已存在的环境变量。因此 shell / systemd 里的 `X` 优先于 `.env` 里的同名项；但也正因为 `.env` 里的值同样进入进程环境，外部再设 `X_FILE` 一样会触发冲突——从 `.env` 迁到文件挂载时，先把 `.env` 里对应那行注释掉。

`CONFIG_FILE` 不在这两个通道里：它指定的是 `config.yaml` 本身的位置，和口令注入是两条独立的轴。

程序侧的判定规则只有下面几条：

| 规则 | 行为 |
| --- | --- |
| 支持范围 | 仅 `JWT_SECRET`、`DB_PASSWORD`、`REDIS_PASSWORD`、`REDIS_SENTINEL_PASSWORD`、`BOOTSTRAP_ADMIN_PASSWORD` 五项；其余配置项仍用普通环境变量覆盖 |
| `X` 与 `X_FILE` 同时非空 | 报错，不定义优先级；`X` 为空串时视为未提供，便于流水线变量缺失的场景 |
| `X_FILE` 设置为空串 | 报错，这类值通常来自流水线里没填的变量 |
| 文件不存在、不可读、内容为空或只有空白 | 报错，不会退化成空密码 |
| 文件末尾换行 | 自动去掉（`\n`、`\r\n` 均可），其余字节原样保留 |

## 4. 配置树与命名

### 4.1 组件划分

根类型 `Config` 按组件拆分，每个组件一个文件：

| YAML 节点 | Go 类型 | 文件 | 环境变量前缀 |
| --- | --- | --- | --- |
| `app` | `App` | `app.go` | `APP_` |
| `log` | `Log` | `log.go` | `LOG_` |
| `http` | `HTTP` | `http.go` | `HTTP_` |
| `bootstrap` | `Bootstrap` | `bootstrap.go` | `BOOTSTRAP_` |
| `database` | `Database` | `database.go` | `DB_` |
| `redis` | `Redis` | `redis.go` | `REDIS_` |
| `cors` | `CORS` | `cors.go` | `CORS_` |
| `jwt` | `JWT` | `jwt.go` | `JWT_` |
| `security` | `Security` | `security.go` | `SECURITY_` |

`config.go` 只保留根配置树、加载顺序和启动期校验的编排，不承载具体组件字段或组件级规则。

### 4.2 字段标签

每个字段声明 `env` 和 `yaml`，必要时加 `validate`；默认值只写在 `config.yaml`，结构体上不再使用 `envDefault`。以 HTTP 为例：

```go
type HTTP struct {
    Addr           string        `env:"ADDR" yaml:"addr"`
    ReadTimeout    time.Duration `env:"READ_TIMEOUT" yaml:"read_timeout"`
    RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" yaml:"request_timeout"`
    MaxBodyBytes   int64         `env:"MAX_BODY_BYTES" yaml:"max_body_bytes"`
}
```

### 4.3 命名规则

**YAML 键**使用小写 snake_case，路径就是嵌套结构：

```yaml
http:
  request_timeout: 30s

database:
  max_open_conns: 15
```

**环境变量名**由“组件前缀 + 字段上的 `env` 标签”组成：

```text
HTTP_REQUEST_TIMEOUT
DB_MAX_OPEN_CONNS
REDIS_KEY_PREFIX
SECURITY_OPERATION_LOG_BODY_LIMIT
```

几个容易写错的映射：

| YAML 路径 | 环境变量 |
| --- | --- |
| `app.name` | `APP_NAME` |
| `http.request_timeout` | `HTTP_REQUEST_TIMEOUT` |
| `database.password` | `DB_PASSWORD`（不是 `DATABASE_PASSWORD`），或 `DB_PASSWORD_FILE=<路径>` |
| `redis.addrs` | `REDIS_ADDRS`（逗号分隔；单机恰好一个） |
| `jwt.secret` | `JWT_SECRET`，或 `JWT_SECRET_FILE=<路径>` |
| `security.operation_log_body_limit` | `SECURITY_OPERATION_LOG_BODY_LIMIT` |

切片字段（如 `http.trusted_proxies`、`cors.allowed_origins`）在环境变量中使用逗号分隔：

```bash
HTTP_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16
CORS_ALLOWED_ORIGINS=https://admin.example.com,https://ops.example.com
```

### 4.4 新增配置项的步骤

以新增一个 HTTP 配置项（例如 `HTTP.RequestTimeout`）为例：

1. 在对应组件文件中加字段和标签：

   ```go
   RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" yaml:"request_timeout"`
   ```

2. 在 `backend/config.yaml` 的对应节点加同名 YAML 键、默认值和注释。这是默认值的唯一来源，不能只加字段不加键。
3. 如果需要按环境覆盖，在 `backend/.env.example` 增加示例。
4. 单字段范围用 `validate` tag；跨字段约束写到对应结构体文件里的 `validate()`，根 `Validate()` 只负责按组件顺序调用。
5. 在 `backend/internal/config/config_test.go` 补测试，至少覆盖“YAML 覆盖 / 环境变量覆盖 / 缺键后的零值校验”。
6. 如果该配置会被组件消费，在装配层显式传入，不要让组件自己读环境变量。

`TestRepositoryConfigDeclaresEveryField` 会检查 `config.yaml` 是否覆盖了所有带 `yaml` 标签的字段；新增字段却忘了补模板时，这个测试会失败。

## 5. 启动期校验

`Validate()` 分三段执行，顺序不能颠倒：先做结构体 tag 校验，再让各组件校验自己的跨字段约束，最后执行生产环境策略。前一段失败会直接返回，后两段才聚合全部问题。

### 5.1 第一阶段：结构体 tag 校验

先执行：

```go
validator.New().Struct(c)
```

常见约束：

| 字段 | 约束示例 | 作用 |
| --- | --- | --- |
| `JWT.Secret` | `required,min=32` | 缺失或短于 32 字符直接失败 |
| `Security.OperationLogBodyLimit` | `min=0,max=4096` | 允许关闭，最大与审计表列宽对齐 |
| `App.Env` | 只允许 `development`、`test`、`production` | 防止拼错环境名导致生产校验失效 |

tag 校验失败时直接返回，不会继续收集跨字段问题。错误形如：

```text
invalid configuration: Key: 'Config.JWT.Secret' Error:Field validation for 'Secret' failed on the 'required' tag
```

### 5.2 第二阶段：各组件跨字段校验

tag 校验通过后，根 `Validate()` 按 `App`、`HTTP`、`Database`、`Redis`、`JWT`、`Security`、`CORS` 的顺序调用各自的 `validate()`。每个方法只返回 `problems`，不打印、不退出；根方法负责汇总，因此 **一次性列出全部错误**：

```go
// config.go：只保留编排
func (c *Config) Validate() error {
    if err := validator.New().Struct(c); err != nil {
        return fmt.Errorf("invalid configuration: %w", err)
    }
    var p problems
    p.extend(c.App.validate())
    p.extend(c.HTTP.validate())
    // ...
    p.extend(c.validateProduction())
    return p.err()
}
```

新增组件级约束时，改对应结构体文件里的 `validate()` 即可，不需要动 `config.go`。当前聚合的规则包括：

| 约束 | 失败示例 |
| --- | --- |
| 表前缀只允许 `^[a-z][a-z0-9_]*$` 或空 | `DB_TABLE_PREFIX "Base-" ...` |
| Redis key 前缀不能为空 | `REDIS_KEY_PREFIX must not be empty` |
| 应用时区、数据库时区必须能被 `time.LoadLocation` 加载 | `APP_TIMEZONE "Mars/Base" is not a known IANA time zone` |
| JWT access/refresh 必须为正，且 refresh > access | `JWT_REFRESH_TTL must be greater than JWT_ACCESS_TTL` |
| 数据库空闲连接数不能超过最大连接数 | `DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS` |
| HTTP 请求体上限必须为正 | `HTTP_MAX_BODY_BYTES must be positive` |
| HTTP/Redis 关键超时、登录窗口、封禁时长必须为正 | `HTTP_READ_TIMEOUT must be positive` |
| `HTTP_IDLE_TIMEOUT` 不能为负（0 合法，回退到 `ReadTimeout`） | `HTTP_IDLE_TIMEOUT must not be negative` |
| `HTTP_REQUEST_TIMEOUT` 不能为负；启用时必须小于 `HTTP_WRITE_TIMEOUT` | `HTTP_REQUEST_TIMEOUT must be less than HTTP_WRITE_TIMEOUT` |
| `CORS_MAX_AGE` 不能为负 | `CORS_MAX_AGE must not be negative` |

失败时错误体是：

```text
invalid configuration:
  - REDIS_KEY_PREFIX must not be empty
  - HTTP_REQUEST_TIMEOUT must be less than HTTP_WRITE_TIMEOUT
```

这种聚合方式让运维一次看到所有配置问题，而不是修一个、重启一次、再发现下一个。

### 5.3 第三阶段：生产环境额外校验

当 `APP_ENV=production` 时，额外拒绝：

- `JWT_SECRET` 仍是 `change-me`、`changeme`、`example`、`placeholder` 等占位值，或形如 `<JWT_SECRET>`、`${JWT_SECRET}` 的模板占位符。
- `CORS_ALLOW_CREDENTIALS=true` 且 `CORS_ALLOWED_ORIGINS` 包含 `*`。
- `APP_DEBUG=true`。
- `DB_PASSWORD`、`REDIS_PASSWORD`、`BOOTSTRAP_ADMIN_PASSWORD` 非空但仍是占位值或 `<...>`、`${...}` 模板占位符。

空口令不会被这条规则拦截：例如本地 PostgreSQL 使用 trust 认证时，`DB_PASSWORD` 可以为空；生产是否允许空口令由部署环境决定。

### 5.4 `Location()` 与 `Summary()`

- `Location()` 返回 `App.Timezone` 对应的时区；解析失败时回落到 `UTC`。正常情况下 `Validate()` 已经保证时区合法，这个回落主要用于防御性调用。
- `Summary()` 返回适合打日志的 `map[string]any`，包含 `config_file`、应用信息、监听地址、HTTP 请求超时、数据库地址/用户、Redis 地址、JWT issuer/TTL 等。**不要绕过 `Summary()` 直接把整个 `Config` 打进日志**，否则可能泄漏凭据。

## 6. 配置如何被消费

装配层负责把配置翻译成各组件的参数：

| 消费方 | 使用的配置 | 说明 |
| --- | --- | --- |
| `cmd/server` | `config.Load()`、`cfg.Summary()` | 加载配置并输出脱敏摘要 |
| `pkg/postgres` | `cfg.Database.Options()` | 连接地址、账号、SSL、连接池、慢查询阈值 |
| `pkg/redis` | `cfg.Redis.Options()` | 地址、账号、DB、超时、连接池、TLS 证书材料 |
| `cmd/migrate` | `config.Load()`、`cfg.Database`、`cfg.Bootstrap` | Job 中执行迁移并创建首个管理员，使用与应用相同的配置 |
| `build()` | `cfg.Security`、`cfg.JWT` | 密码策略、令牌签发 |
| `server.NewRouter` | `cfg.CORS`、`cfg.HTTP.MaxBodyBytes`、`cfg.HTTP.TrustedProxies` | 跨域、请求体上限、可信代理 |
| `server.New` | `cfg.HTTP` | 监听地址、传输层超时、请求超时、优雅退出 |
| 审计中间件 | `cfg.Security.OperationLogEnabled`、`cfg.Security.OperationLogBodyLimit` | 是否记录、请求体快照上限 |

组件不持有全局配置单例；`pkg/*` 只接收 `Options`，不 import `internal/config`。

### 6.1 HTTP 超时的分工

几个“超时”作用在不同层，不能互相替代：

| 配置 | 作用层 | 作用 |
| --- | --- | --- |
| `HTTP_READ_HEADER_TIMEOUT` | `net/http` | 读取请求头的最长时间 |
| `HTTP_READ_TIMEOUT` | `net/http` | 读取完整请求的最长时间 |
| `HTTP_WRITE_TIMEOUT` | `net/http` | 写响应的最长时间 |
| `HTTP_REQUEST_TIMEOUT` | 业务 handler 外层 | 单请求处理时限，超时返回 504 并取消上下文 |
| `HTTP_IDLE_TIMEOUT` | `net/http` | keep-alive 连接空闲时间 |
| `HTTP_SHUTDOWN_TIMEOUT` | 优雅退出 | 等待在途请求结束的最长时间 |

### 6.2 `HTTP.RequestTimeout` 的实现

它不是在 Gin 路由里加一个普通中间件，而是在 `http.Server.Handler` 外层包装：

```text
http.Server
  └─ RequestTimeoutHandler
       └─ Gin Engine
            └─ RequestID → AccessLog → Recovery → CORS → BodyLimit → ...
```

原因是 `gin.Context` 不能被另一个 goroutine 安全复用；如果直接在 Gin 中间件里起 goroutine 等待 handler，会有数据竞争风险。包装在 `http.Handler` 层可以：

- 在独立 goroutine 中执行下游 handler。
- 把响应先写入缓冲区，正常完成后再复制回真实 `ResponseWriter`。
- 超时后返回 HTTP 504，响应体为：

  ```json
  { "code": 50400, "message": "请求处理超时" }
  ```

- 取消请求的 `context.Context`，让数据库、Redis 等下游调用随之中断，及时释放资源。
- 把下游 panic 重新抛给外层，交给 Gin Recovery / `net/http` 处理。

`HTTP_REQUEST_TIMEOUT=0` 表示关闭该包装，直接使用原 handler。启用时必须小于 `HTTP_WRITE_TIMEOUT`，否则连接会先被写超时掐断，504 响应发不出去。

### 6.3 `OperationLogBodyLimit` 不是请求体上限

这两个配置经常被混淆：

| 配置 | 限制对象 | 默认值 | 作用 |
| --- | --- | --- | --- |
| `HTTP_MAX_BODY_BYTES` | 客户端请求体 | 4 MiB | 读取超过上限时拒绝请求，保护服务端内存 |
| `SECURITY_OPERATION_LOG_BODY_LIMIT` | 审计日志中的请求体快照 | 2048 字节 | 只影响记录多少内容，不影响接口能否处理请求 |

也就是说，普通 JSON 请求体远小于 2 KB，审计快照足够定位问题；超大 base64 请求体不会因为审计上限而被拒绝，真正的保护由 `HTTP_MAX_BODY_BYTES` 负责。

`OperationLogBodyLimit=0` 表示审计日志不记录请求体，只记录方法、路径、状态码、耗时等元数据。上限设为 4096，是为了与审计表 `request_body` 列宽对齐；超过上限的内容不会完整落库。

### 6.4 连接池容量与多副本

`max_open_conns` 限制的是**单个进程**，PostgreSQL 的 `max_connections` 限制的是**整个实例**；副本数会把前者放大成后者。规划时按最坏情况算：

```text
(峰值副本数 + 滚动/蓝绿余量) × max_open_conns + 1(迁移 Job) + 运维连接
    ≤ max_connections − superuser_reserved_connections
```

PostgreSQL 默认 `max_connections=100`、`superuser_reserved_connections=3`，普通用户可用约 97 条。以默认 `max_open_conns=15` 为例：

| 副本数 | 稳态（idle=5） | 峰值（open=15） | 滚动更新（+1 副本） |
| --- | --- | --- | --- |
| 1 | 5 | 15 | 30 |
| 3 | 15 | 45 | 60 |
| 6 | 30 | 90 | 105（超出） |

- 副本越多，单副本的 `max_open_conns` 应越小；也可以在数据库前加 PgBouncer。注意 transaction 模式的 PgBouncer 与 pgx 的 `cache_statement`、会话级 `statement_timeout` 不兼容，需要相应调整。
- 迁移与 bootstrap 由 Job 执行：Job 期间 1 条连接，运行期为 0；连接账本里仍要给它留 1 条。
- `pkg/postgres.Open` 启动时输出 `postgres pool` 日志，包含 `max_open`、`max_idle` 与服务器的 `max_connections`、`superuser_reserved_connections`；单池就超过服务器可用连接时输出 warning。
- 生产环境建议在 ConfigMap/Secret 中显式设置 `DB_MAX_OPEN_CONNS`（以及需要时的 `DB_MAX_IDLE_CONNS`），不要依赖模板默认值。

## 7. 常见问题

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 启动报 `config file not found` | 当前工作目录下没有 `config.yaml`，也没有设置 `CONFIG_FILE` | 设置 `CONFIG_FILE` 绝对路径，或从仓库根/`backend/` 目录启动 |
| 启动报 `field xxx not found` | YAML 键拼错，或加了代码中不存在的字段 | 对照 `backend/config.yaml` 修正键名 |
| 删除 `config.yaml` 某个键后启动报零值/校验失败 | 代码没有内置默认值，缺键就是 Go 零值 | 保留全量键；新增字段也要同步补模板 |
| 改了 YAML 但值没变 | 环境变量覆盖了 YAML | 检查同名环境变量；启动日志的 `config_file` 可确认加载了哪个文件 |
| 环境变量不生效 | 变量名拼错，或用了错误的组件前缀 | 查结构体上的 `env` 标签；数据库是 `DB_` 不是 `DATABASE_` |
| 设置 `0` 后启动失败 | 该字段不允许零值（如读超时），或 `0` 没有通过环境变量显式设置 | 看错误信息；只有明确允许 0 的字段（如 `HTTP_REQUEST_TIMEOUT`、`HTTP_IDLE_TIMEOUT`、`CORS_MAX_AGE`、`SECURITY_OPERATION_LOG_BODY_LIMIT`）才能用 0 |
| 生产环境拒绝启动 | 使用了占位密钥、通配 CORS + 凭证、`APP_DEBUG=true` | 换成真实 Secret，收窄 Origin，关闭 debug |
| 本地能跑，容器里找不到配置 | 容器工作目录与配置文件路径不一致 | 挂载配置并设置 `CONFIG_FILE`，不要依赖相对路径 |
| 修改配置后没有生效 | 配置只在启动时读取，没有热加载 | 重启进程 |

本地开发的最小配置方式（前提是 PostgreSQL、Redis 已可用）：

```bash
cd backend

# config.yaml 已在仓库中；JWT_SECRET 必填且至少 32 字符
cat > .env <<'EOF'
JWT_SECRET=dev-only-secret-at-least-32-characters
EOF

go run ./cmd/server
```

生产环境不要把 `.env` 打进镜像；用 ConfigMap 按需覆盖 L1/L2，用 Secret 注入 L3。清单中的 `${...}` 由云效流水线在 apply 前替换，K8s 本身不解析；现成清单见 [`backend/deploy/k8s/`](../../backend/deploy/k8s/)。

## 8. 相关文件

- 配置实现：[`backend/internal/config/`](../../backend/internal/config/)
- 全量配置模板：[`backend/config.yaml`](../../backend/config.yaml)
- 环境变量覆盖示例：[`backend/.env.example`](../../backend/.env.example)
- 配置测试：[`backend/internal/config/config_test.go`](../../backend/internal/config/config_test.go)
- Kubernetes 配置清单：[`backend/deploy/k8s/README.md`](../../backend/deploy/k8s/README.md)
- 架构说明中的配置章节：[`docs/architecture.md`](../architecture.md)
- 部署与环境变量注入：[`docs/deployment.md`](../deployment.md)
