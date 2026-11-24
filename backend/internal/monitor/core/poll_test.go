package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func attrMap(res Result) map[string]any {
	m := make(map[string]any, len(res.Attrs)/2)
	for i := 0; i+1 < len(res.Attrs); i += 2 {
		key, ok := res.Attrs[i].(string)
		if !ok {
			continue
		}
		m[key] = res.Attrs[i+1]
	}
	return m
}

func TestPollFirstSampleOnlyBuildsBaseline(t *testing.T) {
	calls := 0
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		calls++
		return calls, nil
	}, func(prev, cur int) (Result, bool) {
		return Result{Msg: "counter changed", Attrs: []any{"delta", cur - prev}}, prev != cur
	})

	if res := p.Sample(context.Background()); res.Msg != "" {
		t.Fatalf("first sample = %q, want no output (baseline only)", res.Msg)
	}

	res := p.Sample(context.Background())
	if res.Msg != "counter changed" {
		t.Fatalf("second sample = %q, want counter changed", res.Msg)
	}
	if got := attrMap(res)["delta"]; got != 1 {
		t.Fatalf("delta = %v, want 1", got)
	}
}

func TestPollReportsTakeFailure(t *testing.T) {
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		return 0, errors.New("boom")
	})

	res := p.Sample(context.Background())
	if res.Level != slog.LevelWarn {
		t.Fatalf("level = %v, want warn", res.Level)
	}
	if !strings.Contains(res.Msg, "sample failed") {
		t.Fatalf("msg = %q, want it to mention the failed sample", res.Msg)
	}
	if got := attrMap(res)["error"]; got == nil {
		t.Fatal("failed sample must carry the error")
	}
}

func TestPollIsSilentWhenNoStrategyClaims(t *testing.T) {
	p := NewPoll("counter", time.Second, func(context.Context) (int, error) {
		return 1, nil
	}, func(_, _ int) (Result, bool) {
		return Result{}, false
	})

	p.Sample(context.Background())
	if res := p.Sample(context.Background()); res.Msg != "" {
		t.Fatalf("sample = %q, want no output", res.Msg)
	}
}

func TestPollUsesDefaultIntervalWhenUnset(t *testing.T) {
	p := NewPoll("counter", 0, func(context.Context) (int, error) { return 0, nil })
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
