package config

import (
	"strings"
	"time"

	"github.com/nightelforc/base-admin-core/backend/pkg/redis"
)

// Redis 保存 Redis 连接设置。
//
// 部署形态由 Mode 选择：standalone（默认）直连 Addrs 里那唯一的数据节点；
// sentinel 把 Addrs 当作哨兵节点列表，再经 MasterName 查出主节点，主从切换由
// 客户端内部完成，调用方无需感知。
// 集群模式未实现：它需要 hash tag 与跨槽事务层面的改造，属于独立的一步。
type Redis struct {
	Mode string `env:"MODE" validate:"required,oneof=standalone sentinel" yaml:"mode"`

	// Addrs 是连接目标地址列表：单机模式下恰好一个（数据节点本身），哨兵模式下
	// 是哨兵节点列表。默认值只适合单机，切到哨兵时要整体替换成哨兵地址。
	Addrs []string `env:"ADDRS" envSeparator:"," yaml:"addrs"`

	// 以下三项只在哨兵模式下使用。
	MasterName       string `env:"MASTER_NAME" yaml:"master_name"`
	SentinelUsername string `env:"SENTINEL_USERNAME" yaml:"sentinel_username"`
	SentinelPassword string `env:"SENTINEL_PASSWORD" yaml:"sentinel_password"`

	Username     string        `env:"USERNAME" yaml:"username"`
	Password     string        `env:"PASSWORD" yaml:"password"`
	DB           int           `env:"DB" validate:"min=0" yaml:"db"`
	KeyPrefix    string        `env:"KEY_PREFIX" yaml:"key_prefix"`
	DialTimeout  time.Duration `env:"DIAL_TIMEOUT" yaml:"dial_timeout"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT" yaml:"read_timeout"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT" yaml:"write_timeout"`
	PoolSize     int           `env:"POOL_SIZE" validate:"min=1" yaml:"pool_size"`

	// PoolTimeout 限制"从池里取一条连接"最多等多久。它和上面三个超时不是一类：
	// dial/read/write 管一条连接内部，它管池满时的排队。零值会让 go-redis 推导成
	// ReadTimeout+1s，实际值随读超时漂移，所以这里要求显式给出正值。
	PoolTimeout time.Duration `env:"POOL_TIMEOUT" yaml:"pool_timeout"`

	// MaxRetries 是失败后最多重试几次。0 表示不重试（本项目的语义）；go-redis 的
	// 约定正好相反——0 是"用默认 3 次"、-1 才是关闭，翻译只在 Options() 里做一次。
	// 每多一次重试，一条命令的最坏耗时就多算一遍 (pool_timeout + read_timeout)，
	// 所以下面还有一条与请求预算挂钩的校验。
	MaxRetries int `env:"MAX_RETRIES" validate:"min=0" yaml:"max_retries"`

	// ConnMaxIdleTime 是一条连接被回收前的最大空闲时长。go-redis 的默认值是 30 分钟，
	// 而链路上的负载均衡/NAT 往往更早地把空闲连接丢掉：驱动在取连接时会做一次探测，
	// 能识别对端已发 FIN 的死连接，但识别不了被中间设备静默丢弃的半开连接——那种连接
	// 上的第一条命令要等满 read_timeout 才失败。把空闲阈值压到设备的空闲断开阈值以下，
	// 这类概率就降到接近零。回收是惰性的，只在取连接时顺手判断，不占后台资源。
	ConnMaxIdleTime time.Duration `env:"CONN_MAX_IDLE_TIME" yaml:"conn_max_idle_time"`

	// ConnMaxLifetime 是一条连接的最大存活时间，0 表示永不过期。它限制"活了多久"，
	// 与上面的"闲了多久"是两件事；只在 DNS/VIP 漂移、或想周期性摆脱长连接时才需要。
	// 设成非零值后同批连接会在同一时刻到期，驱动有 ConnMaxLifetimeJitter 用来错开，
	// 本模板暂未暴露（见 docs/backend/config.md §6.6）。
	ConnMaxLifetime time.Duration `env:"CONN_MAX_LIFETIME" yaml:"conn_max_lifetime"`

	// 哨兵模式的只读路由：开启后只读命令会被发往从节点。
	RouteByLatency bool `env:"ROUTE_BY_LATENCY" yaml:"route_by_latency"`
	RouteRandomly  bool `env:"ROUTE_RANDOMLY" yaml:"route_randomly"`

	// 传输层加密。TLSEnabled 为 false 时其余 TLS 字段一律忽略（并在校验里报错，
	// 避免"证书配了却没生效"）；开启后数据节点与哨兵节点共用同一份证书材料。
	TLSEnabled            bool   `env:"TLS_ENABLED" yaml:"tls_enabled"`
	TLSCAFile             string `env:"TLS_CA_FILE" yaml:"tls_ca_file"`
	TLSCertFile           string `env:"TLS_CERT_FILE" yaml:"tls_cert_file"`
	TLSKeyFile            string `env:"TLS_KEY_FILE" yaml:"tls_key_file"`
	TLSServerName         string `env:"TLS_SERVER_NAME" yaml:"tls_server_name"`
	TLSInsecureSkipVerify bool   `env:"TLS_INSECURE_SKIP_VERIFY" yaml:"tls_insecure_skip_verify"`
}

