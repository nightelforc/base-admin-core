// Package core 是监控框架的契约与调度实现：Result、Check、Registry。
//
// 它不认识任何具体组件，也不负责「开哪些检查」——那是上层 package monitor 的
// 策略。之所以单独成包：监控项的实现（monitor/pg 等）需要引用这里的类型，
// 而入口 package monitor 需要引用那些实现；把契约放在叶子上，两边都只向下依赖，
// 不会形成 monitor ↔ monitor/pg 的循环。
package core

import (
	"context"
	"log/slog"
	"time"
)

// DefaultInterval 是监控项没有指定间隔时的默认节拍。
const DefaultInterval = 30 * time.Second

// Result 是一条监控结论。
//
// Msg 为空表示这次不输出；Level 决定输出级别；Attrs 是 slog 的 key/value 对，
// 原样展开到日志里。级别由监控项决定而不是固定 Info，是为了让「正常」与「异常」
// 在日志里一眼可分：健康时是 debug，出现等待或接近打满时是 warn。
type Result struct {
	Msg   string
	Level slog.Level
	Attrs []any
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
