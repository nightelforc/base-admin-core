package core

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Registry 收集监控项并统一调度。
//
// 每个监控项一个 goroutine、一个 ticker：它们互不阻塞，一个慢采样不会拖慢其他项。
// 单次采样包了 recover，一个写错的策略只会在日志里留一条 error，不会带走进程。
//
// 每次采样（包括不写日志的健康采样）都会记进 latest，日志与读取看到的是同一份结果。
type Registry struct {
	log      *slog.Logger
	now      func() time.Time
	instance Instance

	mu     sync.Mutex
	items  []Check
	names  map[string]struct{}
	latest map[string]CheckState
}

// New 创建注册表。log 为 nil 时回落到 slog.Default()。
func New(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	// 主机名取不到不是错误：实例标识是给人看的线索，缺了也能定位到 PID。
	hostname, _ := os.Hostname()
	return &Registry{
		log:      log,
		now:      time.Now,
		instance: Instance{Hostname: hostname, PID: os.Getpid()},
		names:    make(map[string]struct{}),
		latest:   make(map[string]CheckState),
	}
}

// Add 注册监控项。名字为空或重复直接 panic：接线错误应该在启动时就暴露，
// 而不是等线上出现两条同名的日志再回头查。
func (r *Registry) Add(checks ...Check) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, c := range checks {
		if c == nil {
			panic("monitor: nil check")
		}
		name := c.Name()
		if name == "" {
			panic("monitor: check with empty name")
		}
		if _, dup := r.names[name]; dup {
			panic("monitor: duplicate check name " + name)
		}
		r.names[name] = struct{}{}
		r.items = append(r.items, c)
	}
}

// Checks 返回已注册监控项的副本。快照而不是原地返回，避免调用方改动内部切片。
func (r *Registry) Checks() []Check {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Check(nil), r.items...)
}

// Snapshot 返回所有已注册监控项的最近状态，按名字排序，并带上实例标识。
//
// 从未采样过的监控项也会出现（SeverityUnknown、Stale）：不出现的话，读的人会以为
// 它根本没被注册。返回的 Metrics 是副本，调用方随意改动不影响后续采样。
func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	out := make([]CheckState, 0, len(r.items))
	for _, c := range r.items {
		name := c.Name()
		state, ok := r.latest[name]
		if !ok {
			state = CheckState{Name: name, Severity: SeverityUnknown}
		}
		state.Interval = intervalOf(c)
		state.Stale = state.SampledAt.IsZero() || now.Sub(state.SampledAt) > staleAfterIntervals*state.Interval
		if state.Stale {
			state.Severity = SeverityUnknown
		}
		state.Metrics = append([]Metric(nil), state.Metrics...)
		out = append(out, state)
	}
	slices.SortFunc(out, func(a, b CheckState) int { return strings.Compare(a.Name, b.Name) })
	return Snapshot{Instance: r.instance, Checks: out}
}

// Run 启动所有监控项，阻塞到 ctx 结束且所有监控项退出。
// 先取快照再启动，之后 Add 的项不会被执行（启动后再注册是接线错误）。
func (r *Registry) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range r.Checks() {
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()
			r.run(ctx, c)
		}(c)
	}
	wg.Wait()
}

func (r *Registry) run(ctx context.Context, c Check) {
	log := r.log.With("monitor", c.Name())
	interval := intervalOf(c)

	// 所有检查都是在同一瞬间启动的，不抖动的话它们会永远锁在同一秒醒来：
	// 多路采样同时打同一个依赖、日志成串、各副本一起滚动重启后仍然对齐。
	// 只随机初始相位一次，周期保持精确，采样时间戳依旧可预测。
	timer := time.NewTimer(rand.N(interval))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.fire(ctx, c, log)
		}
	}
}

func (r *Registry) fire(ctx context.Context, c Check, log *slog.Logger) {
	defer func() {
		if v := recover(); v != nil {
			r.record(c, Result{
				Msg:      "check panicked",
				Severity: SeverityUnknown,
				Err:      fmt.Errorf("panic: %v", v),
			}, r.now())
			log.Error("monitor check panicked", "panic", v)
		}
	}()

	res := c.Sample(ctx)
	r.record(c, res, r.now())
	if res.Msg == "" {
		return
	}

	args := make([]any, 0, 2*len(res.Metrics)+2)
	for _, m := range res.Metrics {
		args = append(args, m.Name, metricValue(m))
	}
	if res.Err != nil {
		args = append(args, "error", res.Err)
	}
	log.Log(ctx, levelOf(res.Severity), res.Msg, args...)
}

// record 保存一次采样的结果。要不要写日志由 Msg 决定，要不要留数据不由它决定：
// 健康采样恰恰是最需要被读到的状态。
func (r *Registry) record(c Check, res Result, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.latest[c.Name()] = CheckState{
		Name:      c.Name(),
		Severity:  res.Severity,
		Message:   res.Msg,
		Metrics:   append([]Metric(nil), res.Metrics...),
		SampledAt: at,
		Err:       res.Err,
		Interval:  intervalOf(c),
	}
}

// levelOf 是严重度到日志级别的唯一映射。监控项不再自己决定日志级别，
// 以后接告警也不去解析日志配置。
func levelOf(s Severity) slog.Level {
	switch s {
	case SeverityNotice:
		return slog.LevelInfo
	case SeverityWarn, SeverityUnknown:
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}

// metricValue 把数值还原成有意义的类型：秒还原成时长，日志里才是 4h12m 而不是 15120。
func metricValue(m Metric) any {
	switch m.Unit {
	case UnitSeconds:
		return time.Duration(m.Value * float64(time.Second))
	case UnitCount:
		return int64(m.Value)
	default:
		return m.Value
	}
}

// intervalOf 把非正间隔折算成默认值：注册即开启，关闭靠「不注册」。
func intervalOf(c Check) time.Duration {
	if d := c.Interval(); d > 0 {
		return d
	}
	return DefaultInterval
}
