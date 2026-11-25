// Package redis 提供 Redis 客户端的创建与关闭，用于会话、登录限流以及其他
// 短生命周期的服务端状态。
//
// key 的命名空间由调用方（应用级 key 前缀）负责，而不是由客户端负责，因此前缀
// 始终是显式且可被 grep 到的。
//
// 本包只负责构建和关闭客户端，刻意不持有包级变量。唯一的客户端在 main 中创建，
// 再交给需要它的组件（会话存储、登录限流、健康检查），这样构造顺序显式可见，
// 测试也能构建自己的客户端而不必触碰全局状态。
//
// 返回的 *goredis.Client 可被多个 goroutine 并发使用，内部自带连接池，因此
// 进程应当只创建一个并共享，而不是每个请求都新开一个客户端。
//
// 支持两种部署形态：直连单机（ModeStandalone）与通过哨兵发现主节点
// （ModeSentinel）。两者都返回 *goredis.Client，主从切换发生在客户端内部，
// 因此调用方不需要区分自己连的是哪一种。集群模式有意不在此实现：它需要
// hash tag / 跨槽事务层面的改造，属于另一件事。
//
// 传输层加密由 TLSOptions 控制：开启后数据节点与哨兵节点使用同一份证书材料
// （托管 Redis 通常对整个实例统一开启 in-transit encryption）。
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// 支持的连接方式。
const (
	// ModeStandalone 直连单个服务器，Addrs 恰好一个元素。
	ModeStandalone = "standalone"
	// ModeSentinel 通过哨兵集群发现主节点，Addrs 是哨兵节点列表。
	ModeSentinel = "sentinel"
)

// TLSOptions 描述到 Redis 的传输层加密。
//
// 零值表示明文连接。开启后至少要能信任服务端证书：要么通过 CAFile 指定私有
// CA（托管服务或自签证书的常见形态），要么让系统根证书兜底。CertFile/KeyFile
// 只在服务端要求客户端证书（mTLS）时才需要。
//
// 字段刻意保持为字符串与布尔，而不是直接收一个 *tls.Config：证书材料在这里
// 加载，配置层因此仍然只是纯数据，和 Database/Redis 的其他字段一致。
type TLSOptions struct {
	// Enabled 为 false 时其余字段一律忽略，连接走明文。
	Enabled bool

	// CAFile 是 PEM 编码的 CA 证书路径。为空时使用系统根证书。
	CAFile string

	// CertFile 与 KeyFile 是客户端证书与私钥路径，必须成对出现；都为空表示
	// 不做 mTLS。
	CertFile string
	KeyFile  string

	// ServerName 覆盖证书校验使用的名字，为空时取连接地址的主机名。
	ServerName string

	// InsecureSkipVerify 关闭服务端证书校验，仅供本地自签环境使用；
	// 开启后 TLS 只能防窃听，不能防冒充。
	InsecureSkipVerify bool
}

