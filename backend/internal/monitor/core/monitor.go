// Package core 是监控框架的契约与调度实现：Result、CheckState、Check、Registry。
//
// 它不认识任何具体组件，也不负责「开哪些检查」——那是上层 package monitor 的
// 策略。之所以单独成包：监控项的实现（monitor/pg 等）需要引用这里的类型，
// 而入口 package monitor 需要引用那些实现；把契约放在叶子上，两边都只向下依赖，
// 不会形成 monitor ↔ monitor/pg 的循环。
package core

import (
	"context"
	"time"
)

// DefaultInterval 是监控项没有指定间隔时的默认节拍。
const DefaultInterval = 30 * time.Second

// Severity 是一次观测的严重程度。
//
// 它回答的是「这个发现有多要紧」，不是「这行日志用什么颜色」：日志级别由 Registry
// 映射一次，告警路由是另一个消费者。把严重度直接写成 slog.Level 会让契约依赖日志库，
// 也会让将来接告警的人去读日志配置来判断要不要响铃。
type Severity int

const (
	// SeverityOK 表示这次采样正常，没有需要人看的东西。
	SeverityOK Severity = iota
	// SeverityNotice 值得记一笔，但不是问题（如连接被回收）。
	SeverityNotice
	// SeverityWarn 需要人关注。
	SeverityWarn
	// SeverityUnknown 表示采不到数，状态未知。
	SeverityUnknown
)

// Unit 是 Metric.Value 的单位。没有单位，下游只能猜 4 是 4 秒还是 4 个字节。
type Unit int

const (
	// UnitCount 是计数，无单位。
	UnitCount Unit = iota
	// UnitSeconds 是时长，以秒为单位。
	UnitSeconds
	// UnitBytes 是字节数。
	UnitBytes
	// UnitRatio 是 0..1 的比率。
	UnitRatio
)

// Metric 是一次采样的一个数值。
//
// Name 是短名（如 in_use）；完整身份 = 检查名 + 指标名，由读取方拼装，
// 免得每个监控项各自维护一套前缀。
type Metric struct {
	Name  string
	Unit  Unit
	Value float64
}

// Result 是一条监控结论。
//
// Msg 为空表示这次不需要记日志——但它不代表「没有数据」：Metrics 在每次成功采样时
// 都会填，健康时也一样（采样失败时为空）。Severity 决定日志级别（由 Registry 映射），
// 将来也决定告警级别。
type Result struct {
	Msg      string
	Severity Severity
	Metrics  []Metric
	// Err 是采样失败的原因；正常采样时为 nil。
	Err error
}

// Check 是一个监控项。
//
// Name 同时作为日志里的 monitor 字段，注册表内必须唯一。
// Interval <= 0 时使用 DefaultInterval；要关闭某个监控项，不要注册它。
// Sample 会被注册表在单个 goroutine 里串行调用，实现不必考虑并发；
// 它必须尊重 ctx 的取消（采样本应只读内存，需要网络或磁盘的实现要自己加超时）。
type Check interface {
	Name() string
	Interval() time.Duration
	Sample(ctx context.Context) Result
}
