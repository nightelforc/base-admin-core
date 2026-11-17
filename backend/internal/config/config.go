// Package config loads every runtime setting from environment variables.
//
// There is deliberately no YAML/JSON configuration file: each setting is bound
// to exactly one environment variable, grouped by a short prefix so that the
// deployment surface stays explicit and greppable:
//
//	APP_*        application identity
//	HTTP_*       http server tuning
//	LOG_*        logging
//	DB_*         postgresql connection (including the table prefix)
//	REDIS_*      redis connection and key namespace
//	JWT_*        token signing
//	SECURITY_*   password policy, login throttling, audit logging
//	CORS_*       cross-origin policy
//	BOOTSTRAP_*  first-run administrator account
//
// Load() is the only entry point: it parses, applies defaults, validates and
// fails fast with an aggregated error so a misconfigured process never starts.
package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"

	"github.com/base-admin/base-admin-core/backend/pkg/postgres"
	"github.com/base-admin/base-admin-core/backend/pkg/redis"
)

// Runtime environments understood by the application.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// tablePrefixPattern keeps the prefix safe to interpolate into DDL identifiers
// (migrations) and into generated table names.
var tablePrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$|^$`)

// Config is the root of the runtime configuration tree.
type Config struct {
	App       App       `envPrefix:"APP_"`
	HTTP      HTTP      `envPrefix:"HTTP_"`
	Log       Log       `envPrefix:"LOG_"`
	Database  Database  `envPrefix:"DB_"`
	Redis     Redis     `envPrefix:"REDIS_"`
	JWT       JWT       `envPrefix:"JWT_"`
	Security  Security  `envPrefix:"SECURITY_"`
	CORS      CORS      `envPrefix:"CORS_"`
	Bootstrap Bootstrap `envPrefix:"BOOTSTRAP_"`
}

// App holds application identity.
type App struct {
	Name     string `env:"NAME" envDefault:"base-admin"`
	Env      string `env:"ENV" envDefault:"development" validate:"required,oneof=development test production"`
	Timezone string `env:"TIMEZONE" envDefault:"Asia/Shanghai"`
	Debug    bool   `env:"DEBUG" envDefault:"false"`
}

// IsProduction reports whether the process runs with production settings.
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// HTTP holds http server tuning.
type HTTP struct {
	Addr              string        `env:"ADDR" envDefault:":8080"`
	RequestTimeout    time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"10s"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"30s"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" envDefault:"60s"`
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s"`
	MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"4194304"`
	TrustedProxies    []string      `env:"TRUSTED_PROXIES" envSeparator:","`
}

// Log holds logging configuration.
type Log struct {
	Level     string `env:"LEVEL" envDefault:"info" validate:"required,oneof=debug info warn error"`
	Format    string `env:"FORMAT" envDefault:"text" validate:"required,oneof=text json"`
	AddSource bool   `env:"ADD_SOURCE" envDefault:"false"`
}

// Database holds the PostgreSQL connection settings.
type Database struct {
	Host     string `env:"HOST" envDefault:"127.0.0.1" validate:"required"`
	Port     int    `env:"PORT" envDefault:"5432" validate:"required,min=1,max=65535"`
	User     string `env:"USER" envDefault:"postgres" validate:"required"`
	Password string `env:"PASSWORD"`
	Name     string `env:"NAME" envDefault:"postgres" validate:"required"`
	SSLMode  string `env:"SSLMODE" envDefault:"disable" validate:"required,oneof=disable allow prefer require verify-ca verify-full"`
	TimeZone string `env:"TIMEZONE" envDefault:"Asia/Shanghai"`

	// TablePrefix is prepended to every table created and queried by the
	// application, so several deployments can share one schema.
	TablePrefix string `env:"TABLE_PREFIX" envDefault:"base_"`

	MaxOpenConns       int           `env:"MAX_OPEN_CONNS" envDefault:"25" validate:"min=1"`
	MaxIdleConns       int           `env:"MAX_IDLE_CONNS" envDefault:"10" validate:"min=0"`
	ConnMaxLifetime    time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"30m"`
	ConnMaxIdleTime    time.Duration `env:"CONN_MAX_IDLE_TIME" envDefault:"5m"`
	SlowQueryThreshold time.Duration `env:"SLOW_QUERY_THRESHOLD" envDefault:"200ms"`
	LogLevel           string        `env:"LOG_LEVEL" envDefault:"warn" validate:"required,oneof=silent error warn info"`

	// Migrate runs the embedded SQL migrations on start-up.
	Migrate bool `env:"MIGRATE" envDefault:"true"`
}

