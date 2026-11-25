package core

import (
	"context"
	"testing"
	"time"
)

// 用注入的时钟做状态测试：陈旧判定是纯时间比较，不该靠 sleep 去凑。
func TestSnapshotKeepsHealthySamples(t *testing.T) {
	registry := New(nil)
	check := &fakeCheck{
		name:  "demo",
		every: time.Second,
		sample: func() Result {
			return Result{
				Severity: SeverityOK,
				Metrics:  []Metric{{Name: "in_use", Unit: UnitCount, Value: 0}},
			}
		},
	}
	registry.Add(check)

	at := time.Unix(1700000000, 0).UTC()
	registry.now = func() time.Time { return at }
	registry.record(check, check.Sample(context.Background()), at)

	states := registry.Snapshot().Checks
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	got := states[0]
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Severity != SeverityOK {
		t.Fatalf("severity = %v, want ok", got.Severity)
	}
	if got.Stale {
		t.Fatal("fresh sample marked stale")
	}
	if got.Message != "" {
		t.Fatalf("message = %q, want empty", got.Message)
	}
	if len(got.Metrics) != 1 || got.Metrics[0].Name != "in_use" {
		t.Fatalf("metrics = %+v", got.Metrics)
	}
	if !got.SampledAt.Equal(at) {
		t.Fatalf("sampled_at = %v, want %v", got.SampledAt, at)
	}
	if got.Interval != time.Second {
		t.Fatalf("interval = %v", got.Interval)
	}
}

func TestSnapshotMarksStaleAfterThreeIntervals(t *testing.T) {
	registry := New(nil)
	check := &fakeCheck{name: "demo", every: time.Second, sample: func() Result { return Result{} }}
	registry.Add(check)

	at := time.Unix(1700000000, 0).UTC()
	registry.record(check, Result{Msg: "postgres pool waiting", Severity: SeverityWarn}, at)

	registry.now = func() time.Time { return at.Add(3 * time.Second) }
	if state := registry.Snapshot().Checks[0]; state.Stale {
		t.Fatal("exactly three intervals should still count as fresh")
	}

	registry.now = func() time.Time { return at.Add(3*time.Second + time.Nanosecond) }
	state := registry.Snapshot().Checks[0]
	if !state.Stale {
		t.Fatal("sample older than three intervals was not marked stale")
	}
	if state.Severity != SeverityUnknown {
		t.Fatalf("stale severity = %v, want unknown", state.Severity)
	}
	if state.Message != "postgres pool waiting" {
		t.Fatalf("stale message = %q, want the last finding", state.Message)
	}
}

func TestSnapshotIncludesUnsampledChecks(t *testing.T) {
	registry := New(nil)
	registry.Add(&fakeCheck{name: "demo", every: time.Second, sample: func() Result { return Result{} }})

	states := registry.Snapshot().Checks
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if !states[0].Stale {
		t.Fatal("unsampled check should be stale")
	}
	if states[0].Severity != SeverityUnknown {
		t.Fatalf("severity = %v, want unknown", states[0].Severity)
	}
	if !states[0].SampledAt.IsZero() {
		t.Fatalf("sampled_at = %v, want zero", states[0].SampledAt)
	}
}

func TestSnapshotCarriesInstance(t *testing.T) {
	snapshot := New(nil).Snapshot()
	if snapshot.Instance.PID <= 0 {
		t.Fatalf("pid = %d, want a positive process id", snapshot.Instance.PID)
	}
	if snapshot.Checks == nil {
		t.Fatal("checks should be an empty slice, not nil")
	}
}

func TestSnapshotCopiesMetrics(t *testing.T) {
	registry := New(nil)
	check := &fakeCheck{name: "demo", every: time.Second, sample: func() Result { return Result{} }}
	registry.Add(check)
	at := time.Unix(1700000000, 0).UTC()
	registry.record(check, Result{Metrics: []Metric{{Name: "waits", Unit: UnitCount, Value: 1}}}, at)

	first := registry.Snapshot().Checks
	first[0].Metrics[0].Value = 999

	second := registry.Snapshot().Checks
	if second[0].Metrics[0].Value != 1 {
		t.Fatalf("mutating a snapshot changed the registry: %v", second[0].Metrics[0].Value)
	}
}

func TestSnapshotSortsByName(t *testing.T) {
	registry := New(nil)
	b := &fakeCheck{name: "b", every: time.Second, sample: func() Result { return Result{} }}
	a := &fakeCheck{name: "a", every: time.Second, sample: func() Result { return Result{} }}
	registry.Add(b, a)

	at := time.Unix(1700000000, 0).UTC()
	registry.record(b, Result{Severity: SeverityOK}, at)
	registry.record(a, Result{Severity: SeverityOK}, at)

	states := registry.Snapshot().Checks
	if len(states) != 2 || states[0].Name != "a" || states[1].Name != "b" {
		t.Fatalf("states not sorted by name: %+v", states)
	}
}

func TestRunRecordsTicksWithoutMessages(t *testing.T) {
	registry := New(nil)
	registry.Add(&fakeCheck{
		name:  "quiet",
		every: time.Millisecond,
		sample: func() Result {
			return Result{Severity: SeverityOK, Metrics: []Metric{{Name: "x", Unit: UnitCount, Value: 1}}}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go registry.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if states := registry.Snapshot().Checks; len(states) == 1 && !states[0].SampledAt.IsZero() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("healthy tick was never recorded")
}

func TestPanicIsRecordedAsUnknown(t *testing.T) {
	buf := &syncBuffer{}
	registry := New(newTestLogger(buf))
	registry.Add(&fakeCheck{name: "boom", every: time.Millisecond, sample: func() Result {
		panic("boom")
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go registry.Run(ctx)
	waitFor(t, buf, "monitor check panicked")

	states := registry.Snapshot().Checks
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if states[0].Severity != SeverityUnknown {
		t.Fatalf("severity = %v, want unknown", states[0].Severity)
	}
	if states[0].Message != "check panicked" {
		t.Fatalf("message = %q", states[0].Message)
	}
	if states[0].Err == nil {
		t.Fatal("panic reason was not recorded")
	}
	if states[0].SampledAt.IsZero() {
		t.Fatal("panic tick did not update sampled_at")
	}
}
