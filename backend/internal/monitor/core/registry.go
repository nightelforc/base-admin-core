package core

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Registry 收集监控项并统一调度。
//
// 每个监控项一个 goroutine、一个 ticker：它们互不阻塞，一个慢采样不会拖慢其他项。
// 单次采样包了 recover，一个写错的策略只会在日志里留一条 error，不会带走进程。
type Registry struct {
	log *slog.Logger

	mu    sync.Mutex
	items []Check
	names map[string]struct{}
}

// New 创建注册表。log 为 nil 时回落到 slog.Default()。
func New(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{log: log, names: make(map[string]struct{})}
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

// Checks 返回已注册监控项的副本，供 /metrics 之类的出口遍历。
func (r *Registry) Checks() []Check {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Check(nil), r.items...)
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

	ticker := time.NewTicker(intervalOf(c))
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
			log.Error("monitor check panicked", "panic", v)
		}
	}()

	if res := c.Sample(ctx); res.Msg != "" {
		log.Log(ctx, res.Level, res.Msg, res.Attrs...)
	}
}

// intervalOf 把非正间隔折算成默认值：注册即开启，关闭靠「不注册」。
func intervalOf(c Check) time.Duration {
	if d := c.Interval(); d > 0 {
		return d
	}
	return DefaultInterval
}
