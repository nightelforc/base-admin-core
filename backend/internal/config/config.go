// Package config 按“内置默认值 < config.yaml < 环境变量”三层装配运行时配置。
//
// config.yaml 是全量配置的唯一声明处，按重要性分为三层：
//
//	L1 基础运行  APP_* / LOG_* / HTTP_* / BOOTSTRAP_*
//	L2 基础设施  DB_* / REDIS_* / CORS_*
//	L3 安全敏感  JWT_* / SECURITY_*，以及 DB_PASSWORD、REDIS_PASSWORD 等口令
//
// 环境变量只用于覆盖，适合由 ConfigMap / Secret 注入；变量名由组件前缀加字段名组成，
// 例如 app.name 对应 APP_NAME，http.request_timeout 对应 HTTP_REQUEST_TIMEOUT。
// 敏感项不要写入 config.yaml，应只通过环境变量或 Secret 提供。
//
// Load() 是唯一入口：先套用默认值，再读取配置文件，最后应用环境变量并校验。跨字段
// 问题会一次性全部列出，配置有误的进程绝不带病启动。
//
// 每个组件一个文件，文件名就是它配置的组件（app、http、database、redis……）。
// 因此新增配置项只需改对应组件的文件；config.go 只保留根配置树、加载顺序与启动期校验。
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"

	"github.com/nightelforc/base-admin-core/backend/pkg/redis"
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
}