// DSN renders a libpq / pgx compatible connection string. The query string is
// assembled by hand: url.Values would escape the slash in a time zone such as
// Asia/Shanghai, and PostgreSQL then rejects the value as an unknown zone.
func (d Database) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   "/" + d.Name,
	}

	query := "sslmode=" + d.SSLMode
	if d.TimeZone != "" {
		query += "&TimeZone=" + d.TimeZone
	}
	u.RawQuery = query
	return u.String()
}

// Options 把数据库配置转换成 postgres 包需要的连接参数。
//
// Logger 有意留空，由装配层（main 的 run）注入：它是运行时对象而不是配置项，
// 把它塞进方法签名会让本包平白依赖日志实现，而且以后每多一个运行时依赖，
// 签名就要多长一个参数。postgres.Options 自己声明了 Logger 为 nil 时回落到
// slog.Default，所以这个返回值本身就是完整可用的。
func (d Database) Options() postgres.Options {
	return postgres.Options{
		DSN:                d.DSN(),
		TablePrefix:        d.TablePrefix,
		MaxOpenConns:       d.MaxOpenConns,
		MaxIdleConns:       d.MaxIdleConns,
		ConnMaxLifetime:    d.ConnMaxLifetime,
		ConnMaxIdleTime:    d.ConnMaxIdleTime,
		LogLevel:           d.LogLevel,
		SlowQueryThreshold: d.SlowQueryThreshold,
	}
}

// Redis holds the Redis connection settings.
type Redis struct {
	Addr         string        `env:"ADDR" envDefault:"127.0.0.1:6379" validate:"required"`
	Username     string        `env:"USERNAME"`
	Password     string        `env:"PASSWORD"`
	DB           int           `env:"DB" envDefault:"0" validate:"min=0"`
	KeyPrefix    string        `env:"KEY_PREFIX" envDefault:"base-admin:"`
	DialTimeout  time.Duration `env:"DIAL_TIMEOUT" envDefault:"5s"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT" envDefault:"3s"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT" envDefault:"3s"`
	PoolSize     int           `env:"POOL_SIZE" envDefault:"20" validate:"min=1"`
}

// Options 把 Redis 配置转换成客户端的连接参数。
//
// 映射放在这里而不是 main：配置项与环境变量的对应关系只有本包知道，新增字段时
// 只需同步这一个方法，不会出现"加了 env 却忘了传给客户端"的静默失效。Redis 的
// Options 全是纯数据，所以这里是完整映射，不需要像 Database 那样留运行时字段。
func (r Redis) Options() redis.Options {
	return redis.Options{
		Addr:         r.Addr,
		Username:     r.Username,
		Password:     r.Password,
		DB:           r.DB,
		DialTimeout:  r.DialTimeout,
		ReadTimeout:  r.ReadTimeout,
		WriteTimeout: r.WriteTimeout,
		PoolSize:     r.PoolSize,
	}
}

// JWT holds token signing configuration.
type JWT struct {
	Secret     string        `env:"SECRET" validate:"required,min=32"`
	Issuer     string        `env:"ISSUER" envDefault:"base-admin"`
	Audience   string        `env:"AUDIENCE" envDefault:"base-admin-web"`
	AccessTTL  time.Duration `env:"ACCESS_TTL" envDefault:"30m"`
	RefreshTTL time.Duration `env:"REFRESH_TTL" envDefault:"168h"`
}

