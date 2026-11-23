// Package config 按“config.yaml < 环境变量”两层装配运行时配置。
//
// config.yaml 是全量配置的唯一声明处，也是默认值的唯一来源；代码不再用
// envDefault 兜底。配置文件按重要性分为三层：
//
//	L1 基础运行  APP_* / LOG_* / HTTP_* / BOOTSTRAP_*
//	L2 基础设施  DB_* / REDIS_* / CORS_*
//	L3 安全敏感  JWT_* / SECURITY_*，以及 DB_PASSWORD、REDIS_PASSWORD 等口令
//
// 环境变量只用于覆盖，适合由 ConfigMap / Secret 注入；变量名由组件前缀加字段名组成，
// 例如 app.name 对应 APP_NAME，http.request_timeout 对应 HTTP_REQUEST_TIMEOUT。
// 敏感项不要写入 config.yaml，应只通过环境变量或 Secret 提供；环境变量也可以只给
// 路径（X_FILE），由程序读取挂载进来的文件内容，见 secrets.go。
//
// Load() 是唯一入口：先读取配置文件，再应用环境变量、读取密钥文件并校验。配置文件
// 必须写全所有配置项；缺失的字段会保持 Go 零值，通常会被启动校验拒绝。跨字段问题会
// 一次性全部列出，配置有误的进程绝不带病启动。
//
// 每个组件一个文件，文件名就是它配置的组件（app、http、database、redis……）。
// 因此新增配置项需要同时改对应组件的文件与 config.yaml；config.go 只保留根配置树、
// 加载顺序与启动期校验。
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// 应用支持的运行环境。
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// ConfigFileEnv 指定配置文件路径；未设置时依次查找 ./config.yaml 与 ./backend/config.yaml。
const ConfigFileEnv = "CONFIG_FILE"

// Config 是运行时配置树的根。
type Config struct {
	App       App       `envPrefix:"APP_" yaml:"app"`
	HTTP      HTTP      `envPrefix:"HTTP_" yaml:"http"`
	Log       Log       `envPrefix:"LOG_" yaml:"log"`
	Database  Database  `envPrefix:"DB_" yaml:"database"`
	Redis     Redis     `envPrefix:"REDIS_" yaml:"redis"`
	JWT       JWT       `envPrefix:"JWT_" yaml:"jwt"`
	Security  Security  `envPrefix:"SECURITY_" yaml:"security"`
	CORS      CORS      `envPrefix:"CORS_" yaml:"cors"`
	Bootstrap Bootstrap `envPrefix:"BOOTSTRAP_" yaml:"bootstrap"`

	// source 记录本次实际加载的配置文件路径，仅用于启动日志，不参与序列化。
	source string

	// secretFiles 记录本次经 X_FILE 注入的项（只记变量名，不记内容），Summary 会
	// 带上：运维看启动日志就能确认"这台机器用的是挂载密钥"而不是环境变量明文。
	secretFiles []string
}

// Load 按“config.yaml -> 环境变量”的顺序装配配置并校验。
func Load() (*Config, error) {
	cfg := &Config{}

	path, err := resolveConfigFile()
	if err != nil {
		return nil, err
	}
	if err := loadConfigFile(path, cfg); err != nil {
		return nil, err
	}

	// 环境变量只覆盖显式设置的键，未设置的字段保留 config.yaml 中的值。
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("apply environment overrides: %w", err)
	}

	// 密钥文件在环境变量之后读取：X_FILE 与 X 写入的是同一个字段，后续校验
	// 因此对两种来源一视同仁。
	if err := applySecretFiles(cfg); err != nil {
		return nil, err
	}

	// 会话标签在这里一次性派生：Database 不暴露设置入口，日志与 PostgreSQL 用的
	// 必然是同一个 app.name。放在校验之前，校验失败时也不影响错误信息。
	cfg.Database.sessionLabel = strings.TrimSpace(cfg.App.Name)

	cfg.source = path
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// resolveConfigFile 返回实际使用的配置文件。显式设置 CONFIG_FILE 时只认该路径；
// 否则依次尝试仓库根目录和 backend 目录，便于从仓库根或 backend 目录启动。
func resolveConfigFile() (string, error) {
	if path := strings.TrimSpace(os.Getenv(ConfigFileEnv)); path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("%s=%q: %w", ConfigFileEnv, path, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s=%q points to a directory", ConfigFileEnv, path)
		}
		return path, nil
	}

	candidates := []string{"config.yaml", filepath.Join("backend", "config.yaml")}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("config file not found: set %s or create one of: %s", ConfigFileEnv, strings.Join(candidates, ", "))
}

