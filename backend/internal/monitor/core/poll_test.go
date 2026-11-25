package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func metricMap(res Result) map[string]Metric {
	m := make(map[string]Metric, len(res.Metrics))
	for _, mt := range res.Metrics {
		m[mt.Name] = mt
	}
	return m
}

func TestPollFirstSampleBuildsBaselineWithMetrics(t *testing.T) {
	calls := 0
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		calls++
		return calls, nil
	}, func(prev, cur int) []Metric {
		return []Metric{{Name: "delta", Unit: UnitCount, Value: float64(cur - prev)}}
	}, func(prev, cur int) (Result, bool) {
		return Result{Msg: "counter changed", Severity: SeverityNotice}, prev != cur
	})

	// 基线 tick 不出日志，但指标要在：读取方从启动就能画图，增量自然是 0。
	base := p.Sample(context.Background())
	if base.Msg != "" {
		t.Fatalf("first sample = %q, want no output (baseline only)", base.Msg)
	}
	if base.Severity != SeverityOK {
		t.Fatalf("baseline severity = %v, want OK", base.Severity)
	}
	if got := metricMap(base)["delta"]; got.Value != 0 {
		t.Fatalf("baseline delta = %v, want 0", got.Value)
	}

	res := p.Sample(context.Background())
	if res.Msg != "counter changed" {
		t.Fatalf("second sample = %q, want counter changed", res.Msg)
	}
	if res.Severity != SeverityNotice {
		t.Fatalf("second sample severity = %v, want notice", res.Severity)
	}
	if got := metricMap(res)["delta"]; got.Value != 1 {
		t.Fatalf("delta = %v, want 1", got.Value)
	}
}

func TestPollReportsTakeFailure(t *testing.T) {
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		return 0, errors.New("boom")
	}, nil)

	res := p.Sample(context.Background())
	if res.Severity != SeverityWarn {
		t.Fatalf("severity = %v, want warn", res.Severity)
	}
	if !strings.Contains(res.Msg, "sample failed") {
		t.Fatalf("msg = %q, want it to mention the failed sample", res.Msg)
	}
	if res.Err == nil {
		t.Fatal("failed sample must carry the error")
	}
	if len(res.Metrics) != 0 {
		t.Fatalf("failed sample produced %d metrics, want none", len(res.Metrics))
	}
}

func TestPollIsSilentWhenNoStrategyClaims(t *testing.T) {
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		return 1, nil
	}, func(prev, cur int) []Metric {
		return []Metric{{Name: "value", Unit: UnitCount, Value: float64(cur)}}
	}, func(_, _ int) (Result, bool) {
		return Result{}, false
	})

	p.Sample(context.Background())
	res := p.Sample(context.Background())
	if res.Msg != "" {
		t.Fatalf("sample = %q, want no output", res.Msg)
	}
	// 没有策略认领不等于没有数据：健康 tick 也必须带指标。
	if got := metricMap(res)["value"]; got.Value != 1 {
		t.Fatalf("healthy tick value = %v, want 1", got.Value)
	}
}

func TestPollUsesDefaultIntervalWhenUnset(t *testing.T) {
	p := NewPoll("counter", 0, func(context.Context) (int, error) { return 0, nil }, nil)
	if got := intervalOf(p); got != DefaultInterval {
		t.Fatalf("interval = %v, want %v", got, DefaultInterval)
	}
}

func TestDeltaNeverGoesNegative(t *testing.T) {
	if got := Delta(5, 9); got != 0 {
		t.Fatalf("Delta(5, 9) = %d, want 0", got)
	}
	if got := Delta(9, 5); got != 4 {
		t.Fatalf("Delta(9, 5) = %d, want 4", got)
	}
	if got := DurationDelta(time.Second, 3*time.Second); got != 0 {
		t.Fatalf("DurationDelta(1s, 3s) = %v, want 0", got)
	}
	if got := DurationDelta(5*time.Second, time.Second); got != 4*time.Second {
		t.Fatalf("DurationDelta(5s, 1s) = %v, want 4s", got)
	}
}
