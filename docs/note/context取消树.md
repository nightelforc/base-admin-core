# 笔记：context 的取消树是怎么工作的

- 日期：2026-11-18
- 状态：已定（结论都对着 Go 1.25.14 标准库源码核对过）
- 结论：`context` 是一棵**单向**的树——取消从父向子传播，子被取消对父没有任何影响；通知机制只有一种：`close(chan struct{})`。

---

## 1. 起因

追踪 `server.Run` 里的 `<-ctx.Done()` 时发现，它的上游不在我们的代码里，而在 `context` 包内部。既然这套机制撑着整个优雅关闭，索性把它拆开看一遍，顺便把平时的模糊印象逐个校准。

---

## 2. 树，以及单向传播

```
Background
   └── srvCtx (WithCancel, 由信号取消)
         ├── reqCtx1 (WithTimeout, 30s)
         │     └── dbCtx (WithTimeout, 5s)
         └── reqCtx2
```

规则只有一条：**父取消 → 所有子孙被取消；子取消 → 父不受影响。**

这条约束带来了两个直接后果：

- `context.Background()` 没有 `Done()`（返回 `nil`），语义上永不取消，也没有任何东西在等它；
- 你想「让某个操作不再阻塞当前请求，同时不被请求取消影响」，正确做法是 `context.WithoutCancel(parent)`，而不是把 ctx 存起来或传 `nil`。

---

## 3. `WithCancel` 到底建了什么

`cancelCtx` 的实质是「一个可关闭的 channel + 一张子节点表」：

- `done`：延迟创建的 `chan struct{}`；
- `children map[canceler]struct{}`：所有还在等待父取消的子节点；
- `err` / `cause`：取消原因，只写一次。

`WithCancel` 返回的 `cancel` 等价于 `c.cancel(true, context.Canceled, nil)`。所以 **`Err()` 永远是 `context.Canceled`，`Cause()` 也不会带信号信息**——想记录「收到的是 SIGTERM 还是 SIGINT」，得自己再注册一个 `chan os.Signal`。

---

## 4. 父子怎么挂上：`propagateCancel` 的三条分支

新建子 ctx 时，`propagateCancel` 决定「父取消时谁来通知我」：

**分支一：父永不取消**

```go
done := parent.Done()
if done == nil {
	return // parent is never canceled
}
```

`Background` 走这条：不注册、不起协程、不留引用，只在 `c.Context` 字段里记一下父，供 `Value`/`Deadline` 回溯。所以 `WithCancel(context.Background())` 是零成本的。

**分支二：父已经是 `*cancelCtx`（标准链上的所有情况）**

```go
if p, ok := parentCancelCtx(parent); ok {
	p.mu.Lock()
	if err := p.err.Load(); err != nil {
		child.cancel(false, err.(error), p.cause)   // 父已取消，立刻跟进
	} else {
		p.children[child] = struct{}{}              // 登记，等父取消时遍历
	}
	p.mu.Unlock()
	return
}
```

纯 map 登记，**不产生 goroutine**。`WithTimeout`/`WithDeadline`/`WithCancel` 全部走这条，所以一棵深树也不会多出协程。

**分支三：父的 `Done()` 是自定义 channel**

```go
go func() {
	select {
	case <-parent.Done():
		child.cancel(false, parent.Err(), Cause(parent))
	case <-child.Done():
	}
}()
```

这才是「每个子一个监视协程」的唯一场景。自己实现 `Context` 接口时才会踩到；代价是子必须先取消才能释放这个协程。

---

## 5. 取消：`close(done)` + 连坐 + 幂等

`cancel` 的全部工作：

```go
if c.err.Load() != nil {
	return // already canceled
}
c.err.Store(err)
c.cause = cause
if d == nil {
	c.done.Store(closedchan)   // 还没人调过 Done()，直接存包级已关闭 channel
} else {
	close(d)                    // 那一刻，所有等待者一起醒来
}
for child := range c.children {
	child.cancel(false, err, cause)   // 同步递归，不是异步
}
```

三个要点：

1. **同步连坐**。取消是深度优先的同步递归，`cancel` 返回时整棵子树都已取消。所以「父取消后子 `Err()` 立刻可见」不是巧合。
2. **幂等**。`err` 只写一次，重复 `cancel` 直接返回。信号先到、`defer stop()` 后执行，不会二次 `close` 而 panic。
3. **`done` 的两种形态**。已有 channel 就 `close` 它；还没有（没人调过 `Done()`）就把包级 `closedchan` 存进去。`Done()` 之后的读永远返回已关闭 channel，因此「先取消、后取 `Done`」也不会阻塞。