// Options 描述一个 Redis 连接。
//
// 零值不可用：单机模式的 Addrs 必须恰好一个元素，哨兵模式必须设置 MasterName
// 且 Addrs 非空。其余字段都是可选的，为零时沿用 go-redis 自身的默认值，
// 这也是 Options 保持为普通结构体而不在此处做校验的原因。
type Options struct {
	// Mode 选择连接方式，必须是 ModeStandalone 或 ModeSentinel。
	// 空值不会回落到单机：没配就是配错了，让它报错好过静默换个连法。
	Mode string

	// Addrs 是连接目标地址列表，元素形如 "host:port"。
	//
	// 两种模式下含义不同：单机模式是数据节点本身且恰好一个；哨兵模式是哨兵
	// 节点列表（主节点由 MasterName 经哨兵查出）。切换模式时地址要跟着换。
	Addrs []string

	// MasterName 是哨兵用来定位主节点的名字，哨兵模式必填。
	MasterName string
	// SentinelUsername 与 SentinelPassword 是连接哨兵本身所需的凭据，
	// 与数据节点的 Username/Password 相互独立：哨兵常常单独开启认证。
	SentinelUsername string
	SentinelPassword string

	// Username 与 Password 是数据节点的 ACL 凭据。服务器未开启认证时 Password
	// 可以为空，这种情况下两者都留空即可。
	Username string
	Password string
	// DB 选择服务器内的逻辑数据库。该字段为零时 go-redis 使用 0 号库，
	// 因此非默认库必须显式设置。
	DB int
	// DialTimeout 限制建立单条连接的超时时间。
	DialTimeout time.Duration
	// ReadTimeout 与 WriteTimeout 分别限制单条命令读/写阶段的超时时间。
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// PoolSize 限制连接池的最大连接数。为零时 go-redis 依据 GOMAXPROCS 自动决定。
	PoolSize int
	// PoolTimeout 限制"从池里取一条连接"最多等多久。它既不是建连时间
	// （DialTimeout），也不是命令执行时间（Read/WriteTimeout），而是池满时请求
	// 排队的地方。零值交给 go-redis 推导（ReadTimeout + 1s），实际值会随读超时
	// 漂移，所以配置层要求显式给出正值。
	PoolTimeout time.Duration

	// MaxRetries 是失败后最多重试几次，语义与 go-redis 完全一致：0 表示用驱动
	// 默认值（3 次），-1 表示关闭重试（驱动用 -1 表达关闭，0 反而是"用默认"，
	// 这个反直觉的约定不在本包里改写，调用方负责翻译）。
	//
	// 它不改变单次尝试的耗时，但会把 pool_timeout 与 read_timeout 相乘：
	// 最坏耗时是 (MaxRetries+1) × (PoolTimeout+ReadTimeout)，所以配置层用一条
	// 预算校验把它和 HTTP_REQUEST_TIMEOUT 绑在一起。
	MaxRetries int

	// ConnMaxIdleTime 是连接被判定为空闲过久、可以回收之前的时长。零值交给
	// go-redis 推导（30 分钟），而驱动的默认值通常长于链路上负载均衡/NAT 的
	// 空闲断开阈值，所以要压到那个阈值以下只能显式配置。
	//
	// 回收是惰性的：没有后台扫描，只在取连接时顺手判断。被回收的连接下一次
	// 需要重新拨号（TLS 还要握手）。
	ConnMaxIdleTime time.Duration

	// ConnMaxLifetime 是一条连接的最大存活时间，零值表示永不过期。它和
	// ConnMaxIdleTime 是两件事：前者限制"活了多久"，后者限制"闲了多久"。
	// 设置非零值时，同一批连接会在同一时刻到期，需要错开时用驱动提供的
	// ConnMaxLifetimeJitter（本包暂未暴露，见 docs/backend/config.md §6.6）。
	ConnMaxLifetime time.Duration

	// RouteByLatency 与 RouteRandomly 只对哨兵模式下的读流量有意义：
	// 开启后 go-redis 会把只读命令发往从节点。
	RouteByLatency bool
	RouteRandomly  bool

	// TLS 控制传输层加密，零值即明文。
	TLS TLSOptions
}

