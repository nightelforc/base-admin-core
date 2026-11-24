package core

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer 让采样 goroutine 的写入与测试的读取之间不产生数据竞争。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestLogger(buf *syncBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// waitFor 轮询到出现 marker 或超时。用轮询而不是固定 sleep，
// 是为了不依赖具体的 tick 次数（CI 上机器慢时也能过）。
func waitFor(t *testing.T, buf *syncBuffer, marker string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), marker) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("log output did not contain %q, got:\n%s", marker, buf.String())
}

type fakeCheck struct {
	name   string
	every  time.Duration
	sample func() Result
}

func (f *fakeCheck) Name() string            { return f.name }
func (f *fakeCheck) Interval() time.Duration { return f.every }
func (f *fakeCheck) Sample(context.Context) Result {
	return f.sample()
}

func TestRegistryRunsChecksUntilCancel(t *testing.T) {
	buf := &syncBuffer{}
	registry := New(newTestLogger(buf))
	registry.Add(&fakeCheck{
		name:  "demo",
		every: time.Millisecond,
		sample: func() Result {
			return Result{Msg: "demo tick", Level: slog.LevelInfo, Attrs: []any{"k", "v"}}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.Run(ctx)
	}()

	waitFor(t, buf, "demo tick")
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Registry.Run did not return after ctx was cancelled")
	}

	out := buf.String()
	for _, want := range []string{"level=INFO", "monitor=demo", "k=v"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRegistrySkipsEmptyResults(t *testing.T) {
	buf := &syncBuffer{}
	registry := New(newTestLogger(buf))
	registry.Add(&fakeCheck{
		name:   "quiet",
		every:  time.Millisecond,
		sample: func() Result { return Result{} },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.Run(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if out := buf.String(); out != "" {
		t.Fatalf("expected no output, got:\n%s", out)
	}
}

func TestRegistrySurvivesPanickingCheck(t *testing.T) {
	buf := &syncBuffer{}
	registry := New(newTestLogger(buf))
	registry.Add(&fakeCheck{
		name:   "boom",
		every:  time.Millisecond,
		sample: func() Result { panic("strategy bug") },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.Run(ctx)
	}()

	waitFor(t, buf, "monitor check panicked")

	// 一次 panic 不能让监控项停下：下一个 tick 仍应继续执行。
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("panic log missing monitor name:\n%s", buf.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Registry.Run did not return after a panicking check")
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	registry := New(nil)
	registry.Add(&fakeCheck{name: "dup", sample: func() Result { return Result{} }})

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic on duplicate check name")
		}
		if msg, _ := recovered.(string); !strings.Contains(msg, "duplicate check name") {
			t.Fatalf("panic = %v, want duplicate check name", recovered)
		}
	}()

	registry.Add(&fakeCheck{name: "dup", sample: func() Result { return Result{} }})
}

func TestRegistryRejectsEmptyName(t *testing.T) {
	registry := New(nil)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty check name")
		}
	}()

	registry.Add(&fakeCheck{name: "", sample: func() Result { return Result{} }})
}

func TestRegistryChecksReturnsCopy(t *testing.T) {
	registry := New(nil)
	registry.Add(&fakeCheck{name: "demo", sample: func() Result { return Result{} }})

	got := registry.Checks()
	got[0] = &fakeCheck{name: "mutated", sample: func() Result { return Result{} }}

	if name := registry.Checks()[0].Name(); name != "demo" {
		t.Fatalf("registry entry changed to %q through the returned slice", name)
	}
}

func TestIntervalOfFallsBackToDefault(t *testing.T) {
	if got := intervalOf(&fakeCheck{every: 0}); got != DefaultInterval {
		t.Fatalf("intervalOf(0) = %v, want %v", got, DefaultInterval)
	}
	if got := intervalOf(&fakeCheck{every: -time.Second}); got != DefaultInterval {
		t.Fatalf("intervalOf(negative) = %v, want %v", got, DefaultInterval)
	}
	if got := intervalOf(&fakeCheck{every: 5 * time.Millisecond}); got != 5*time.Millisecond {
		t.Fatalf("intervalOf(5ms) = %v, want 5ms", got)
	}
}
