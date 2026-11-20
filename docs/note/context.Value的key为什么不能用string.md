# 笔记：`context.Value` 的 key 为什么不能用 string

- 日期：2026-11-19
- 状态：已定，已落地（`backend/pkg/logger` 用未导出的 `contextKey struct{}`）
- 结论：key 的相等是「动态类型 + 值」的相等；string 没有类型边界，所有包共用同一套字符串键。用未导出的命名类型，让类型系统替你把门。

---

## 1. 起因

读 `backend/pkg/logger/logger.go` 时看到：

```go
type contextKey struct{}                                      // logger.go:17

func IntoContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)          // logger.go:121-123
}

func fromContext(ctx context.Context) (*slog.Logger, bool) {
	log, ok := ctx.Value(contextKey{}).(*slog.Logger)         // logger.go:139
	return log, ok && log != nil
}
```

第一反应是：为什么不能图省事写成 `context.WithValue(ctx, "logger", log)`？反正这个键只有我自己在用。

结论是：**这个「只有我自己在用」是错觉**。`ctx` 会往下传，会穿过第三方库，会进测试替身；键一旦是 string，所有权就消失了。

---

## 2. 第一层：key 的比较规则

`context.WithValue` 的签名是 `WithValue(parent Context, key, val any)`，内部把 key 存进 `valueCtx`，查找时是：

```go
func (c *valueCtx) Value(key any) any {
	if c.key == key {
		return c.val
	}
	return c.Context.Value(key)
}
```

`c.key == key` 比较的是两个 `any`。接口比较的规则是：**动态类型相同，且值相等**。

- 用 `"logger"` 作 key，动态类型是 `string`，值就是字面量内容；
- `string` 是内建类型，全进程只有一个 `string` 命名空间。

于是「我的 key」和「别人的 key」没有任何机制区分。碰撞不是理论风险，下面三种都会发生：

1. **覆盖**：两个中间件都用 `"logger"`，后执行的 `WithValue` 把前一个换掉，整条链路悄悄改写日志去向；
2. **读取**：任何拿到 `ctx` 的代码，只凭字面量就能 `ctx.Value("logger")` 并断言类型，拿到本不该暴露的 `*slog.Logger`；
3. **伪造**：日志是「全局能力」。如果攻击者能影响某段代码写 context（例如一个可插拔插件），就能把请求级 logger 换成自己的，之后这台请求上的日志要么消失，要么把用户输入伪装成正常日志输出（log injection）。

这跟 `c.Set("request_id", ...)` 那种仅在单次请求内、由框架自己管的键不是一回事——`context.Value` 的键会在包与包之间流动。

---

## 3. 第二层：命名类型自带「所有权」

Go 里类型的身份不是「长得像」，而是**包路径 + 类型名**。`contextKey` 声明在 `backend/pkg/logger` 包内且未导出，于是：

- 包外代码**无法写出** `logger.contextKey` 这个类型（编译器直接拒绝：cannot refer to unexported name）；
- 不能声明该类型的变量 → 构造不出 key → **写不进**这个键；
- 同理也**读不走**，除非用 `unsafe` 或反射硬拗。

这就是为什么用 `struct{}`：不是因为它特殊，而是因为它是一个**包私有的、可比较的、不可变的零大小值**。换成 `type ctxKey int` + `const loggerKey ctxKey = 0` 同样安全，真正起作用的是「未导出命名类型」，不是底层类型。

一个容易忽略的细节：**两个包各自定义 `type contextKey struct{}`，它们也是不同的类型**。类型身份带包路径，比较是不相等的：

```go
type otherKey struct{}
any(contextKey{}) == any(otherKey{}) // false
```

所以别的包就算「猜到」形状、也写个同名类型，照样打不开你的门。demo 的第 3 行验证的正是这一点。这招不是 context 专属，第 9 节列出同一机制在其他地方的用法。

---

## 4. 官方文档与工具链

`context` 包 `WithValue` 的文档（Go 1.25.14 源码原文）：

> The provided key must be comparable and should not be of type string or any other built-in type to avoid collisions between packages using context. Users of WithValue should define their own types for keys. To avoid allocating when assigning to an interface{}, context keys often have concrete type struct{}. Alternatively, exported context key variables' static type should be a pointer or interface.

三句话对应三件事：**别用内建类型**（碰撞）、**自定义类型**（所有权）、**常用 `struct{}`**（零大小，装进 interface 不额外分配）。

工具链上补一个容易记错的点：

- `staticcheck` 的 **SA1029** 专门报 `context.WithValue(ctx, "k", v)` 这种内建类型 key；
- `go vet` 的默认分析器套件**不检查** context key（它有 `slog` 分析器，但管的是日志调用参数，不是 context key）。所以别指望 vet 兜底，review 或 staticcheck 才能拦住。