// Security holds authentication hardening and audit settings.
type Security struct {
	BcryptCost          int           `env:"BCRYPT_COST" envDefault:"10" validate:"min=4,max=31"`
	PasswordMinLength   int           `env:"PASSWORD_MIN_LENGTH" envDefault:"8" validate:"min=6,max=64"`
	LoginMaxAttempts    int           `env:"LOGIN_MAX_ATTEMPTS" envDefault:"5" validate:"min=1"`
	LoginWindow         time.Duration `env:"LOGIN_WINDOW" envDefault:"15m"`
	LoginBlockDuration  time.Duration `env:"LOGIN_BLOCK_DURATION" envDefault:"15m"`
	OperationLogEnabled bool          `env:"OPERATION_LOG_ENABLED" envDefault:"true"`
	OperationLogLimit   int           `env:"OPERATION_LOG_BODY_LIMIT" envDefault:"2048" validate:"min=0"`
}

// CORS holds the cross-origin policy. The frontend normally talks to the API
// through the dev-server proxy, so this is only needed for split deployments.
type CORS struct {
	AllowedOrigins   []string      `env:"ALLOWED_ORIGINS" envSeparator:"," envDefault:"*"`
	AllowedMethods   []string      `env:"ALLOWED_METHODS" envSeparator:"," envDefault:"GET,POST,PUT,PATCH,DELETE,OPTIONS"`
	AllowedHeaders   []string      `env:"ALLOWED_HEADERS" envSeparator:"," envDefault:"Origin,Content-Type,Accept,Authorization,X-Request-ID,X-Requested-With"`
	ExposeHeaders    []string      `env:"EXPOSE_HEADERS" envSeparator:"," envDefault:"X-Request-ID"`
	AllowCredentials bool          `env:"ALLOW_CREDENTIALS" envDefault:"false"`
	MaxAge           time.Duration `env:"MAX_AGE" envDefault:"12h"`
}

// Bootstrap describes the administrator account created on first start.
type Bootstrap struct {
	AdminUsername string `env:"ADMIN_USERNAME" envDefault:"admin"`
	AdminPassword string `env:"ADMIN_PASSWORD"`
	AdminNickname string `env:"ADMIN_NICKNAME" envDefault:"超级管理员"`
	AdminEmail    string `env:"ADMIN_EMAIL" envDefault:""`
}

// Load parses the environment, then validates the resulting tree.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks every constraint that cannot be expressed as a single struct
// tag: cross-field consistency, identifier safety and time zone existence.
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
	if c.App.IsProduction() {
		if strings.Contains(strings.ToLower(c.JWT.Secret), "changeme") {
			problems = append(problems, "JWT_SECRET must be replaced with a random value in production")
		}
		if c.CORS.AllowCredentials && hasWildcard(c.CORS.AllowedOrigins) {
			problems = append(problems, "CORS_ALLOW_CREDENTIALS requires explicit CORS_ALLOWED_ORIGINS (wildcard is not allowed)")
		}
		if c.App.Debug {
			problems = append(problems, "APP_DEBUG must be false in production")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// Location returns the application time zone, falling back to UTC when the
// configured value cannot be resolved.
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.App.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Summary returns a log-friendly view of the configuration with every secret
// masked, so start-up logs never leak credentials.
func (c *Config) Summary() map[string]any {
	return map[string]any{
		"app":          c.App.Name,
		"env":          c.App.Env,
		"debug":        c.App.Debug,
		"http_addr":    c.HTTP.Addr,
		"db_host":      fmt.Sprintf("%s:%d/%s", c.Database.Host, c.Database.Port, c.Database.Name),
		"db_user":      c.Database.User,
		"db_ssl":       c.Database.SSLMode,
		"table_prefix": c.Database.TablePrefix,
		"migrate":      c.Database.Migrate,
		"redis_addr":   c.Redis.Addr,
		"redis_db":     c.Redis.DB,
		"redis_prefix": c.Redis.KeyPrefix,
		"jwt_issuer":   c.JWT.Issuer,
		"jwt_access":   c.JWT.AccessTTL.String(),
		"jwt_refresh":  c.JWT.RefreshTTL.String(),
		"log_level":    c.Log.Level,
		"log_format":   c.Log.Format,
	}
}

func hasWildcard(values []string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "*" {
			return true
		}
	}
	return false
}
