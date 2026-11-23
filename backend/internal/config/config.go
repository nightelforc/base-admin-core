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
// 敏感项不要写入 config.yaml，应只通过环境变量或 Secret 提供。
//
// Load() 是唯一入口：先读取配置文件，再应用环境变量并校验。配置文件必须写全所有
// 配置项；缺失的字段会保持 Go 零值，通常会被启动校验拒绝。跨字段问题会一次性全部
// 列出，配置有误的进程绝不带病启动。
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

// validateTimeouts 保证数据库语句超时早于请求超时。反过来的话，请求早已返回
// 504、连接也还给了池子，数据库里的语句却还在跑，这个超时等于白设。
func (c *Config) validateTimeouts() problems {
	var p problems
	if c.HTTP.RequestTimeout > 0 && c.Database.StatementTimeout > 0 &&
		c.Database.StatementTimeout >= c.HTTP.RequestTimeout {
		p.add("DB_STATEMENT_TIMEOUT must be shorter than HTTP_REQUEST_TIMEOUT")
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
		"redis_mode":   c.Redis.Mode,
		"redis_addrs":  strings.Join(c.Redis.Addrs, ","),
		"redis_master": c.Redis.MasterName,
		"redis_db":     c.Redis.DB,
		"redis_prefix": c.Redis.KeyPrefix,
		"redis_tls":    c.Redis.TLSEnabled,
		"jwt_issuer":   c.JWT.Issuer,
		"jwt_access":   c.JWT.AccessTTL.String(),
		"jwt_refresh":  c.JWT.RefreshTTL.String(),
		"log_level":    c.Log.Level,
		"log_format":   c.Log.Format,
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