`Done()` 自己是懒创建的：第一次调用 `make(chan struct{})` 存进 atomic，之后复用同一个。

---

## 6. 超时的语义：更早的那个赢

`WithDeadline(parent, d)` 不是「新建一个独立的计时器」，而是：

- 父的 deadline 比 `d` 更早 → 子继承父的 deadline（再早也没意义）；
- 否则起一个定时器，到点 `cancel(DeadlineExceeded)`。

所以超时是**单调收紧**的：请求 30s、DB 5s，DB 的 ctx 一定在 5s 到点，不会等满 30s。这也是本仓库的预算链 `45s（反代）> 30s（HTTP_REQUEST_TIMEOUT）> 5s（DB_STATEMENT_TIMEOUT）` 的依据。

反过来，`http.Server.WriteTimeout` **不会**让 `r.Context()` 变成 `DeadlineExceeded`——它只是给连接的写设置 deadline。这是最常见的误解，`WriteTimeout` 到点时 handler 里的 `ctx.Err()` 仍然是 `nil`。

---

## 7. 本仓库怎么用它

| 位置 | 用法 | 关键取舍 |
| --- | --- | --- |
| `internal/server/deadline.go` | 每个请求 `WithTimeout(r.Context(), cfg.HTTP.RequestTimeout)` | 只注入预算，不缓冲响应、不代写 504；超时由处理器自己渲染 |
| `internal/server/server.go` | `Shutdown` 用 `context.WithTimeout(context.Background(), shutdownTimeout)` | 必须是 `Background` 派生，不能用请求 ctx，否则关到一半被取消 |
| `internal/middleware/operationlog.go` | `context.Background()` + 5s | 审计必须活过请求，不能跟着请求 ctx 一起死 |
| `internal/handler/health.go` | 3s 就绪探测 | 探测本身要快速失败，不能拖住 K8s |
| `pkg/postgres` / `pkg/redis` | 启动 ping 10s | 启动阶段没有请求预算可借 |
| `pkg/migrate` | 连接 30s，关闭/解锁各 10s | 迁移失败必须能回滚并释放 advisory lock |

一个贯穿全局的约定：**只有 `handler` 从 `c.Request.Context()` 取 ctx，其余层由参数传入**。没有任何一层把 ctx 存进结构体——`context` 包文档对此有明确警告。

---

## 8. 常犯的错误

- 把 `context.Context` 存进结构体字段（`http.Request` 是唯一合理的例外），导致生命周期与请求脱钩。
- 传 `nil` 代替 `context.TODO()`，等到某层 `ctx.Done()` 直接 panic。
- 用 `WithValue` 传业务参数。它只适合传请求域元数据（trace id、用户身份这类），且 key 应该是自定义类型而不是字符串。
- 在循环里 `defer cancel()`，导致取消动作堆积到函数返回。
- 派生 ctx 后忘记 `cancel()`，`WithTimeout` 的定时器要等到期才释放。
- 以为「超时了 handler 就会停」。不会——只有代码检查 `ctx.Err()` 或把 ctx 传给下游，才会真正中断。

---

## 9. 自检题

1. `ctx.Done()` 收到的是通知还是数据？关闭后还能读到内容吗？
2. 一个 `cancel()` 之后，`children` 里的子 ctx 是同步还是异步被取消？
3. `context.Background()` 的 `Done()` 返回什么？为什么 `propagateCancel` 会提前 return？
4. `context.Cause(ctx)` 在 `NotifyContext` 的 ctx 上返回什么？为什么拿不到信号名？
5. `WithDeadline` 的父 deadline 更早时，子会自己起定时器吗？

答案都在上文：通知（关闭即通知）/ 同步递归 / `nil`，因为父永不取消 / `context.Canceled`，因为 `cancel` 时传的就是它 / 不会，直接继承父的 deadline。

---

## 10. 如果只看一行

> `context` 的全部机制就是「一棵只向下传播的树 + 一次 `close(channel)`」，其余都是围绕这两件事的便利封装。

---

## 11. 遗留

- [ ] `internal/server` 里 `deadlineHandler` 只看 `ctx.Err()` 打日志，没有把「因超时返回 504」的计数做成指标；接入 metrics 时补。
- [ ] 尚未给 `WithValue` 建立项目级约束（目前代码里没用），将来加 trace id 时应在 `middleware` 统一注入，并定义私有 key 类型。
