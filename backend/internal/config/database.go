package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/nightelforc/base-admin-core/backend/pkg/postgres"
)

// tablePrefixPattern 保证表前缀可以安全地拼进 DDL 标识符（迁移）和生成的表名。
var tablePrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$|^$`)

// Database 保存 PostgreSQL 连接设置。
type Database struct {
	Host     string `env:"HOST" validate:"required" yaml:"host"`
	Port     int    `env:"PORT" validate:"required,min=1,max=65535" yaml:"port"`
	User     string `env:"USER" validate:"required" yaml:"user"`
	Password string `env:"PASSWORD" yaml:"password"`
	Name     string `env:"NAME" validate:"required" yaml:"name"`
	SSLMode  string `env:"SSLMODE" validate:"required,oneof=disable allow prefer require verify-ca verify-full" yaml:"sslmode"`
	TimeZone string `env:"TIMEZONE" yaml:"timezone"`

	// TablePrefix 会加到应用创建和查询的每张表名前，使多个部署可以共用一个 schema。
	TablePrefix string `env:"TABLE_PREFIX" yaml:"table_prefix"`

	MaxOpenConns       int           `env:"MAX_OPEN_CONNS" validate:"min=1" yaml:"max_open_conns"`
	MaxIdleConns       int           `env:"MAX_IDLE_CONNS" validate:"min=0" yaml:"max_idle_conns"`
	ConnMaxLifetime    time.Duration `env:"CONN_MAX_LIFETIME" yaml:"conn_max_lifetime"`
	ConnMaxIdleTime    time.Duration `env:"CONN_MAX_IDLE_TIME" yaml:"conn_max_idle_time"`
	SlowQueryThreshold time.Duration `env:"SLOW_QUERY_THRESHOLD" yaml:"slow_query_threshold"`
	LogLevel           string        `env:"LOG_LEVEL" validate:"required,oneof=silent error warn info" yaml:"log_level"`

	// StatementTimeout 让 PostgreSQL 自己给单条语句设上限。请求超时只能让调用方
	// 提前放弃，服务端语句仍会继续跑；这个参数才是数据库侧的止损点。0 表示不设置。
	// 该值不含迁移：迁移走 DSN()，因为大表的 DDL 合法地超过应用查询的时限。
	StatementTimeout time.Duration `env:"STATEMENT_TIMEOUT" yaml:"statement_timeout"`

	// Migrate 控制启动时是否执行内嵌的 SQL 迁移。
	Migrate bool `env:"MIGRATE" yaml:"migrate"`
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
	if d.StatementTimeout < 0 {
		p.add("DB_STATEMENT_TIMEOUT must not be negative")
	}
	return p
}

// DSN 生成 libpq / pgx 兼容的连接串，不含语句超时：迁移沿用它，因为大表的
// DDL 可能合法地跑过应用查询的时限。查询串是手工拼的：url.Values 会转义
// Asia/Shanghai 这类时区中的斜杠，PostgreSQL 随后会把它当成未知时区拒绝。
func (d Database) DSN() string { return d.dsn(0) }

// AppDSN 是应用连接池使用的连接串：在 DSN 之上带 statement_timeout。
// 之所以放进连接串而不是逐条执行 SET，是因为它按会话生效，连接池复用连接时
// 不必再补一次，也不会漏掉任何一个新建的连接。
func (d Database) AppDSN() string { return d.dsn(d.StatementTimeout) }

func (d Database) dsn(statementTimeout time.Duration) string {
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
	if statementTimeout > 0 {
		query += "&statement_timeout=" + strconv.FormatInt(statementTimeout.Milliseconds(), 10)
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
		DSN:                d.AppDSN(),
		TablePrefix:        d.TablePrefix,
		MaxOpenConns:       d.MaxOpenConns,
		MaxIdleConns:       d.MaxIdleConns,
		ConnMaxLifetime:    d.ConnMaxLifetime,
		ConnMaxIdleTime:    d.ConnMaxIdleTime,
		LogLevel:           d.LogLevel,
		SlowQueryThreshold: d.SlowQueryThreshold,
	}
}