// loadConfigFile 把 YAML 反序列化到空白配置树上。KnownFields 让拼错的键直接报错，
// 而不是被静默忽略后继续用零值运行。
func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("parse config file %q: file is empty", path)
		}
		return fmt.Errorf("parse config file %q: %w", path, err)
	}
	return nil
}

// Source 返回本次加载的配置文件路径；Load 成功时必非空。
func (c *Config) Source() string { return c.source }

// problems 收集一次配置校验中的全部问题，便于一次性返回。
type problems []string

func (p *problems) add(message string) {
	*p = append(*p, message)
}

func (p *problems) addf(format string, args ...any) {
	*p = append(*p, fmt.Sprintf(format, args...))
}

func (p *problems) extend(other problems) {
	*p = append(*p, other...)
}

func (p problems) err() error {
	if len(p) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(p, "\n  - "))
}

// Validate 先执行结构体 tag 校验，再汇总各组件自己的跨字段校验，最后检查只在
// 生产环境生效的部署策略。所有问题一次性返回，进程不会带病启动。
func (c *Config) Validate() error {
	if err := validator.New().Struct(c); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var p problems
	p.extend(c.App.validate())
	p.extend(c.HTTP.validate())
	p.extend(c.Database.validate())
	p.extend(c.Redis.validate())
	p.extend(c.JWT.validate())
	p.extend(c.Security.validate())
	p.extend(c.CORS.validate())
	p.extend(c.validateTimeouts())
	p.extend(c.validateProduction())
	return p.err()
}

// validateTimeouts 保证数据库侧的等待都早于请求超时。反过来的话，请求早已返回
// 504、连接也还给了池子，数据库里的语句却还在跑，这些超时等于白设。
func (c *Config) validateTimeouts() problems {
	var p problems
	if c.HTTP.RequestTimeout > 0 && c.Database.StatementTimeout > 0 &&
		c.Database.StatementTimeout >= c.HTTP.RequestTimeout {
		p.add("DB_STATEMENT_TIMEOUT must be shorter than HTTP_REQUEST_TIMEOUT")
	}
	if c.HTTP.RequestTimeout > 0 && c.Database.ConnectTimeout > 0 &&
		c.Database.ConnectTimeout >= c.HTTP.RequestTimeout {
		p.add("DB_CONNECT_TIMEOUT must be shorter than HTTP_REQUEST_TIMEOUT")
	}
	return p
}

// validateProduction 汇总只在生产环境生效的部署策略。这些规则跨多个组件，
// 不适合塞进单个结构体，因此留在根配置上。
func (c *Config) validateProduction() problems {
	var p problems
	if !c.App.IsProduction() {
		return p
	}

	if hasPlaceholderSecret(c.JWT.Secret) {
		p.add("JWT_SECRET must be replaced with a random value in production")
	}
	if c.CORS.AllowCredentials && hasWildcard(c.CORS.AllowedOrigins) {
		p.add("CORS_ALLOW_CREDENTIALS requires explicit CORS_ALLOWED_ORIGINS (wildcard is not allowed)")
	}
	if c.App.Debug {
		p.add("APP_DEBUG must be false in production")
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{"DB_PASSWORD", c.Database.Password},
		{"REDIS_PASSWORD", c.Redis.Password},
		{"BOOTSTRAP_ADMIN_PASSWORD", c.Bootstrap.AdminPassword},
	} {
		if item.value != "" && hasPlaceholderSecret(item.value) {
			p.addf("%s must be replaced with a real value in production", item.name)
		}
	}
	return p
}

