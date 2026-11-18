package config

import (
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/nightelforc/base-admin-core/backend/pkg/postgres"
)

// tablePrefixPattern 保证表前缀可以安全地拼进 DDL 标识符（迁移）和生成的表名。
var tablePrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$|^$`)

// Database 保存 PostgreSQL 连接设置。
type Database struct {
	Host     string `env:"HOST" envDefault:"127.0.0.1" validate:"required" yaml:"host"`
	Port     int    `env:"PORT" envDefault:"5432" validate:"required,min=1,max=65535" yaml:"port"`
	User     string `env:"USER" envDefault:"postgres" validate:"required" yaml:"user"`
	Password string `env:"PASSWORD" yaml:"password"`
	Name     string `env:"NAME" envDefault:"postgres" validate:"required" yaml:"name"`
	SSLMode  string `env:"SSLMODE" envDefault:"disable" validate:"required,oneof=disable allow prefer require verify-ca verify-full" yaml:"sslmode"`
	TimeZone string `env:"TIMEZONE" envDefault:"Asia/Shanghai" yaml:"timezone"`

	// TablePrefix 会加到应用创建和查询的每张表名前，使多个部署可以共用一个 schema。
	TablePrefix string `env:"TABLE_PREFIX" envDefault:"base_" yaml:"table_prefix"`

	MaxOpenConns       int           `env:"MAX_OPEN_CONNS" envDefault:"25" validate:"min=1" yaml:"max_open_conns"`
	MaxIdleConns       int           `env:"MAX_IDLE_CONNS" envDefault:"10" validate:"min=0" yaml:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `env:"CONN_MAX_LIFETIME" envDefault:"30m" yaml:"conn_max_lifetime"`
	ConnMaxIdleTime    time.Duration `env:"CONN_MAX_IDLE_TIME" envDefault:"5m" yaml:"conn_max_idle_time"`
	SlowQueryThreshold time.Duration `env:"SLOW_QUERY_THRESHOLD" envDefault:"200ms" yaml:"slow_query_threshold"`
	LogLevel           string        `env:"LOG_LEVEL" envDefault:"warn" validate:"required,oneof=silent error warn info" yaml:"log_level"`

	// Migrate 控制启动时是否执行内嵌的 SQL 迁移。
	Migrate bool `env:"MIGRATE" envDefault:"true" yaml:"migrate"`
}

// validate 校验表前缀、连接时区与连接池容量之间的约束。
func (d Database) validate() problems {
	var p problems
	if !tablePrefixPattern.MatchString(d.TablePrefix) {
		p.addf("DB_TABLE_PREFIX %q must be lowercase letters, digits and underscores, starting with a letter (or be empty)", d.TablePrefix)
	}
	if _, err := time.LoadLocation(d.TimeZone); err != nil {
		p.addf("DB_TIMEZONE %q is not a known IANA time zone", d.TimeZone)
	}
	if d.MaxIdleConns > d.MaxOpenConns {
		p.add("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	return p
}

// DSN 生成 libpq / pgx 兼容的连接串。查询串是手工拼的：url.Values 会转义
// Asia/Shanghai 这类时区中的斜杠，PostgreSQL 随后会把它当成未知时区拒绝。
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
