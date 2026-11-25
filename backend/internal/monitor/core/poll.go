package core

import (
	"context"
	"time"
)

// Strategy 判定相邻两次快照，返回 false 表示这次不归它管。
//
// 策略是纯函数：同样的 (prev, cur) 永远得出同样的 Result，因此可以脱离数据源单独测试。
// 它只负责「要不要说、说什么、多严重」，数值由 Poll 的 measure 统一产出。
type Strategy[T any] func(prev, cur T) (Result, bool)

// Delta 返回非负增量。计数类指标只增不减，但接口不保证，倒退时按 0 处理，
// 免得一次重置让日志出现负数。
func Delta(cur, prev int64) int64 {
	if cur <= prev {
		return 0
	}
	return cur - prev
}

// DurationDelta 同 Delta，用于累计时长。
func DurationDelta(cur, prev time.Duration) time.Duration {
	if cur <= prev {
		return 0
	}
	return cur - prev
}

// Poll 是「定期取快照、差分、按策略链判定」这一类监控项的通用实现。
//
// 连接池、队列长度、命令耗时直方图都是同一个形状：T 是快照类型，take 把数据源
// 当前状态读成 T，measure 把相邻两次快照摊成数值，策略只关心 prev 与 cur 的差别。
//
// 第一个 tick 只建基线、不判定，但**照样产出指标**：读取方从启动就能画图，
// 增量字段在基线自然是 0。
type Poll[T any] struct {
	name       string
	every      time.Duration
	take       func(context.Context) (T, error)
	measure    func(prev, cur T) []Metric
	strategies []Strategy[T]
	prev       T
	primed     bool
}

// NewPoll 构造一个监控项。ss 为空时永远不输出，调用方要给默认策略；
// measure 为 nil 时不产出指标。
func NewPoll[T any](
	name string,
	every time.Duration,
	take func(context.Context) (T, error),
	measure func(prev, cur T) []Metric,
	ss ...Strategy[T],
) *Poll[T] {
	return &Poll[T]{name: name, every: every, take: take, measure: measure, strategies: ss}
}

// Name 实现 Check。
func (p *Poll[T]) Name() string { return p.name }

// Interval 实现 Check。
func (p *Poll[T]) Interval() time.Duration { return p.every }

// Sample 取一次快照交给策略链。
//
// 取数失败直接给 warn——这类错误不该被策略吞掉；策略都不认领时仍返回指标，
// 只是不带消息，由注册表跳过日志。
func (p *Poll[T]) Sample(ctx context.Context) Result {
	cur, err := p.take(ctx)
	if err != nil {
		return Result{
			Msg:      p.name + " sample failed",
			Severity: SeverityWarn,
			Err:      err,
		}
	}

	prev := p.prev
	if !p.primed {
		p.primed, p.prev = true, cur
		return Result{Severity: SeverityOK, Metrics: p.measureMetrics(cur, cur)}
	}

	p.prev = cur
	res := Result{Severity: SeverityOK, Metrics: p.measureMetrics(prev, cur)}
	for _, s := range p.strategies {
		if claimed, ok := s(prev, cur); ok {
			res.Msg, res.Severity = claimed.Msg, claimed.Severity
			break
		}
	}
	return res
}

func (p *Poll[T]) measureMetrics(prev, cur T) []Metric {
	if p.measure == nil {
		return nil
	}
	return p.measure(prev, cur)
}
