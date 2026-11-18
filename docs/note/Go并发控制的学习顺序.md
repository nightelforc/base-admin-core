# 笔记：Go 并发控制的学习顺序

- 日期：2026-11-18
- 状态：参考路线，按需取用
- 结论：`context` 是核心，`http.Server` 与优雅关闭是生产必备，信号的 runtime 内部机制只需读一遍——判断标准是「能否预测行为」，不是「读过多少源码」。

---

## 1. 先分清三层

| 内容 | 定位 | 要不要读源码 |
| --- | --- | --- |
| `context` 取消树 | 核心，每个服务端程序都用 | 要，`cancelCtx` 相关的 300 行读完就再无疑问 |
| `http.Server` + 优雅关闭 | 核心，生产必备 | 不用，读文档 + 写实验即可 |
| `os/signal` 用法 | 必备，但只是工具 | 不用，记四条规则（见下） |
| 信号在 runtime 里的传递（位图 / note / linkname） | 不是核心 | 读一次足够，用来破除「每信号一个协程」的误解 |

判断标准很简单：**能不能在不查文档的情况下预测「某个 ctx 被 cancel 之后，哪些 goroutine 会退出、阻塞中的调用返回什么 error」**。能，就是掌握了；不能，读再多 `sigsend` 也没用。

---

## 2. 六步路线

### 第 0 步：并发原语打底

goroutine、channel 的方向与关闭语义、`select` 的随机选择、`sync.Mutex`/`Once`/`WaitGroup`、`sync/atomic`。重点不是语法，而是「谁负责关闭 channel」「关闭后接收返回什么」。`context` 的通知机制本质就是关闭一个 `chan struct{}`。

**验证**：不用 `context`，只用 channel 关闭，写一个「3 个 worker + 1 个 producer + 中途叫停」的程序。写不出来，说明还需要回头补这一层。

### 第 1 步：`context`（最值得花时间）

顺序：包文档 → `go.dev/blog/context` → 源码。源码只看四段：

- `WithCancel` / `withCancel`：子节点怎么建出来；
- `propagateCancel`：三条分支（父永不取消 / 登记进 `children` / 起监视协程）；
- `cancel`：`close(done)` + 遍历 children 连坐 + 幂等；
- `Done()`：懒创建 + `closedchan` 快路径。

配套学会 `Err()` 与 `Cause()` 的区别、`WithCancelCause`、`AfterFunc`、`WithoutCancel`。

**验证**：纸上画出 `srvCtx → reqCtx → dbCtx` 三层，答出「取消 `srvCtx` 后 `dbCtx.Err()` 是什么、哪个 channel 先被关闭」。

### 第 2 步：错误处理与 `errors.Is/As`

`context` 的返回值几乎都是 error，而 `Canceled` / `DeadlineExceeded` 必须能被识别，否则「超时」和「业务失败」会混成 500。

**验证**：把 `pkg/response` 里 `errors.Is(err, context.DeadlineExceeded)` 改成 `==`，跑 `go test ./pkg/response/` 看它挂掉——体会包装与判等的差别。

### 第 3 步：`net/http` 的请求生命周期

只需要三件事：`http.Handler` 接口（`ServeHTTP` 在哪个 goroutine 里被调用）、`Request.Context()` 从哪来（连接级 ctx 派生 + 请求 deadline）、`http.Server` 那四个 timeout 字段各自管什么。

**验证**：起本项目，用慢速 header 观察 `ReadHeaderTimeout`；把 `HTTP_REQUEST_TIMEOUT` 调成 2s，访问一个 sleep 5s 的临时 handler，看日志里那条 `request exceeded deadline`。

### 第 4 步：优雅关闭

理解三件事：`Shutdown` 等什么（在飞请求，不等 hijacked 连接）、`ListenAndServe` 为什么返回 `ErrServerClosed`、为什么必须「先摘流量 → 排空异步写 → 再关数据库」。

**验证**：删掉 `server.go` 里 `case <-ctx.Done()`，按 Ctrl+C，观察进程不再退出——这一步能让人永久分清「信号」和「退出」。

### 第 5 步：`os/signal`

只记四条：

1. `Notify` 之后该信号的默认行为被抑制；
2. channel 要带缓冲（`make(chan os.Signal, 1)`），否则 `process()` 的 `default` 分支会静默丢弃；
3. `Stop` 才恢复默认行为，`NotifyContext` 的 `stop()` 兼做 cancel 与反注册；
4. Windows 上只有 Ctrl+C 能进来，`Stop-Process` 不经过 Go 代码。

**验证**：同时用 `signal.Notify` 的 channel 和 `NotifyContext`，观察两者同时被唤醒，而 `context.Cause(ctx)` 只能拿到 `context.Canceled`。

### 第 6 步（可选）：runtime 内部

`src/runtime/sigqueue.go`（位图 + 三态状态机 + note）、`src/runtime/signal_unix.go`（`sighandler` / `sigenable` / `ensureSigM`）、`//go:linkname` 的用法。读一遍只为破除两个误解：信号处理器不是 goroutine；不是每个信号一个接收协程。读完就够，不必背。

---

## 3. 自检题

能脱口答出下面五题，这部分就算过关：

1. `ctx.Done()` 收到的是「通知」还是「数据」？关闭后还能不能读到内容？
2. 一个 `cancel()` 之后，子 ctx 是同步还是异步被取消？
3. `context.Background()` 的 `Done()` 返回什么？为什么 `propagateCancel` 会提前 return？
4. `http.Server.WriteTimeout` 到点后，handler 里的 `r.Context()` 会变成 `DeadlineExceeded` 吗？
5. `signal.Notify` 的 channel 不带缓冲会发生什么？为什么会「偶尔丢信号」？

---

## 4. 材料优先级

- 必读：`context` 包文档（含 “Do not store Contexts inside a struct type” 一段）、`go.dev/blog/context`、`http.Server.Shutdown` 的文档（关于 hijacked 连接的说明很关键）。
- 选读：《Concurrency in Go》的 context 与并发模式章节；《Go 语言设计与实现》的 runtime 相关章节（中文，讲信号与调度比较系统）。
- 只在解释怪异现象时翻：`go/src/net/http/server.go`、`go/src/runtime/sigqueue.go`。

---

## 5. 如果只看一行

> 别顺着源码目录读；按「我要能预测行为」的问题驱动去读，每读一段就回本仓库做一次实验验证。