顺便修正我上一轮口头说的「go vet 也有对应检查」——不准确，vet 不查这个。

---

## 5. 导出 key 的诱惑，以及为什么不行

「那我导出 `const LoggerKey = "logger"`，至少有个名字」——这比裸字面量更糟：

- 它成了**公共 API**：任何人 import 之后都能覆盖、伪造；
- 字面量一旦发布就改不动（改了就是破坏性变更），而键的内容根本不该是对外契约；
- 名字给了「这是正式接口」的错觉，掩盖了「谁都能写」的事实。

另一种常见变体是导出**指针变量**：`var Key = &ctxKey{}`。指针可比较、身份唯一，也能挡住碰撞；但导出的变量可以被重新赋值，而且让调用方拿到了 key 本身。既然封装函数（`IntoContext` / `From`）已经能表达全部意图，就没必要把 key 暴露出去。

至于「把 key 类型定义成 `type ctxKey string`、不导出」——运行时同样安全（不同命名类型不相等），只是这种写法离 string 太近、可读性上容易让人误以为可以互转，`struct{}` 的意图更明确。

---

## 6. 本仓库的对照组：gin 的字符串键

`backend/internal/middleware/context.go:12` 里：

```go
ContextKeyRequestID = "request_id"
```

`c.Set(ContextKeyRequestID, id)` / `c.GetString(...)` 用的就是 string。为什么不矛盾？因为风险模型不同：

- gin 的 `Keys` 是**单次 HTTP 请求生命周期内**的 map，作用域被框架锁死，不跨包、不跨进程；
- 键由同一个 `middleware` 包定义，使用者是同一仓库的 handler，没有第三方往 `c.Keys` 里塞东西的常规路径；
- 它不承载「可替换的全局能力」，只是一个字符串 id。

如果哪天要在 `context.Context` 里传 request_id（而不是 gin 的 `c`），也应该用和 logger 相同的包私有类型，而不是把 `"request_id"` 写进 `context.WithValue`。

---

## 7. string key 什么时候可以接受

诚实的边界：

- 单个 `main` 包、无第三方依赖、所有代码在同一个仓库同一团队 → 风险确实低，很多脚本就这么写；
- 但**库代码和共享基础设施永远不要**：你的键会进入别人的 context，别人的键也会进入你的。`backend/pkg/logger` 被所有 `internal` 包使用，正好是最该避免 string key 的位置。

判断标准很简单：**这个键会不会离开你这个包的控制范围？** 会，就必须有类型边界。

---

## 8. 最小 demo

单文件，只依赖标准库。复制到任意目录存成 `main.go`，`go run main.go` 即可。

```go
package main

import (
	"context"
	"fmt"
)

// ownKey 模拟 backend/pkg/logger 里的 contextKey：未导出命名类型。
type ownKey struct{}

// thirdParty 模拟一个不知道 ownKey 存在的库：它只会写自己认识的 string 键。
func thirdParty(ctx context.Context) context.Context {
	return context.WithValue(ctx, "logger", "第三方库的 logger")
}

func main() {
	base := context.Background()

	// 1. string 作 key：任何包都能写出同一个键，后写的覆盖先写的。
	ctx := context.WithValue(base, "logger", "我们的 logger")
	ctx = thirdParty(ctx)
	fmt.Println("string key 被覆盖:", ctx.Value("logger"))

	// 2. 未导出命名类型作 key：第三方库造不出同类型的值，写不进也读不走。
	ctx = context.WithValue(base, ownKey{}, "我们的 logger")
	ctx = thirdParty(ctx)
	fmt.Printf("命名类型 key: %v | 第三方 string key: %v\n",
		ctx.Value(ownKey{}), ctx.Value("logger"))

	// 3. 类型身份：两个包各自定义的 struct{} 类型互不相等。
	type otherKey struct{}
	fmt.Println("ownKey{} == otherKey{} ?", any(ownKey{}) == any(otherKey{}))
	fmt.Println("ctx.Value(otherKey{}):", ctx.Value(otherKey{}))
}
```

实测输出（Go 1.25.14，Windows）：

```text
string key 被覆盖: 第三方库的 logger
命名类型 key: 我们的 logger | 第三方 string key: 第三方库的 logger
ownKey{} == otherKey{} ? false
ctx.Value(otherKey{}): <nil>
```

四行对应四件事：

- 第一行：第三方库用同一个 string 键，静默覆盖了我们的 logger；
- 第二行：命名类型 key 不受影响，第三方写的 string 键也照常存在（两套键互不干扰）；
- 第三行：两个不同的命名 `struct{}` 类型不相等——「形状一样」不等于「类型一样」；
- 第四行：拿另一个命名类型去查，查不到，返回 `nil`。

demo 里读不到「别的包的私有 key」这件事没法在单文件里完整演示（同包内当然能访问自己的类型），但第 3 行已经说明了关键机制：**key 的匹配靠类型身份，而类型身份带包路径**。

