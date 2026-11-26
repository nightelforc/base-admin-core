package redis

import (
	"context"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
)

// stats 造一份快照，字段只填测试用到的部分。
func stats(mutate func(*goredis.PoolStats)) *goredis.PoolStats {
	s := &goredis.PoolStats{}
	if mutate != nil {
		mutate(s)
	}
	return s
}

func TestChecksSkipsNilClient(t *testing.T) {
	if got := Checks(nil); got != nil {
		t.Fatalf("Checks(nil) = %d 项, want nil", len(got))
	}
}

func TestChecksNamesThePoolCheck(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	items := Checks(client)
	if len(items) != 1 {
		t.Fatalf("Checks() = %d 项, want 1", len(items))
	}
	if got := items[0].Name(); got != "redis.pool" {
		t.Fatalf("Name() = %q, want redis.pool", got)
	}
}

func TestTimeoutsFiresOnIncrease(t *testing.T) {
	prev := stats(nil)
	cur := stats(func(s *goredis.PoolStats) { s.Timeouts = 2 })

	res, ok := Timeouts(prev, cur)
	if !ok {
		t.Fatal("Timeouts() 没有认领超时增量")
	}
	if res.Severity != core.SeverityWarn || res.Msg != "redis pool timeout" {
		t.Fatalf("Timeouts() = %+v, want warn/redis pool timeout", res)
	}

	// 累计值不变就不该报：否则每 30 秒复述一次同样的历史。
	if _, ok := Timeouts(cur, cur); ok {
		t.Fatal("Timeouts() 在增量 0 时仍然报出")
	}
}

func TestStarvationReportsPending(t *testing.T) {
	res, ok := Starvation(nil, stats(func(s *goredis.PoolStats) { s.PendingRequests = 3 }))
	if !ok || res.Severity != core.SeverityWarn {
		t.Fatalf("Starvation() = %+v, %v; want warn", res, ok)
	}
	if _, ok := Starvation(nil, stats(nil)); ok {
		t.Fatal("Starvation() 在无人排队时仍然报出")
	}
}

func TestUnusableIsNoticeNotWarn(t *testing.T) {
	prev := stats(nil)
	cur := stats(func(s *goredis.PoolStats) { s.Unusable = 1 })

	res, ok := Unusable(prev, cur)
	if !ok {
		t.Fatal("Unusable() 没有认领不可用连接的增量")
	}
	// 丢一条空闲连接是可自行恢复的现象，记 note 而不是 warn。
	if res.Severity != core.SeverityNotice {
		t.Fatalf("Unusable() severity = %v, want notice", res.Severity)
	}
}

func TestHealthyIsTheFallback(t *testing.T) {
	res, ok := Healthy(nil, stats(nil))
	if !ok || res.Severity != core.SeverityOK {
		t.Fatalf("Healthy() = %+v, %v; want ok", res, ok)
	}
}

func TestPoolMetricsMixesDeltasAndGauges(t *testing.T) {
	prev := stats(func(s *goredis.PoolStats) {
		s.Timeouts = 1
		s.WaitCount = 4
		s.WaitDurationNs = int64(500 * 1e6)
		s.Unusable = 2
	})
	cur := stats(func(s *goredis.PoolStats) {
		s.Timeouts = 3
		s.WaitCount = 10
		s.WaitDurationNs = int64(1500 * 1e6)
		s.Unusable = 2
		s.TotalConns = 20
		s.IdleConns = 5
		s.PendingRequests = 2
	})

	got := map[string]float64{}
	for _, m := range poolMetrics(prev, cur) {
		got[m.Name] = m.Value
	}

	want := map[string]float64{
		"timeouts": 2,
		"waits":    6,
		"waited":   1, // 1 秒
		"unusable": 0, // 增量 0：累计值没变
		"total":    20,
		"idle":     5,
		"pending":  2,
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("指标 %s = %v, want %v", name, got[name], value)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("poolMetrics() 产出 %d 个指标, want %d", len(got), len(want))
	}
}

func TestPoolMetricsBaselineIsZero(t *testing.T) {
	// Poll 的第一个采样用 cur 同时当基线，增量必须自然为 0，否则监控一启动就
	// 把历史计数当成本区间的活动报出来。
	s := stats(func(st *goredis.PoolStats) {
		st.Timeouts = 9
		st.WaitCount = 99
		st.WaitDurationNs = int64(3 * 1e9)
	})

	for _, m := range poolMetrics(s, s) {
		switch m.Name {
		case "timeouts", "waits", "waited":
			if m.Value != 0 {
				t.Fatalf("基线指标 %s = %v, want 0", m.Name, m.Value)
			}
		}
	}
}

func TestChecksSampleReadsPoolStats(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	items := Checks(client)
	// 第一个采样只建基线：不产生日志，但要有指标。
	res := items[0].Sample(context.Background())
	if res.Msg != "" {
		t.Fatalf("基线采样 Msg = %q, want 空（不出日志）", res.Msg)
	}
	if res.Severity != core.SeverityOK {
		t.Fatalf("基线采样 severity = %v, want ok", res.Severity)
	}
	if len(res.Metrics) != 7 {
		t.Fatalf("基线采样产出 %d 个指标, want 7", len(res.Metrics))
	}
}
