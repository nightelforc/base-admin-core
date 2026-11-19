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
	} {
		if item.value <= 0 {
			p.addf("%s must be positive", item.name)
		}
	}
	return p
}

// Options 把 Redis 配置转换成客户端的连接参数。
//
// 映射放在这里而不是 main：配置项与环境变量的对应关系只有本包知道，新增字段时
// 只需同步这一个方法，不会出现"加了 env 却忘了传给客户端"的静默失效。Redis 的
// Options 全是纯数据，所以这里是完整映射，不需要像 Database 那样留运行时字段。
func (r Redis) Options() redis.Options {
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
