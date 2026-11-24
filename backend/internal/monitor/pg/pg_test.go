package pg

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
)

func attrMap(res core.Result) map[string]any {
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

func TestWaiting(t *testing.T) {
	prev := sql.DBStats{WaitCount: 2, WaitDuration: time.Second}
	cur := sql.DBStats{
		MaxOpenConnections: 15,
		OpenConnections:    15,
		InUse:              4,
		Idle:               11,
		WaitCount:          12,
		WaitDuration:       26 * time.Second,
	}

	res, ok := Waiting(prev, cur)
	if !ok {
		t.Fatal("Waiting should claim a snapshot with new waits")
	}
	if res.Msg != "postgres pool waiting" || res.Level != slog.LevelWarn {
		t.Fatalf("got %q at %v", res.Msg, res.Level)
	}

	attrs := attrMap(res)
	if got := attrs["waits"]; got != int64(10) {
		t.Fatalf("waits = %v, want 10", got)
	}
	if got := attrs["waited"]; got != 25*time.Second {
		t.Fatalf("waited = %v, want 25s", got)
	}
	if got := attrs["avg_wait"]; got != 2500*time.Millisecond {
		t.Fatalf("avg_wait = %v, want 2.5s", got)
	}
	if got := attrs["max_open"]; got != 15 {
		t.Fatalf("max_open = %v, want 15", got)
	}
}

func TestWaitingIgnoresUnchangedCounters(t *testing.T) {
	s := sql.DBStats{WaitCount: 7, WaitDuration: 3 * time.Second}
	if _, ok := Waiting(s, s); ok {
		t.Fatal("Waiting must not claim a snapshot without new waits")
	}
}

func TestExhaustion(t *testing.T) {
	cases := []struct {
		name    string
		inUse   int
		maxOpen int
		want    bool
	}{
		{name: "at threshold", inUse: 12, maxOpen: 15, want: true},
		{name: "above threshold", inUse: 15, maxOpen: 15, want: true},
		{name: "below threshold", inUse: 11, maxOpen: 15, want: false},
		{name: "unlimited pool", inUse: 99, maxOpen: 0, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := sql.DBStats{InUse: tc.inUse, MaxOpenConnections: tc.maxOpen}
			if _, ok := Exhaustion(0.8)(sql.DBStats{}, cur); ok != tc.want {
				t.Fatalf("claimed = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestRecycling(t *testing.T) {
	prev := sql.DBStats{MaxIdleClosed: 1}
	cur := sql.DBStats{
		MaxOpenConnections: 15,
		OpenConnections:    8,
		InUse:              2,
		Idle:               6,
		MaxIdleClosed:      4,
		MaxLifetimeClosed:  2,
	}

	res, ok := Recycling(prev, cur)
	if !ok {
		t.Fatal("Recycling should claim a snapshot with closed connections")
	}
	if res.Msg != "postgres pool recycling connections" || res.Level != slog.LevelInfo {
		t.Fatalf("got %q at %v", res.Msg, res.Level)
	}

	attrs := attrMap(res)
	if got := attrs["idle_closed"]; got != int64(3) {
		t.Fatalf("idle_closed = %v, want 3", got)
	}
	if got := attrs["lifetime_closed"]; got != int64(2) {
		t.Fatalf("lifetime_closed = %v, want 2", got)
	}
}

func TestHealthyAlwaysClaims(t *testing.T) {
	res, ok := Healthy(sql.DBStats{}, sql.DBStats{MaxOpenConnections: 15, InUse: 1})
	if !ok {
		t.Fatal("Healthy must always claim so it can serve as the fallback")
	}
	if res.Msg != "postgres pool healthy" || res.Level != slog.LevelDebug {
		t.Fatalf("got %q at %v", res.Msg, res.Level)
	}
}

func TestDefaultStrategiesPreferHeavierSignals(t *testing.T) {
	// 同时满足 waiting 与 exhaustion 时，应该报出更重的等待。
	cur := sql.DBStats{
		MaxOpenConnections: 15,
		InUse:              14,
		WaitCount:          1,
		WaitDuration:       time.Second,
	}

	for _, strategy := range DefaultStrategies() {
		if res, ok := strategy(sql.DBStats{}, cur); ok {
			if res.Msg != "postgres pool waiting" {
				t.Fatalf("first match = %q, want postgres pool waiting", res.Msg)
			}
			return
		}
	}
	t.Fatal("no strategy matched")
}

func TestPollJudgesIntervalDelta(t *testing.T) {
	snaps := []sql.DBStats{
		{MaxOpenConnections: 15, OpenConnections: 5, InUse: 1, Idle: 4, WaitCount: 5, WaitDuration: 3 * time.Second},
		{MaxOpenConnections: 15, OpenConnections: 15, InUse: 3, Idle: 12, WaitCount: 8, WaitDuration: 9 * time.Second},
	}
	idx := 0
	take := func(context.Context) (sql.DBStats, error) {
		s := snaps[idx]
		if idx < len(snaps)-1 {
			idx++
		}
		return s, nil
	}

	check := core.NewPoll("postgres.pool", core.DefaultInterval, take, DefaultStrategies()...)
	if res := check.Sample(context.Background()); res.Msg != "" {
		t.Fatalf("first sample = %q, want baseline only", res.Msg)
	}

	// 第二个 tick 对应第一段增量：3 次等待、6 秒。
	res := check.Sample(context.Background())
	if res.Msg != "postgres pool waiting" {
		t.Fatalf("msg = %q, want postgres pool waiting", res.Msg)
	}
	if got := attrMap(res)["waits"]; got != int64(3) {
		t.Fatalf("waits = %v, want 3 (only the delta)", got)
	}

	// 快照不再变化：没有新增量，回落到 healthy。
	if res := check.Sample(context.Background()); res.Msg != "postgres pool healthy" {
		t.Fatalf("msg = %q, want postgres pool healthy", res.Msg)
	}
}

func TestChecksWithoutDBReturnsNothing(t *testing.T) {
	got, err := Checks(nil)
	if err != nil {
		t.Fatalf("Checks(nil) failed: %v", err)
	}
	if got != nil {
		t.Fatalf("Checks(nil) = %v, want nil", got)
	}
}

func TestChecksUsesDefaultStrategies(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{ConnPool: &sql.DB{}}}

	checks, err := Checks(db)
	if err != nil {
		t.Fatalf("Checks failed: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	if got := checks[0].Name(); got != "postgres.pool" {
		t.Fatalf("name = %q, want postgres.pool", got)
	}
}

func TestChecksReportsUnusableHandle(t *testing.T) {
	if _, err := Checks(&gorm.DB{Config: &gorm.Config{}}); err == nil {
		t.Fatal("expected an error when the connection pool is not *sql.DB")
	}
}
