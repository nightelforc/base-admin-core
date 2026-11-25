package config

import "testing"

// TestRedisMaxRetriesDefault 固定模板里的默认值：重试一次是权衡后的选择，
// 改动它应当是一次有意为之的提交，而不是顺手跟驱动的默认值对齐。
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