---

## 9. 同一招的其他应用：用类型身份画边界

context key 只是「类型身份当命名空间或所有权令牌」的一个例子。同一个机制在 Go 里至少有四类用法，理解它们之后，第 3 节就不再是孤立的技巧。

### 9.1 防碰撞：给键划命名空间

- **map 的键**：两个包往同一个 `map[string]any` 里放东西，各用 `type authKey string` / `type traceKey string`，值都叫 `"id"` 也不会互相覆盖；和 context 完全同构。
- **注册表**：`image.RegisterFormat(name, ...)`、`database/sql` 的 driver 名、`flag` 名都是开放字符串命名空间，第三方重名就静默覆盖。要避免，要么用 `(type, string)` 复合键，要么让注册方交出包私有类型的令牌。
- 回到本仓库：`RequestIDHeader = "X-Request-ID"` 是字符串且没问题——它是对外协议，本来就要和网关共享命名空间；ctx 里的键是进程内实现细节，才需要隔离。判据：**这个键该不该被别人知道**。

### 9.2 密封接口：只允许本包实现

给接口加一个未导出方法，外部包就实现不了：

```go
type Option interface {
	apply(*config) // 未导出：外部无法实现
}

type timeout time.Duration

func (t timeout) apply(c *config) { c.timeout = time.Duration(t) }
```

`testing.TB` 带未导出的 `private()`（伪造不出一个 `TB`）；gRPC 的 `DialOption` / `CallOption`、`cmp.Option` 也是未导出方法密封。和 context key 互为镜像：一个防「别人读写我的值」，一个防「别人实现我的类型」。

### 9.3 语义类型：让编译器阻止混用

```go
type UserID int64
type OrderID int64
```

底层相同、类型不同，`GetUser(orderID)` 直接编译失败，而不是线上把订单号当用户 id。`time.Duration` 与 `int64` 不能混、`rune` 与 `int` 不能混、`Meter` 与 `Foot` 不能混，都是同一招。价值随项目规模上升：小脚本里是噪音，公共 API 里是护栏。

### 9.4 不透明句柄与「构造即合法」

- 返回未导出具体类型、导出接口：外部只能调方法，不能拆字段（`sql.Tx`、各种 `Reader` / `Writer` 包装）。
- parse, don't validate：解析函数返回一个只能由本包构造的类型，字段全未导出、构造时校验完毕；下游拿到值就代表合法，不必再传 `bool` / `error`。

### 9.5 是否重要，什么时候值得引入

- 写业务代码：属于中级 Go 的关键习惯，不是天天用，但决定 API 是「能编译就不会错」还是「靠调用方自觉」，投入产出比极高。
- 设计库 / 公共 API：几乎是必修。能否密封、能否防伪造、key 会不会泄漏，直接决定 API 的演化空间。

满足一条就值得引入：

1. 同一底层类型有两个以上并存、混淆会出错的语义（两个 ID、两种单位、两种 key）；
2. 值会跨包边界流动，且不希望外部伪造或覆盖；
3. 需要「合法状态只能由某个函数产生」；
4. 键或名字会成为进程内共享资源。

反面同样要记：别给每个 `string` 都包一层类型——转换噪音、与 JSON / ORM / 驱动签名的摩擦是真实成本；`type A = B` 这种别名**没有任何保护**，别名共享同一个类型身份。

---

## 10. 代价与遗留

代价接近于零：`struct{}` 零大小、不可变；`ctx.Value` 的查找仍是沿链向上、O(深度)，与 key 类型无关。唯一的「成本」是每次读写要写一个类型名，以及调用方不能靠字面量猜键——这正是我们要的。

本仓库的遗留：

- [x] `IntoContext` 已接线：`AccessLog` 在 `backend/internal/middleware/logger.go:21-22` 用 `log.With("request_id", ...)` 生成请求级 logger 并注入 request context；GORM 在 `backend/pkg/logger/gorm.go:60` 用 `fromContext` 取它，因此 SQL 记录会继承 `request_id`。注入方和消费方都只经过 `logger.IntoContext` / `logger.From`，两者都不需要知道 key 长什么样——这正是包私有类型的收益。
- [ ] `From` 选择「查不到就返回 default」而不是 `(log, bool)`，会让「忘了注入」变成静默行为。等出现第二个使用方时再评估是否改成显式 ok 风格。
- [ ] 以后若接入 trace，`trace_id` / `span_id` 走同一套包私有 key 模式，不要用字符串；slog 的内置 handler 不读 context，仍需 `With` 到 logger 上（或用 OTel 的 slog handler）。

---

## 11. 如果只看一行

> key 的相等是「类型 + 值」的相等；string 没有类型身份，所以没有边界。未导出的命名类型就是那道边界。