// Location 返回应用时区；配置值无法解析时回落到 UTC。
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.App.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Summary 返回适合打日志的配置视图：键名就是 config.yaml 里的路径，启动日志因此
// 可以和配置文件逐条对上，也可以按前缀 grep（例如 database.）。
//
// 返回值里没有秘密：口令与 JWT 密钥一律不出现，连长度也不报——长度足以反推弱密钥。
// 唯一与密钥有关的键是 secrets.from_files，它只列"哪些项走了文件注入"的变量名。
func (c *Config) Summary() map[string]any {
	// 0 表示"未设置/关闭"，写 0s 容易被误读成零超时，因此单独标注。
	off := func(d time.Duration) string {
		if d == 0 {
			return "off"
		}
		return d.String()
	}
	// 空列表在日志里是空值，和"忘了配"分不开；空字符串同理。
	list := func(values []string) string {
		if len(values) == 0 {
			return "none"
		}
		return strings.Join(values, ",")
	}
	text := func(value string) string {
		if strings.TrimSpace(value) == "" {
			return "none"
		}
		return value
	}

	files := append([]string(nil), c.secretFiles...)
	slices.Sort(files)

	// 引导账号为空表示跳过引导，此时也不存在"随机生成密码"这回事。
	generated := c.Bootstrap.AdminUsername != "" && c.Bootstrap.AdminPassword == ""

	return map[string]any{
		"config_file": c.source,

		"app.name":     c.App.Name,
		"app.env":      c.App.Env,
		"app.timezone": c.App.Timezone,
		"app.debug":    c.App.Debug,

		"log.level":      c.Log.Level,
		"log.format":     c.Log.Format,
		"log.add_source": c.Log.AddSource,

		"http.addr":                c.HTTP.Addr,
		"http.read_header_timeout": off(c.HTTP.ReadHeaderTimeout),
		"http.read_timeout":        off(c.HTTP.ReadTimeout),
		"http.write_timeout":       off(c.HTTP.WriteTimeout),
		"http.request_timeout":     off(c.HTTP.RequestTimeout),
		"http.idle_timeout":        off(c.HTTP.IdleTimeout),
		"http.shutdown_timeout":    off(c.HTTP.ShutdownTimeout),
		"http.max_body_bytes":      c.HTTP.MaxBodyBytes,
		"http.trusted_proxies":     list(c.HTTP.TrustedProxies),

		"database.host":               c.Database.Host,
		"database.port":               c.Database.Port,
		"database.name":               c.Database.Name,
		"database.user":               c.Database.User,
		"database.sslmode":            c.Database.SSLMode,
		"database.timezone":           text(c.Database.TimeZone),
		"database.table_prefix":       text(c.Database.TablePrefix),
		"database.max_open_conns":     c.Database.MaxOpenConns,
		"database.max_idle_conns":     c.Database.MaxIdleConns,
		"database.conn_max_lifetime":  off(c.Database.ConnMaxLifetime),
		"database.conn_max_idle_time": off(c.Database.ConnMaxIdleTime),
		"database.statement_timeout":  off(c.Database.StatementTimeout),
		"database.connect_timeout":    off(c.Database.ConnectTimeout),
		"database.slow_query":         off(c.Database.SlowQueryThreshold),
		"database.log_level":          c.Database.LogLevel,

		"redis.mode":             c.Redis.Mode,
		"redis.addrs":            list(c.Redis.Addrs),
		"redis.master_name":      text(c.Redis.MasterName),
		"redis.db":               c.Redis.DB,
		"redis.key_prefix":       c.Redis.KeyPrefix,
		"redis.dial_timeout":     off(c.Redis.DialTimeout),
		"redis.read_timeout":     off(c.Redis.ReadTimeout),
		"redis.write_timeout":    off(c.Redis.WriteTimeout),
		"redis.pool_size":        c.Redis.PoolSize,
		"redis.route_by_latency": c.Redis.RouteByLatency,
		"redis.route_randomly":   c.Redis.RouteRandomly,
		"redis.tls_enabled":      c.Redis.TLSEnabled,

		"jwt.issuer":      c.JWT.Issuer,
		"jwt.audience":    c.JWT.Audience,
		"jwt.access_ttl":  off(c.JWT.AccessTTL),
		"jwt.refresh_ttl": off(c.JWT.RefreshTTL),

		"security.bcrypt_cost":              c.Security.BcryptCost,
		"security.password_min_length":      c.Security.PasswordMinLength,
		"security.login_max_attempts":       c.Security.LoginMaxAttempts,
		"security.login_window":             off(c.Security.LoginWindow),
		"security.login_block_duration":     off(c.Security.LoginBlockDuration),
		"security.operation_log_enabled":    c.Security.OperationLogEnabled,
		"security.operation_log_body_limit": c.Security.OperationLogBodyLimit,

		"cors.allowed_origins":   list(c.CORS.AllowedOrigins),
		"cors.allow_credentials": c.CORS.AllowCredentials,
		"cors.max_age":           off(c.CORS.MaxAge),

		"bootstrap.admin_username":     text(c.Bootstrap.AdminUsername),
		"bootstrap.password_generated": generated,

		"secrets.from_files": list(files),
	}
}

// hasPlaceholderSecret 判断密钥是否仍是示例占位值。除常见占位词外，
// 也把 <JWT_SECRET>、${JWT_SECRET} 这类模板占位符视为未替换，避免 Secret
// 清单里的占位名称绕过生产校验。
func hasPlaceholderSecret(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "<") && strings.HasSuffix(lower, ">") {
		return true
	}
	if strings.HasPrefix(lower, "${") && strings.HasSuffix(lower, "}") {
		return true
	}
	for _, marker := range []string{"changeme", "change-me", "replace-me", "placeholder", "example"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// hasWildcard 判断来源列表里是否包含通配符 "*"。
func hasWildcard(values []string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "*" {
			return true
		}
	}
	return false
}