// Open 连接 Redis，并通过 PING 验证连接是否可用。
//
// 仅有客户端并不能证明服务器可达，因为 go-redis 是在执行第一条命令时才惰性建连；
// PING 把地址错误或凭据错误转化为此处的启动期错误，而不是等到某个请求执行时才
// 暴露出来。失败时会先关闭这个只构建了一半的客户端再返回，调用方无需做清理。
//
// ctx 只约束握手阶段（在调用方 deadline 之上再叠加 10 秒上限），不会限制之后通过
// 该客户端发出的命令。
func Open(ctx context.Context, opts Options) (*goredis.Client, error) {
	client, err := newClient(opts)
	if err != nil {
		return nil, err
	}

	// 超时基于调用方的 context 派生，而不是基于 context.Background()，
	// 这样收到退出信号时仍能中断启动流程。
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := Ping(pingCtx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// config 把 TLSOptions 转换成 *tls.Config，未启用时返回 nil。
//
// 证书材料在建立连接前加载，路径写错会直接变成启动错误，而不是等到第一条命令
// 才失败。MinVersion 固定为 TLS 1.2：托管 Redis 早已不支持更低版本，显式钉住
// 可以避免协商出旧协议。
func (t TLSOptions) config() (*tls.Config, error) {
	if !t.Enabled {
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: t.ServerName,
		// 仅用于本地自签环境，由配置显式开启。
		InsecureSkipVerify: t.InsecureSkipVerify, //nolint:gosec
	}

	if t.CAFile != "" {
		raw, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("redis tls: read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("redis tls: no PEM certificates in %s", t.CAFile)
		}
		cfg.RootCAs = pool
	}

	switch {
	case t.CertFile == "" && t.KeyFile == "":
		// 不做 mTLS，只校验证书链。
	case t.CertFile == "" || t.KeyFile == "":
		return nil, fmt.Errorf("redis tls: cert file and key file must be set together")
	default:
		pair, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("redis tls: load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}

	return cfg, nil
}

// newClient 按模式构造客户端，不做任何网络往返，因此可以在测试里直接调用。
//
// 两种模式在这里就分叉，而不是用 go-redis 的 NewUniversalClient 靠地址数量与
// MasterName 猜测：显式分支让配置错误变成一条说明清楚的启动错误，而不是静默选错
// 连接方式。
func newClient(opts Options) (*goredis.Client, error) {
	tlsCfg, err := opts.TLS.config()
	if err != nil {
		return nil, err
	}

	switch opts.Mode {
	case ModeStandalone:
		// 单机是"就这一台"，多给了地址说明配置想表达别的拓扑，宁可启动失败
		// 也不要悄悄只用第一个。
		if len(opts.Addrs) != 1 {
			return nil, fmt.Errorf("redis standalone mode requires exactly one address, got %d", len(opts.Addrs))
		}
		return goredis.NewClient(&goredis.Options{
			Addr:         opts.Addrs[0],
			Username:     opts.Username,
			Password:     opts.Password,
			DB:           opts.DB,
			DialTimeout:  opts.DialTimeout,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			PoolSize:     opts.PoolSize,
			PoolTimeout:  opts.PoolTimeout,
			MaxRetries:   opts.MaxRetries,

			ConnMaxIdleTime: opts.ConnMaxIdleTime,
			ConnMaxLifetime: opts.ConnMaxLifetime,
			TLSConfig:       tlsCfg,
		}), nil

	case ModeSentinel:
		if opts.MasterName == "" {
			return nil, fmt.Errorf("redis sentinel master name is empty")
		}
		if len(opts.Addrs) == 0 {
			return nil, fmt.Errorf("redis sentinel addresses are empty")
		}
		return goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName:       opts.MasterName,
			SentinelAddrs:    opts.Addrs,
			SentinelUsername: opts.SentinelUsername,
			SentinelPassword: opts.SentinelPassword,
			Username:         opts.Username,
			Password:         opts.Password,
			DB:               opts.DB,
			DialTimeout:      opts.DialTimeout,
			ReadTimeout:      opts.ReadTimeout,
			WriteTimeout:     opts.WriteTimeout,
			PoolSize:         opts.PoolSize,
			PoolTimeout:      opts.PoolTimeout,
			MaxRetries:       opts.MaxRetries,
			ConnMaxIdleTime:  opts.ConnMaxIdleTime,
			ConnMaxLifetime:  opts.ConnMaxLifetime,
			RouteByLatency:   opts.RouteByLatency,
			RouteRandomly:    opts.RouteRandomly,
			// go-redis 的 FailoverOptions 只有一个 TLSConfig，哨兵连接与数据连接
			// 共用它，正好对应"整个实例统一开启传输加密"的部署方式。
			TLSConfig: tlsCfg,
		}), nil

	default:
		return nil, fmt.Errorf("unknown redis mode %q", opts.Mode)
	}
}

// Ping 验证客户端当前仍能与服务器通信。
//
// 超时完全由调用方的 context 决定：Open 在握手时叠加 10 秒上限，就绪探针则复用
// 请求自身的 deadline。client 为 nil 时返回错误而不是 panic，让探针在连接缺失时
// 能正常降级为 unhealthy，而不是把整个进程带崩。
func Ping(ctx context.Context, client *goredis.Client) error {
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

// HealthChecker 把客户端适配成只含 Ping 的最小接口，供就绪探针这类调用方使用。
//
// go-redis 的 Client.Ping 返回 *StringCmd，与"能否连上"这样的小接口签名对不上，
// 这一层适配让调用方只依赖接口，而不必把具体客户端类型写进签名。
type HealthChecker struct {
	client *goredis.Client
}

// NewHealthChecker 返回适配 client 的就绪检查器；client 为 nil 时其 Ping 返回错误。
func NewHealthChecker(client *goredis.Client) HealthChecker {
	return HealthChecker{client: client}
}

// Ping 检查连接是否可用，语义与包级 Ping 相同，超时由调用方的 context 决定。
func (h HealthChecker) Ping(ctx context.Context) error {
	return Ping(ctx, h.client)
}

// Close 释放客户端。
//
// 接受 nil 客户端，并将其视作“没有需要关闭的东西”，因此调用方可以无条件地
// defer Close，包括 Open 失败的分支。关闭同时会停止该客户端的后台连接池。
func Close(client *goredis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}
