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
package redis

import (
	"context"
	"fmt"
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

	// RouteByLatency 与 RouteRandomly 只对哨兵模式下的读流量有意义：
	// 开启后 go-redis 会把只读命令发往从节点。
	RouteByLatency bool
	RouteRandomly  bool
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

// newClient 按模式构造客户端，不做任何网络往返，因此可以在测试里直接调用。
//
// 两种模式在这里就分叉，而不是用 go-redis 的 NewUniversalClient 靠地址数量与
// MasterName 猜测：显式分支让配置错误变成一条说明清楚的启动错误，而不是静默选错
// 连接方式。
func newClient(opts Options) (*goredis.Client, error) {
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
			RouteByLatency:   opts.RouteByLatency,
			RouteRandomly:    opts.RouteRandomly,
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