// validate 校验 Key 命名空间、地址列表与部署模式是否匹配，以及连接超时。
func (r Redis) validate() problems {
	var p problems
	if r.KeyPrefix == "" {
		p.add("REDIS_KEY_PREFIX must not be empty")
	}

	// 空字符串地址也要拒绝：go-redis 收到空 Addr 会回落到 127.0.0.1:6379，
	// 等于静默连错地方。
	for _, addr := range r.Addrs {
		if strings.TrimSpace(addr) == "" {
			p.add("REDIS_ADDRS must not contain empty addresses")
			break
		}
	}
	switch r.Mode {
	case redis.ModeStandalone:
		if len(r.Addrs) != 1 {
			p.addf("REDIS_ADDRS must contain exactly one address when REDIS_MODE=standalone, got %d", len(r.Addrs))
		}
	case redis.ModeSentinel:
		if strings.TrimSpace(r.MasterName) == "" {
			p.add("REDIS_MASTER_NAME is required when REDIS_MODE=sentinel")
		}
		if len(r.Addrs) == 0 {
			p.add("REDIS_ADDRS must list at least one sentinel address when REDIS_MODE=sentinel")
		}
	}

	// TLS 的互斥关系在这里拦下：证书配了却没开开关，等于静默走明文，
	// 这类"以为加密了"的错误比连不上更难排查。
	tlsFieldsSet := r.TLSCAFile != "" || r.TLSCertFile != "" || r.TLSKeyFile != "" ||
		r.TLSServerName != "" || r.TLSInsecureSkipVerify
	switch {
	case !r.TLSEnabled && tlsFieldsSet:
		p.add("REDIS_TLS_* options require REDIS_TLS_ENABLED=true")
	case r.TLSEnabled && (r.TLSCertFile == "") != (r.TLSKeyFile == ""):
		p.add("REDIS_TLS_CERT_FILE and REDIS_TLS_KEY_FILE must be set together")
	}

	for _, item := range []struct {
		name  string
		value time.Duration
	}{
		{"REDIS_DIAL_TIMEOUT", r.DialTimeout},
		{"REDIS_READ_TIMEOUT", r.ReadTimeout},
		{"REDIS_WRITE_TIMEOUT", r.WriteTimeout},
		{"REDIS_POOL_TIMEOUT", r.PoolTimeout},
		{"REDIS_CONN_MAX_IDLE_TIME", r.ConnMaxIdleTime},
	} {
		if item.value <= 0 {
			p.addf("%s must be positive", item.name)
		}
	}
	// 空闲阈值不允许关闭（0 在驱动里等于"用默认 30 分钟"，语义含糊），存活时间
	// 允许 0：它表达"连接永不过期"，这正是驱动的默认行为。
	if r.ConnMaxLifetime < 0 {
		p.add("REDIS_CONN_MAX_LIFETIME must not be negative")
	}
	return p
}

// Options 把 Redis 配置转换成客户端的连接参数。
//
// 映射放在这里而不是 main：配置项与环境变量的对应关系只有本包知道，新增字段时
// 只需同步这一个方法，不会出现"加了 env 却忘了传给客户端"的静默失效。Redis 的
// Options 全是纯数据，所以这里是完整映射，不需要像 Database 那样留运行时字段。
func (r Redis) Options() redis.Options {
	// 配置层用 0 表达"不重试"，驱动用 -1 表达关闭；只在这一个地方翻译，其余
	// 代码看到的是同一个数。
	maxRetries := r.MaxRetries
	if maxRetries == 0 {
		maxRetries = -1
	}

	return redis.Options{
		Mode:             r.Mode,
		Addrs:            r.Addrs,
		MasterName:       r.MasterName,
		SentinelUsername: r.SentinelUsername,
		SentinelPassword: r.SentinelPassword,
		Username:         r.Username,
		Password:         r.Password,
		DB:               r.DB,
		DialTimeout:      r.DialTimeout,
		ReadTimeout:      r.ReadTimeout,
		WriteTimeout:     r.WriteTimeout,
		PoolSize:         r.PoolSize,
		PoolTimeout:      r.PoolTimeout,
		MaxRetries:       maxRetries,
		ConnMaxIdleTime:  r.ConnMaxIdleTime,
		ConnMaxLifetime:  r.ConnMaxLifetime,
		RouteByLatency:   r.RouteByLatency,
		RouteRandomly:    r.RouteRandomly,
		TLS: redis.TLSOptions{
			Enabled:            r.TLSEnabled,
			CAFile:             r.TLSCAFile,
			CertFile:           r.TLSCertFile,
			KeyFile:            r.TLSKeyFile,
			ServerName:         r.TLSServerName,
			InsecureSkipVerify: r.TLSInsecureSkipVerify,
		},
	}
}
