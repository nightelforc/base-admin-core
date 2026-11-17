# 笔记：Redis 客户端为什么没做成全局变量

- 日期：2026-11-17
- 状态：已定，且已落地（`backend/pkg/redis` 不持有包级变量）
- 结论：`pkg/redis` 只负责创建和关闭客户端，唯一实例在 `main` 里造出来，再作为参数交给会话存储、登录限流和就绪探针。

---

## 1. 起因

那天本来只是要给 `backend/pkg/redis/redis.go` 补注释。写着写着冒出一个念头：能不能在这个包里放一个全局变量当连接用，启动时通过一个 `Init` 把配置注入进去？这样别处用起来就省事了。

当时我甚至想好了形状：

```go
// 草案，最终没有采用
var client *goredis.Client

func Init(cfg Options) error {
	c, err := Open(context.Background(), cfg)
	if err != nil {
		return err
	}
	client = c
	return nil
}

func Client() *goredis.Client { return client }
```

算盘是三条：业务代码不用一路传参数；`log`、`flag` 都是这么干的，`sql.DB` 也常被写成全局；初始化收敛到一个函数，看起来干净。

后来这三条被逐个拆掉了。

---

## 2. 第一轮：我要的是「共享连接」，不是「全局查找」

go-redis 的 `*Client` 本身就是连接池，而且是并发安全的；`NewClient` 不会拨号，第一条命令发出时才惰性建连。

所以「全进程共用一个连接池」这件事，**传一个值就够了**——session、throttle、健康检查各自拿到同一个 `*Client`，底层仍是同一个 pool。全局变量额外给我的只有一件东西：任何代码在任意时刻都能顺着包名摸到它。这不叫省事，这叫隐式。

顺带纠正一个我自己的想当然：全局变量**并不省连接**。两种写法底层是同一个 pool，连接数完全一样。真正要盯的是 `PoolSize × 副本数` 对 Redis 的 `maxclients`（本项目的 `PoolSize` 默认 20），跟变量放哪儿无关。

第一条理由（少打字）就这么没了——省下的是传参，付出的是"依赖看不见"。

---

## 3. 第二轮：我拿标准库给自己找理由，结果没站住

这是我当时最有力的一条心理支撑，所以专门查了一遍：

- **`sql.DB`**：标准库提供的是 `sql.Open` **返回 `*DB`**，由调用方持有、调用方 `Close`。它并没有一个全局的 `sql.DB`。它恰恰是"显式返回、调用方负责生命周期"的范本，被我误当成了反例。
- **`slog.SetDefault`**：这个确实成立，但成立的原因是它有一个合理默认值（stderr 输出），允许先跑起来再替换。
- 那么问题来了：`redis.Default()` 该返回什么？"默认连哪台机器"不存在合理答案。没有合理默认值，就不该有默认实例。

第三条理由也没站住。剩下来的只有"初始化收敛"，而这件事 `main` 本来就在做。

---

## 4. 真正让我改主意的，是三个具体画面

原则说起来很空，下面这三个场景是我确实会碰上的：

**画面一：优雅关闭，谁 `Close`？**
全局变量没有作用域，也就没有 `defer` 的落点。而关闭顺序是有语义的：**先摘流量 → 等在途请求排空 → 再 Close**。这个顺序只能由生命周期最长的 `main` 表达。

**画面二：测试想用替身。**
得先存旧值、测完还原，`defer` 漏一次就污染后面的用例。更难受的是失败跟顺序相关——本地跑过，CI 换个顺序就挂。这种 bug 最难查，因为看起来毫无道理。

**画面三：哪天要第二个客户端。**
业务库和缓存库分到不同 Redis、给某个模块插一个影子缓存、或者多租户各连各的——一个全局槽位只能装一个值，到时候要么升级成 `map`，要么改回注入。两个方向都是返工。

---

## 5. 压死骆驼的：忘了 `Init` 长什么样

这条是我最后下决心的原因，demo 里也留了这段：

忘了调用 `Init`，**编译通过、进程正常启动、第一个请求进来才 nil panic**。

而如果客户端是构造参数，手上没有值就构造不出服务，编译期直接过不去。依赖缺失从"线上事故"变成"编译错误"——这是我在重构时最想要的那个信号。

同理，`Init` 返回的 error 很容易被写成 `_ = redis.Init(cfg)`，地址写错、密码不对都不影响启动，等到有人登录才报 `connection refused`。现在 `main` 里是：

