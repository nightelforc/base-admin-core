package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
	// 该值不含迁移：迁移走 MigrateDSN()，因为大表的 DDL 合法地超过应用查询的时限。
	StatementTimeout time.Duration `env:"STATEMENT_TIMEOUT" yaml:"statement_timeout"`

	// ConnectTimeout 限制 TCP 建连这一段：网络黑洞时 SYN 重传会把新连接拖到调用方
	// 的 ctx 上限，进而占满连接池槽位。pgx 以整秒接收，0 表示不设置。它管不到 TLS
	// 握手与认证，那两段仍受调用方 ctx 约束。
	ConnectTimeout time.Duration `env:"CONNECT_TIMEOUT" yaml:"connect_timeout"`

	// PingTimeout 限制启动时那次连通性验证。它与 ConnectTimeout 不是一回事：
	// ConnectTimeout 管每个新连接的 TCP 建连，PingTimeout 管"这次验证最多等多久"。
	// 0 表示使用 postgres 包的默认值（10s）。建连可能比它慢，所以校验要求它不短于
	// ConnectTimeout——否则一次健康的建连也会被判失败。
	PingTimeout time.Duration `env:"PING_TIMEOUT" yaml:"ping_timeout"`

	// sessionLabel 不是配置项：它由 Load 从 app.name 派生后写入，没有 yaml/env tag，
	// 外部无法单独设置。日志里的 app 字段与 pg_stat_activity.application_name 因此
	// 必然同名，排查时不需要先对表。
	sessionLabel string
}

// sessionLabelMaxBytes 是 PostgreSQL 标识符上限 NAMEDATALEN-1。app.name 会原样
// 作为会话标签，超过这个长度会被服务端静默截断，所以校验放在 App 上。
const sessionLabelMaxBytes = 63

// migrateSessionSuffix 让迁移会话在 pg_stat_activity 里与应用的连接区分开。
const migrateSessionSuffix = "-migrate"

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
	if d.ConnectTimeout < 0 {
		p.add("DB_CONNECT_TIMEOUT must not be negative")
	}
	if d.ConnectTimeout > 0 && d.ConnectTimeout%time.Second != 0 {
		p.add("DB_CONNECT_TIMEOUT must be a whole number of seconds (pgx reads it as integer seconds)")
	}
	if d.PingTimeout < 0 {
		p.add("DB_PING_TIMEOUT must not be negative")
	}
	// 0 会被 postgres 包折算成默认值，这里比较的是折算后的值，否则「没配 ping」会被
	// 误判成「比 connect 短」。
	if d.ConnectTimeout > 0 && d.PingTimeoutOf() < d.ConnectTimeout {
		p.add("DB_PING_TIMEOUT must not be shorter than DB_CONNECT_TIMEOUT")
	}
	return p
}

// PingTimeoutOf 返回实际生效的 ping 预算：零值折算成 postgres 包的默认值。
// 校验与 Summary 都用它，避免"配 0 显示 off，实际却是 10s"。
func (d Database) PingTimeoutOf() time.Duration {
	if d.PingTimeout > 0 {
		return d.PingTimeout
	}
	return postgres.DefaultPingTimeout
}

// MigrateDSN 是迁移 Job 的连接串：不含语句超时（大表的 DDL 可能合法地跑过应用
// 查询的时限），会话标签追加 -migrate，便于在 pg_locks / pg_stat_activity 里认出
// 正在持有 advisory lock 的是迁移而不是服务。
//
// 标签来自 app.name（Load 时写入 sessionLabel），不单独配置：同一个进程在日志和
// 数据库里应该用同一个名字，否则出了事故还得先对表。
func (d Database) MigrateDSN() string { return d.dsn(0, migrateSessionSuffix) }

// AppDSN 是应用连接池使用的连接串：带 statement_timeout 与不带后缀的会话标签。
// 之所以把超时放进连接串而不是逐条执行 SET，是因为它按会话生效，连接池复用连接时
// 不必再补一次，也不会漏掉任何一个新建的连接。
func (d Database) AppDSN() string { return d.dsn(d.StatementTimeout, "") }

func (d Database) dsn(statementTimeout time.Duration, suffix string) string {
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
	if name := d.labelWith(suffix); name != "" {
		query += "&application_name=" + escapeQueryValue(name)
	}
	if d.ConnectTimeout > 0 {
		// pgx 只接受整数秒（pgconn 的 parseConnectTimeoutSetting 用 ParseInt 后乘
		// time.Second），写成 "5s" 会在建连时报解析错误，所以这里先折算成秒。
		query += "&connect_timeout=" + strconv.FormatInt(int64(d.ConnectTimeout/time.Second), 10)
	}
	u.RawQuery = query
	return u.String()
}

// labelWith 返回会话标签：从 app.name 派生来的应用名 + 角色后缀（应用侧为空，
// 迁移为 -migrate）。名称为空则不发送该参数（PostgreSQL 会显示为空串）。
func (d Database) labelWith(suffix string) string {
	name := strings.TrimSpace(d.sessionLabel)
	if name == "" {
		return ""
	}
	return name + suffix
}

// escapeQueryValue 对参数值做 percent 编码。不能直接用 url.QueryEscape：它把空格
// 编成 "+"，而 pgx 的 URI 解码不会把 "+" 还原成空格。
func escapeQueryValue(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
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
		PingTimeout:        d.PingTimeout,
	}
}
