# 监控模块（monitor）

> 适用范围：`backend/internal/monitor/`。
>
> 代码入口：`backend/internal/monitor/monitor.go`；契约与调度：`backend/internal/monitor/core/`；具体监控项：`backend/internal/monitor/pg/`。
>
> 本文说明监控模块的定位、概念、调度语义、失败处理和扩展方式。日志字段的完整清单以代码里的 `Result` 构造为准。

## 1. 定位

监控模块回答一个问题：**「进程此刻依赖的那些组件，是否健康」**。它在后台按固定节拍采样，只在需要关注时输出一行结构化日志，不产生数据库往返、不阻塞请求路径、也不作为启动的前置条件。

它刻意的边界：

| 做 | 不做 |
| --- | --- |
| 定期采样进程内计数（连接池等） | 不做请求级埋点、不做链路追踪 |
| 用策略链把快照判成「异常/正常」 | 不判定业务指标（订单量、QPS 归属业务侧） |
| 输出结构化日志 | 不提供 HTTP 查询接口、不做前端页面（见 §9 TODO） |
| 生命周期跟随 `ctx`，随时可退出 | 不热加载策略、不持久化历史数据 |

它不放进 `pkg`：这里编码的是「本服务看哪些信号、什么阈值、多久看一次」的策略，不是无业务语义的可复用基础设施。将来真要拆成独立 module，再整体迁移，见 §8.1。

## 2. 概念

五个概念，分布在三个包：

```text
cmd/server/main.go
  └─ monitor.Run(ctx, log, monitor.Deps{DB: db})    入口：选来源、装配
       ├─ monitor.source                          来源表：一路依赖 → 一批 Check
       ├─ core.Registry                           调度：节拍、panic 隔离、输出
       └─ core.Poll[T] + pg.Strategy              通用监控项：取快照 → 差分 → 策略链
```

### 2.1 `core.Result`

一条监控结论，把三件事分开：**判定**（多严重）、**呈现**（要不要说）、**数据**（数值）：

| 字段 | 含义 |
| --- | --- |
| `Msg` | 日志消息。为空表示这次不输出日志——但不代表没有数据 |
| `Severity` | 严重度：`OK` / `Notice` / `Warn` / `Unknown` |
| `Metrics` | 本次采样的数值；成功采样时都有，健康时也有 |
| `Err` | 采样失败的原因；正常为 `nil` |

`Severity` 不是日志级别：日志只是它的一种渲染（`Registry.levelOf` 一处映射），告警路由是另一个消费者。因此 core 不依赖 `slog`，将来接告警也不必解析日志配置。

```go
type Metric struct {
	Name  string  // 短名，如 in_use；完整身份 = 检查名 + 指标名
	Unit  Unit    // UnitCount / UnitSeconds / UnitBytes / UnitRatio
	Value float64
}
```

`Unit` 让下游恢复类型：`UnitSeconds` 渲染回 `time.Duration`，日志里才是 `4h12m` 而不是 `15120`。

### 2.2 `core.Check`

一个监控项，三个方法：

```go
type Check interface {
	Name() string                        // 同时是日志里的 monitor 字段，注册表内唯一
	Interval() time.Duration             // <= 0 时用 DefaultInterval（30s）
	Sample(ctx context.Context) Result   // 单 goroutine 串行调用，实现不必考虑并发
}
```

关闭某个监控项的方式是**不注册它**，不是配一个 `enabled: false`。

### 2.3 `core.Registry`

做四件事：按名字去重注册（`Add` 对 nil、空名、重名直接 panic，接线错误要在启动时暴露）、给每个监控项起一个 goroutine 并按间隔采样（`Run`）、把每次采样记进最近状态（`record`）、把需要记的 `Result` 写进日志（`fire`，带 `recover`）。

记录发生在日志之前，所以「健康」也有数据：日志只输出 `Msg` 非空的采样，状态缓存保留每一次。`Snapshot()` 返回 `core.Snapshot`——实例标识（hostname/pid）加上按名字排序的 `[]core.CheckState`（含从未采样过的项），每次调用给的是副本；超过 3 个周期没有新样本的项标成 `Stale`，严重度置为 `Unknown`，原来的判定留在 `Message` 里。

它不认识任何具体组件，也不决定「开哪些检查」。

### 2.4 `core.Poll[T]`

「取快照 → 差分 → 按策略链判定」这一类监控项的通用实现，泛型参数 `T` 是快照类型：

```go
core.NewPoll("postgres.pool", core.DefaultInterval, take, poolMetrics, strategies...)
```

