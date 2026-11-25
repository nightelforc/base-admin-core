package core

import "time"

// staleAfterIntervals 是「多久没有新样本就认为检查已经死了」的阈值。
//
// 一个周期的偏差是正常的（抖动、慢采样）；连续错过三个就不是慢，而是协程卡住
// 或者根本没起来。判定放在读取时做，不缓存在状态里——缓存自己也会过期。
const staleAfterIntervals = 3

// Instance 标识产出这份状态的进程。
//
// 连接池只在进程内计数，PostgreSQL 侧没有对应视图；多副本部署时没有实例标识，
// 读的人会拿 A 进程的数值解释 B 进程的故障。
type Instance struct {
	Hostname string
	PID      int
}

// Snapshot 是一次读取的完整结果：这份状态是谁给的，以及每个监控项现在怎么样。
type Snapshot struct {
	Instance Instance
	Checks   []CheckState
}

// CheckState 是某个监控项最近一次采样的结果。
//
// 与 Result 的区别：Result 说的是「这一次采样发现了什么」，是事件；CheckState 说的是
// 「它现在是什么样」，是状态。区别的核心是 SampledAt——没有它，读的人无法区分
// 「一直健康」和「检查早就没在跑了」，因为这两种情况都没有日志。
//
// Registry 保存的 Metrics 是副本，读取方拿到的又是副本：改它不会影响后续采样。
type CheckState struct {
	Name      string
	Severity  Severity
	Message   string
	Metrics   []Metric
	SampledAt time.Time
	Err       error
	Interval  time.Duration
	// Stale 表示最近一次采样已经超过 staleAfterIntervals 个周期，或者从未采样过。
	// 为 true 时 Severity 会被置成 SeverityUnknown：陈旧的值不能当作当前状态，
	// 原来的判定只留在 Message 里供排查。
	Stale bool
}