// Load 按“默认值 -> config.yaml -> 环境变量”的顺序装配配置并校验。
func Load() (*Config, error) {
	cfg := &Config{}

	// 第一次解析只读取 envDefault：传入空环境，避免环境变量提前覆盖配置文件。
	if err := env.ParseWithOptions(cfg, env.Options{Environment: map[string]string{}}); err != nil {
		return nil, fmt.Errorf("load config defaults: %w", err)
	}

	path, err := resolveConfigFile()
	if err != nil {
		return nil, err
	}
	if err := loadConfigFile(path, cfg); err != nil {
		return nil, err
	}

	// 第二次解析只应用显式设置的环境变量。把默认值 tag 指向不存在的名字，
	// 防止 envDefault 再次覆盖 config.yaml 中的值。
	if err := env.ParseWithOptions(cfg, env.Options{DefaultValueTagName: "envDefaultDisabled"}); err != nil {
		return nil, fmt.Errorf("apply environment overrides: %w", err)
	}

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

// loadConfigFile 把 YAML 反序列化到已经填好默认值的配置树上。KnownFields 让拼错的
// 键直接报错，而不是被静默忽略后继续用默认值运行。
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

// Validate 校验无法用单个 struct tag 表达的约束：跨字段一致性、标识符安全性
// 与时区是否存在；同时拒绝会让服务器失去超时保护的零值或负值。
func (c *Config) Validate() error {
	if err := validator.New().Struct(c); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	var problems []string

	if !tablePrefixPattern.MatchString(c.Database.TablePrefix) {
		problems = append(problems, fmt.Sprintf("DB_TABLE_PREFIX %q must be lowercase letters, digits and underscores, starting with a letter (or be empty)", c.Database.TablePrefix))
	}
	if c.Redis.KeyPrefix == "" {
		problems = append(problems, "REDIS_KEY_PREFIX must not be empty")
	}
	// 两种模式各自必填的项不同：单机要恰好一个数据节点地址，哨兵要主节点名与
	// 至少一个哨兵地址。配错就退回单机是最危险的失败方式——请求会打到别的实例
	// 上，因此在这里拦下。空字符串地址也要拒绝：go-redis 收到空 Addr 会回落到
	// 127.0.0.1:6379，等于静默连错地方。
	for _, addr := range c.Redis.Addrs {
		if strings.TrimSpace(addr) == "" {
			problems = append(problems, "REDIS_ADDRS must not contain empty addresses")
			break
		}
	}
	switch c.Redis.Mode {
	case redis.ModeStandalone:
		if len(c.Redis.Addrs) != 1 {
			problems = append(problems, fmt.Sprintf("REDIS_ADDRS must contain exactly one address when REDIS_MODE=standalone, got %d", len(c.Redis.Addrs)))
		}
	case redis.ModeSentinel:
		if strings.TrimSpace(c.Redis.MasterName) == "" {
			problems = append(problems, "REDIS_MASTER_NAME is required when REDIS_MODE=sentinel")
		}
		if len(c.Redis.Addrs) == 0 {
			problems = append(problems, "REDIS_ADDRS must list at least one sentinel address when REDIS_MODE=sentinel")
		}
	}
	if _, err := time.LoadLocation(c.App.Timezone); err != nil {
		problems = append(problems, fmt.Sprintf("APP_TIMEZONE %q is not a known IANA time zone", c.App.Timezone))
	}
	if _, err := time.LoadLocation(c.Database.TimeZone); err != nil {
		problems = append(problems, fmt.Sprintf("DB_TIMEZONE %q is not a known IANA time zone", c.Database.TimeZone))
	}
	if c.JWT.AccessTTL <= 0 || c.JWT.RefreshTTL <= 0 {
		problems = append(problems, "JWT_ACCESS_TTL and JWT_REFRESH_TTL must be positive")
	}
	if c.JWT.RefreshTTL <= c.JWT.AccessTTL {
		problems = append(problems, "JWT_REFRESH_TTL must be greater than JWT_ACCESS_TTL")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		problems = append(problems, "DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	if c.HTTP.MaxBodyBytes <= 0 {
		problems = append(problems, "HTTP_MAX_BODY_BYTES must be positive")
	}

	// 超时取 0 或负数时，net/http 与 go-redis 会按"不超时"处理，慢连接或故障
	// 实例就可能把进程拖住，因此统一在启动期拦下。HTTP_IDLE_TIMEOUT 允许为 0，
	// 此时 net/http 回退到 ReadTimeout，属于有意义的配置。
	for _, item := range []struct {
		name  string
		value time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", c.HTTP.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", c.HTTP.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", c.HTTP.WriteTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", c.HTTP.ShutdownTimeout},
		{"REDIS_DIAL_TIMEOUT", c.Redis.DialTimeout},
		{"REDIS_READ_TIMEOUT", c.Redis.ReadTimeout},
		{"REDIS_WRITE_TIMEOUT", c.Redis.WriteTimeout},
		{"SECURITY_LOGIN_WINDOW", c.Security.LoginWindow},
		{"SECURITY_LOGIN_BLOCK_DURATION", c.Security.LoginBlockDuration},
	} {
		if item.value <= 0 {
			problems = append(problems, item.name+" must be positive")
		}
	}
	if c.HTTP.IdleTimeout < 0 {
		problems = append(problems, "HTTP_IDLE_TIMEOUT must not be negative")
	}
	// RequestTimeout 取 0 表示不限制，负值没有意义；启用时必须早于 WriteTimeout，
	// 否则连接会先被写超时掐断，504 响应发不出去。
	if c.HTTP.RequestTimeout < 0 {
		problems = append(problems, "HTTP_REQUEST_TIMEOUT must not be negative")
	}
	if c.HTTP.RequestTimeout > 0 && c.HTTP.WriteTimeout > 0 && c.HTTP.RequestTimeout >= c.HTTP.WriteTimeout {
		problems = append(problems, "HTTP_REQUEST_TIMEOUT must be less than HTTP_WRITE_TIMEOUT")
	}
	if c.CORS.MaxAge < 0 {
		problems = append(problems, "CORS_MAX_AGE must not be negative")
	}

	if c.App.IsProduction() {
		if hasPlaceholderSecret(c.JWT.Secret) {
			problems = append(problems, "JWT_SECRET must be replaced with a random value in production")
		}
		if c.CORS.AllowCredentials && hasWildcard(c.CORS.AllowedOrigins) {
			problems = append(problems, "CORS_ALLOW_CREDENTIALS requires explicit CORS_ALLOWED_ORIGINS (wildcard is not allowed)")
		}
		if c.App.Debug {
			problems = append(problems, "APP_DEBUG must be false in production")
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
				problems = append(problems, item.name+" must be replaced with a real value in production")
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// Location 返回应用时区；配置值无法解析时回落到 UTC。
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.App.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Summary 返回适合打日志的配置视图，所有敏感字段均已脱敏，启动日志不会泄漏凭据。
func (c *Config) Summary() map[string]any {
	return map[string]any{
		"config_file":  c.source,
		"app":          c.App.Name,
		"env":          c.App.Env,
		"debug":        c.App.Debug,
		"http_addr":    c.HTTP.Addr,
		"http_timeout": c.HTTP.RequestTimeout.String(),
		"db_host":      fmt.Sprintf("%s:%d/%s", c.Database.Host, c.Database.Port, c.Database.Name),
		"db_user":      c.Database.User,
		"db_ssl":       c.Database.SSLMode,
		"table_prefix": c.Database.TablePrefix,
		"migrate":      c.Database.Migrate,
		"redis_mode":   c.Redis.Mode,
		"redis_addrs":  strings.Join(c.Redis.Addrs, ","),
		"redis_master": c.Redis.MasterName,
		"redis_db":     c.Redis.DB,
		"redis_prefix": c.Redis.KeyPrefix,
		"jwt_issuer":   c.JWT.Issuer,
		"jwt_access":   c.JWT.AccessTTL.String(),
		"jwt_refresh":  c.JWT.RefreshTTL.String(),
		"log_level":    c.Log.Level,
		"log_format":   c.Log.Format,
	}
}

// hasPlaceholderSecret 判断密钥是否仍是示例占位值。只查 "changeme" 会漏掉
// .env.example 里的 "change-me"，因此把常见占位词一起拦下。
func hasPlaceholderSecret(value string) bool {
	lower := strings.ToLower(value)
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