- `take func(context.Context) (T, error)`：把数据源当前状态读成 `T`。
- `measure func(prev, cur T) []Metric`：把相邻两次快照摊成数值。**每次采样都调用**，包括基线 tick。
- 第一个 tick **只建基线、不判定**，但照样产出指标（`measure(cur, cur)`，增量自然是 0）：读取方从启动就能画图，装配期也不必为了基线多访问一次数据源。
- 取数失败输出 `<name> sample failed`（`Warn`，带 `error`，无指标），不交给策略链判断。
- 策略链按顺序尝试，**第一条认领的生效**；都不认领时返回不带消息、但仍带指标的 `Result`，注册表只跳过日志。

### 2.5 `core.Strategy[T]`

```go
type Strategy[T any] func(prev, cur T) (Result, bool)
```

纯函数：同样的 `(prev, cur)` 永远得到同样的结果，可以脱离数据源单独测试。策略只填 `Msg` 与 `Severity`——**数值统一由 `measure` 产出**，日志和将来的指标端点读同一份数据，不会各算一套。`core.Delta` / `core.DurationDelta` 把累计值折算成非负增量（数据源重置时按 0 处理，避免日志里出现负数）。

## 3. 装配链

`cmd/server` 只有五行，且不出现任何具体检查名：

```go
if cfg.Monitor.Enabled {
	monitor.Run(ctx, log, monitor.Deps{DB: db})
} else {
	log.Info("monitor disabled by config")
}
```

`monitor.Run` 的职责：

1. 遍历 `sources` 表（当前一行：`postgres` → `pg.Checks(d.DB)`），逐路装配；
2. **某一路失败只跳过这一路**，输出 `monitor source unavailable`（`warn`，带 `source` 与 `error`），其他来源照常启动；
3. 没有任何可用检查时直接返回，不起协程：无失败输出 `monitor disabled: no checks registered`，有失败输出 `monitor disabled: no checks available`；
4. 有检查时输出 `monitor started`（带 `checks` 名字列表），创建 `Registry`，然后 `go registry.Run(ctx)` 并**立即返回**。

协程的创建全部在 `monitor` 包内完成：一个调度协程 + 每个监控项一个采样协程。调用方不写 `go`，也不等它退出。

开关是**部署期**的：`monitor.enabled`（`MONITOR_ENABLED`），默认 `true`。没有热加载；关闭时启动日志会留一条记录，避免监控悄悄消失。

## 4. 调度语义

每个监控项独立：

```text
启动 ──[0, interval) 随机相位──▶ T0 ──interval──▶ T0+interval ──▶ ...
```

- **初始相位随机一次**。所有检查是在同一瞬间启动的，不抖动的话它们会一直锁在同一秒醒来：多路采样同时打同一个依赖、日志成串、多副本一起滚动重启后仍然对齐。只随机这一次，周期保持精确，采样时间戳依旧可预测。
- 抖动的局限：它打散的是「检查之间」「副本之间」的碰撞；与外部整点事件（`cron`、抓取）的重合任何固定周期都躲不开，30 秒又恰好整除 60 秒，判定为可接受。
- **ticker 不漂移**：采样慢于间隔时丢样本而不是错位（`time.Ticker` 通道容量为 1，错过即丢）。因此相位承诺在慢采样下依然成立。
- 一个采样卡住只影响它自己（每个检查独立 goroutine），但会静默地少采几次——目前没有单次采样超时，见 §9。
- 每个检查的 `Sample` 在自己的 goroutine 里串行调用，实现不需要考虑并发。
- **每次采样都留状态**：`Msg` 为空的健康采样不写日志，但照样进 `Registry` 的状态缓存——日志是事件，状态才是「现在怎么样」。
- `ctx` 取消后所有采样协程退出，`Run` 返回。

## 5. 判定策略（连接池）

两个来源共用同一套形状：`Checks(依赖) → core.Poll[T] → 策略链 → Result`。区别只在「怎么取快照」和「哪些信号值得报」。

### 5.1 PostgreSQL 连接池

`pg.Checks(db *gorm.DB, ss ...Strategy)` 收 `*gorm.DB` 而不是 `*sql.DB`：`gorm → database/sql` 的转换是 pg 自己的知识，不该出现在 `monitor` 的开关表里（Redis、ES 都没有这一步）。`db` 为 `nil` 时返回空，`db.DB()` 失败时把错误交给 `monitor` 记一条 warning。

默认策略顺序，从重到轻：

| 顺序 | 策略 | 认领条件 | 严重度 | 消息 |
| --- | --- | --- | --- | --- |
| 1 | `Waiting` | 本区间内 `WaitCount` 有增量 | `Warn` | `postgres pool waiting` |
| 2 | `Exhaustion(0.8)` | `InUse / MaxOpen ≥ 0.8` | `Warn` | `postgres pool nearly exhausted` |
| 3 | `Recycling` | 本区间内有连接因空闲/寿命被回收 | `Notice` | `postgres pool recycling connections` |
| 4 | `Healthy` | 总是认领（兜底） | `OK` | `postgres pool healthy` |

