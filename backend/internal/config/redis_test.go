package config

import (
	"testing"
	"time"
)

// TestRedisOptionsCarryConnectionTimes 钉住两个连接回收参数的透传。它们是纯粹的
// 数据字段，漏传不会报错，只会让连接按驱动默认（空闲 30 分钟、寿命不限）回收，
// 与配置里写的数不一致。
func TestRedisOptionsCarryConnectionTimes(t *testing.T) {
	r := Redis{
		ConnMaxIdleTime: 10 * time.Minute,
		ConnMaxLifetime: 45 * time.Minute,
	}

	opts := r.Options()
	if got := opts.ConnMaxIdleTime; got != 10*time.Minute {
		t.Fatalf("Options().ConnMaxIdleTime = %v, want 10m", got)
	}
	if got := opts.ConnMaxLifetime; got != 45*time.Minute {
		t.Fatalf("Options().ConnMaxLifetime = %v, want 45m", got)
	}
}

// TestRedisMaxRetriesDefault 固定模板里的默认值：重试一次是权衡后的选择，
// 改动它应当是一次有意为之的提交，而不是顺手跟驱动的默认值对齐。
// TestLoadDerivesClientNameFromAppName 钉住"CLIENT LIST 的 name 与日志里的 app
// 同名"这件事：它不是一个可配置项，唯一的来源就是 app.name（首尾空白去掉）。
func TestLoadDerivesClientNameFromAppName(t *testing.T) {
	cfg := loadForSummary(t, `
app:
  name: "  base-admin  "
`)

	if got := cfg.Redis.Options().ClientName; got != "base-admin" {
		t.Fatalf("Options().ClientName = %q, want base-admin", got)
	}
}

func TestRedisMaxRetriesDefault(t *testing.T) {
	cfg := loadForSummary(t, "")
	if got := cfg.Redis.MaxRetries; got != 1 {
		t.Fatalf("redis.max_retries = %d, want 1", got)
	}
}

// TestRedisOptionsTranslateDisabledRetries 盯住两层的语义差异：配置层用 0 表示
// 不重试，驱动用 -1 表示关闭、用 0 表示"使用默认 3 次"。翻译漏掉的话，
// 配 0 会得到比不配更多的重试。
func TestRedisOptionsTranslateDisabledRetries(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  int
		want int
	}{
		{name: "zero disables retries", cfg: 0, want: -1},
		{name: "positive passes through", cfg: 2, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Redis{MaxRetries: tc.cfg}
			if got := r.Options().MaxRetries; got != tc.want {
				t.Fatalf("Options().MaxRetries = %d, want %d", got, tc.want)
			}
		})
	}
}