```go
rdb, err := redis.Open(ctx, cfg.Redis.Options())   // backend/cmd/server/main.go:94
if err != nil {
    return fmt.Errorf("connect redis: %w", err)
}
```

失败就启动失败。这条跟"忘了 Init"是同一件事的两面：**把错误推到越早越好**。

---

## 6. 我给自己列的账（放弃全局变量换来的）

按重要性排，前三条是硬的，后面是附带的：

1. **签名不再说谎。** `NewUserService()` 括号里没东西，但运行时它需要 Redis——读代码的人只能靠 grep 才知道。
2. **缺依赖在编译期/构造期暴露**，而不是第一个请求。
3. **测试彼此隔离**，可以放心 `t.Parallel()`。
4. **生命周期有主**：`main` 拥有，`defer` 表达关闭顺序。
5. **多实例天然支持**：两个实例各持后端可以并存。
6. **依赖图可静态分析**：DI 框架、死代码检测、调用图都依赖"构造签名即依赖清单"。
7. **key 前缀这类"属于实例的配置"有地方放了**，不用挤进全局或散落到每个调用点。

---

## 7. 我认真考虑过、最后否决的另外两个方案

### 7.1 全局 `Infra` 容器

把所有依赖打包成一个结构体，全局放一个 `var App *Infra`：

```go
type Infra struct {
	DB    *gorm.DB
	Cache *goredis.Client
	Log   *slog.Logger
}

var App *Infra
```

签名上写着"依赖 `*Infra`"，实际上依赖了一切。这是给全局变量换了个马甲，第 6 节那七条一条不少，还额外丢了"这个函数到底用了什么"这个信息。否决。

### 7.2 上 wire（编译期 DI 代码生成）

用 `//go:build wireinject` 的 `wire.go` + `wire.Build(...)` 生成 `wire_gen.go`，`main` 只剩一行 `app.Init(ctx, cfg, log)`。

想过，但没上。原因：

- 它**不消除** provider / 适配函数（还是得写 `provideRedisOptions(cfg) redis.Options`），只消除调用顺序和参数搬运；
- 生成物要提交，且 CI 得校验它没漂移；build tag 下平时不编译，调试时要读生成码；
- 本项目组件不到 15 个，净行数大概率不降反增。

记下触发条件：**出现第二个 main 入口、`build()` 超过 ~100 行、或 provider 超过 15 个**，到时候再认真评估。`fx` / `dig` 那种反射容器则直接排除——它等于把"全局对象"换成"运行时容器"，依赖缺失从编译错误退化成启动 panic，方向反了。

---

## 8. 判据（以后遇到同类问题直接用这条）

> 依赖出现在构造函数签名里，就是注入；依赖靠包级变量或全局容器查找，就是全局状态。
>
> 判据只认这一条，不认"看起来像不像 DI"。

配套的两个判断：这个值**会不会被替换**？测试**需不需要隔离它**？都踩，就不该是全局。

---

## 9. 代价，我认了

- 构造函数多一个参数，层数深的时候要一路透传。这是真实成本，不粉饰。
- 缓解一：**只注入真正需要的层**。handler 不需要 `*redis.Client`，只需要"能 Ping"：

  ```go
  // backend/internal/handler/health.go:16
  type Pinger interface {
      Ping(ctx context.Context) error
  }
  ```

- 缓解二：**参数多了打包成结构体**。仓库里已有 `AuthDeps`、`Dependencies`。打包解决参数个数，不解决可见性。
- 顺带一句自我提醒：`HealthChecker` 那层适配器不是为了好看。`*Client` 的 `Ping` 返回 `*StringCmd`，签名对不上窄接口，不包一层 handler 就得 import go-redis，把基础设施泄漏进 HTTP 层。

---

## 10. 最后落成的形状

三句话记住职责：**pkg 只造和关，config 只翻译，main 管顺序和生死。**

```go
rdb, err := redis.Open(ctx, cfg.Redis.Options())    // 失败即启动失败
if err != nil {
    return fmt.Errorf("connect redis: %w", err)
}
defer func() {
    if err := redis.Close(rdb); err != nil {
        log.Error("close redis", "error", err)
    }
}()

store   := session.NewStore(rdb, cfg.Redis.KeyPrefix, cfg.Session.TTL)
limiter := throttle.NewLimiter(rdb, cfg.Redis.KeyPrefix, ...)

router.GET("/readyz", handler.Readiness(db, redis.NewHealthChecker(rdb)))
```