每次采样固定产出九个指标，与哪条策略认领无关：`waits`、`waited`、`avg_wait`、`open`、`in_use`、`idle`、`max_open`、`idle_closed`、`lifetime_closed`。`waits` 是区间增量、`open` 一类是当前值；没有新增等待时 `avg_wait` 记 0，不做除法。

几处细节：

- `Exhaustion` 用**除法比较**而不是 `ratio*max_open`：`0.8*15` 会算出 `12.000000000000002`，边界上判不中；`1e-9` 的容差吸收 `2/3` 这类除不尽的比率。
- `max_open = 0`（不限量）时 `Exhaustion` 不生效。
- `wait_count`、`wait_duration` 这类是**累计值**，日志里给的是区间增量；多副本各记各的，PostgreSQL 侧没有对应视图（`pg_stat_activity` 看不到客户端排队）。
- 慢查询日志的 `elapsed` **包含池等待**（GORM 在整条回调链之后才 `Trace`，而取连接发生在链内），所以「等连接」与「执行慢」只能靠这里区分——这是连接池监控的核心价值。

### 5.2 Redis 连接池

`redis.Checks(client *goredis.Client, ss ...Strategy)` 收 `*goredis.Client`：v9 的 `NewFailoverClient` 也返回 `*Client`，单机与哨兵不需要分叉。采样只读客户端内存里的计数（`PoolStats()` 是一次原子读），**不向服务端发命令**。

| 顺序 | 策略 | 认领条件 | 严重度 | 消息 |
| --- | --- | --- | --- | --- |
| 1 | `Timeouts` | 本区间内 `Timeouts` 有增量 | `Warn` | `redis pool timeout` |
| 2 | `Starvation` | 采样这一刻 `PendingRequests > 0` | `Warn` | `redis pool starving` |
| 3 | `Unusable` | 本区间内 `Unusable` 有增量 | `Notice` | `redis pool discarded unusable connections` |
| 4 | `Healthy` | 总是认领（兜底） | `OK` | `redis pool healthy` |

每次采样固定产出七个指标：`timeouts`、`waits`、`waited`（秒）、`unusable` 是区间增量；`total`、`idle`、`pending` 是当前值。`hits`/`misses` 不产出——它们只描述快路径占比，没有可执行结论。

几处细节：

- `Starvation` 看的是**瞬时值**：30 秒采样会漏掉短促排队，它回答「现在还卡着吗」；「卡过没有」看 `Timeouts` 的增量。两者互补。
- `Timeouts` 直接对应 `redis.pool_timeout` 那个预算：出现增量说明排队已经越过了我们配的值。
- `Unusable` 是**连接被对端丢掉**的证据（取出的连接已经不可用），严重度给 `Notice` 而不是 `Warn`：一条空闲连接坏掉是可自行恢复的现象，除非持续发生，否则不该报成故障。
- 与 PostgreSQL 同源：`app.name` 同时写进 `CLIENT LIST` 的 `name` 列和 `pg_stat_activity.application_name`，库侧两边看到的是同一个标识。

## 6. 日志契约

| 事件 | 级别 | 关键字段 |
| --- | --- | --- |
| `monitor started` | info | `checks`（名字列表） |
| `monitor disabled: no checks registered` | info | — |
| `monitor disabled: no checks available` | info | — |
| `monitor source unavailable` | warn | `source`、`error` |
| `monitor disabled by config` | info | — |
| `monitor check panicked` | error | `panic` |
| `<name> sample failed` | warn | `error` |
| `postgres pool healthy` | debug | `open`、`in_use`、`idle`、`max_open` |
| `postgres pool waiting` | warn | `waits`、`waited`、`avg_wait` + 池字段 |
| `postgres pool nearly exhausted` | warn | 池字段 |
| `postgres pool recycling connections` | info | `idle_closed`、`lifetime_closed` + 池字段 |
| `redis pool healthy` | debug | `total`、`idle`、`pending`、`timeouts`、`waits`、`waited`、`unusable` |
| `redis pool timeout` | warn | 同上 |
| `redis pool starving` | warn | 同上 |
| `redis pool discarded unusable connections` | info | 同上 |

每条采样日志都带 `monitor=<name>`（如 `monitor=postgres.pool`），来自 `Registry` 的 scoped logger。默认日志级别是 `info`，健康采样走 `debug` 因此不可见：正常运行**不产生任何监控日志**——这是有意的，健康不需要占一行。

## 7. 失败处理

三层，互不干扰：

