package postgres

import (
	"testing"
	"time"
)

// TestPingTimeoutOf 盯住"零值回落默认值"这一条：调用方不配 PingTimeout 时，
// 校验方（internal/config）与这里必须看到同一个数，否则两边对"实际等多久"的理解会分叉。
func TestPingTimeoutOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want time.Duration
	}{
		{name: "unset falls back to default", opts: Options{}, want: DefaultPingTimeout},
		{name: "zero falls back to default", opts: Options{PingTimeout: 0}, want: DefaultPingTimeout},
		{name: "negative falls back to default", opts: Options{PingTimeout: -time.Second}, want: DefaultPingTimeout},
		{name: "configured value wins", opts: Options{PingTimeout: 1500 * time.Millisecond}, want: 1500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pingTimeoutOf(tc.opts); got != tc.want {
				t.Fatalf("pingTimeoutOf(%+v) = %v, want %v", tc.opts, got, tc.want)
			}
		})
	}
}

// 容量查询与 ping 用的是两段预算，改动其中一段不应悄悄改动另一段。
func TestCapacityTimeoutIsIndependentOfPing(t *testing.T) {
	if capacityTimeout <= 0 || capacityTimeout >= DefaultPingTimeout {
		t.Fatalf("capacityTimeout = %v, want it to be positive and shorter than %v", capacityTimeout, DefaultPingTimeout)
	}
}