注意最后一行：需要 Redis 的组件拿到的是**具体值**（`*Client`），面向 HTTP 的探针拿到的是**行为**（`Ping(ctx) error`）。注入的粒度是按需的，不是"把客户端整个传下去"。

---

## 11. 附：最小 demo

单文件，只依赖标准库。复制到任意目录存成 `main.go`，`go run main.go` 即可（不需要 `go mod init`）。

```go
package main

import (
	"context"
	"fmt"
	"sync"
)

// Cache 代表 *redis.Client：本身并发安全，内部自带连接池。
type Cache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string)
}

type memCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func newMemCache() *memCache { return &memCache{m: map[string]string{}} }

func (c *memCache) Get(_ context.Context, key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *memCache) Set(_ context.Context, key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = value
}

// ---------- 写法 A：包级全局变量 + 手动 Init ----------

var globalCache Cache

func SetGlobalCache(c Cache) { globalCache = c }

type svcA struct{} // 看构造函数：它不需要任何东西

func (svcA) Nick(ctx context.Context, id string) string {
	v, _ := globalCache.Get(ctx, "user:"+id)
	return v
}

// ---------- 写法 B：构造函数注入 ----------

type svcB struct{ cache Cache } // 依赖写在类型里

func newSvcB(c Cache) *svcB {
	if c == nil {
		panic("cache is nil") // 缺依赖在构造期就炸，而不是第一个请求
	}
	return &svcB{cache: c}
}

func (s *svcB) Nick(ctx context.Context, id string) string {
	v, _ := s.cache.Get(ctx, "user:"+id)
	return v
}

func main() {
	ctx := context.Background()
	prod, fake := newMemCache(), newMemCache()
	prod.Set(ctx, "user:1", "alice")
	fake.Set(ctx, "user:1", "FAKE")

	SetGlobalCache(prod)
	a := svcA{}
	fmt.Println("A 全局变量读取    :", a.Nick(ctx, "1"))

	SetGlobalCache(fake) // 测试里注入替身
	fmt.Println("A 替换全局后      :", a.Nick(ctx, "1"), "  <- 同一个 a，后端换了")

	b1, b2 := newSvcB(prod), newSvcB(fake)
	fmt.Println("B 两个实例并存    :", b1.Nick(ctx, "1"), "/", b2.Nick(ctx, "1"))

	globalCache = nil // 等价于启动时忘了 Init
	func() {
		defer func() { fmt.Println("A 忘记 Init      :", recover()) }()
		_ = a.Nick(ctx, "1")
	}()
}
```

实测输出：

```text
A 全局变量读取    : alice
A 替换全局后      : FAKE   <- 同一个 a，后端换了
B 两个实例并存    : alice / FAKE
A 忘记 Init      : runtime error: invalid memory address or nil pointer dereference
```

四行对应四件事：

- 第一、二行：同一个实例因为别处调了 `SetGlobalCache`，静默换了后端——这就是"签名说谎"的实际后果。
- 第三行：注入版两个实例各持后端，可以并存（生产 + 影子）。
- 第四行：忘记初始化，编译通过、启动通过，第一次调用才炸。

**没有放并发对比**：两种写法访问的是同一个线程安全的 client，并发表现不会有差别。真要验证，`go run -race main.go` 跑一遍即可，结论不会变——并发从来不是这场争论的筹码，别拿它当理由。

---

## 12. 如果只看一行

> 全局变量省的是打字时间，注入省的是排查时间。在基础设施句柄上，后者的账更划算。

---

## 13. 以后可能重开这个话题的条件

- 出现第二个 main 入口、或装配代码超过 ~100 行 / 组件超过 15 个 → 重新评估 wire；
- 代码里冒出一个"全局容器"（`App`、`Infra`、`Container`）→ 先拒绝，再讨论，别让它悄悄长起来；
- 真需要"全进程唯一且不可替换"的东西 → 那也应该是一个不可变配置或注册表，不是 Redis 客户端。

## 14. 遗留

- [ ] wire 待评估（等触发条件出现）
- [ ] 如果以后加第二个 Redis（业务/缓存分离），确认是按实例注入而不是给 `pkg/redis` 加第二个包级变量