1. **来源不可用**（如 `db.DB()` 出错）：跳过该来源，`warn` 一条，其他来源继续；全部失败时监控退化为「无检查」，进程照常运行。
2. **单次采样失败**（`Poll.take` 返回 error）：`warn` 一条 `<name> sample failed`，下一个 tick 继续尝试。
3. **采样 panic**：`fire` 的 `recover` 捕获，`error` 一条 `monitor check panicked`，进程与其他监控项不受影响。

监控永远不是启动的前置条件：任何一层失败都不会让进程退出。

## 8. 扩展

### 8.1 加一个监控来源（照 `internal/monitor/redis` 抄）

现成的两个来源（`pg`、`redis`）就是模板，步骤固定四步：

1. 新建 `internal/monitor/<组件>/`，用 `core.NewPoll[T]` 组装监控项：`take` 取快照、`measure` 把快照变成指标、策略从重到轻；
2. 在 `monitor.go` 的 `sources` 表加一行 `{name: "<组件>", build: func(d Deps) ([]core.Check, error) { return <pkg>.Checks(d.X), nil }}`（依赖为 `nil` 时 `Checks` 返回空，不要返回错误）；
3. `Deps` 增加字段，`cmd/server` 的 `monitor.Deps{...}` 多传一个依赖，**其余不动**；
4. 补测试：策略纯函数单测 + 指标形状 + 「一路失败不影响另一路」。

依赖转换（如 `*gorm.DB → *sql.DB`）留在来源包里，`sources` 表只管挑依赖。

`cmd` 始终只知道 `monitor.Run`，检查名、阈值、节拍都留在 `monitor` 内。

### 8.2 加一个判定规则

写一个 `func(prev, cur T) (core.Result, bool)`，插到 `DefaultStrategies()` 里合适的位置（从重到轻），补一条纯函数测试。不要修改 `Poll` 或 `Registry`。

## 9. 已知缺口（TODO）

监控目前只有**日志**这一条出口。日志是事件流，回答不了「此刻连接池是什么样」；要做「后台可看的监控页面」，本质是一个前后端模块，不是一次小改动，因此暂缓，留作 TODO：

- **读出口**：状态层已经有了——`Registry.Snapshot()` 返回 `core.Snapshot`（实例标识 + 每个检查的 `CheckState`：严重度、消息、指标、采样时间、是否陈旧），缺的是把它暴露出去的那一层。
  - 注意读操作**不能直接调 `Sample`**：`Poll` 的判定基于相邻快照的增量，额外采样会消耗掉本区间的增量，页面刷新会改变日志行为。
  - 出口形态待定：`/debug/monitor`（仅内网/管理员）还是管理后台页面（要走权限体系 `system:monitor:list`）。
- **指标导出**：把同样的 `Result` 投影成 Prometheus 指标（`counter`/`gauge`），与日志共用采样，不重复采集。
- **单次采样超时**：`Sample` 卡住时 ticker 会丢样本且无告警。目前两个来源都只读内存（`sql.DB.Stats()`、`PoolStats()`），不需要超时；将来接走网络的来源（ES、Redis `INFO`）时要补。
- **按来源配置节拍**：现在是全局 `DefaultInterval`。等到有慢源（网络往返）再考虑，避免过早引入配置面。
- **前端页面**：图表、时间范围、多副本聚合——需要先有指标存储，属于独立模块。

## 10. 文件与测试

| 路径 | 职责 |
| --- | --- |
| `internal/monitor/monitor.go` | 入口：`Deps`、`sources` 表、`Run`、`checks` |
| `internal/monitor/core/monitor.go` | `DefaultInterval`、`Severity`、`Unit`、`Metric`、`Result`、`Check` |
| `internal/monitor/core/registry.go` | `Registry`：注册、调度、状态缓存、panic 隔离、输出 |
| `internal/monitor/core/poll.go` | `Poll[T]`、`Strategy[T]`、`Delta`、`DurationDelta` |
| `internal/monitor/core/state.go` | `CheckState`、`Instance`、`Snapshot`：最近一次采样与陈旧判定 |
| `internal/monitor/pg/pg.go` | PostgreSQL 连接池监控项与四条默认策略 |
| `internal/monitor/redis/redis.go` | Redis 连接池监控项与四条默认策略 |
| `internal/config/monitor.go` | `monitor.enabled` 开关 |

测试共 49 个：`core` 22 个（注册表 9 + `Poll` 5 + 状态 8）、`pg` 12 个（策略纯函数、边界、默认顺序、指标形状、真实 `Poll` 的增量判定、句柄不可用）、`redis` 9 个（策略、指标形状、基线为 0、真实客户端的首个采样）、`monitor` 6 个（装配、空依赖、两个来源、一路失败不影响其他路、无事可做时不起协程）。

运行方式：`go test ./internal/monitor/...`。
