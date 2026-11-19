package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewHandlerJSON(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler(Options{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	slog.New(handler).Info("hello", "user_id", 7)

	line := strings.TrimSpace(buf.String())
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, line)
	}
	if got["level"] != "info" {
		t.Errorf("level = %v, want %q", got["level"], "info")
	}
	if got["msg"] != "hello" {
		t.Errorf("msg = %v, want %q", got["msg"], "hello")
	}
	if got["user_id"] != float64(7) {
		t.Errorf("user_id = %v, want 7", got["user_id"])
	}
	ts, ok := got["time"].(string)
	if !ok {
		t.Fatalf("time = %v, want a string", got["time"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("time %q does not parse as RFC 3339: %v", ts, err)
	}
}

func TestNewHandlerText(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler(Options{Level: "info", Format: "text"}, &buf)
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	slog.New(handler).Info("hello", "user_id", 7)

	line := strings.TrimSpace(buf.String())
	if !strings.Contains(line, "level=info") {
		t.Errorf("output %q does not contain level=info", line)
	}
	if !strings.Contains(line, "msg=hello") {
		t.Errorf("output %q does not contain msg=hello", line)
	}

	var ts string
	for _, field := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(field, "time="); ok {
			ts = v
		}
	}
	if ts == "" {
		t.Fatalf("output %q has no time field", line)
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("time %q does not parse as RFC 3339: %v", ts, err)
	}
}

func TestNewHandlerAppliesLevel(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler(Options{Level: "warn", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	log := slog.New(handler)
	log.Debug("dropped")
	log.Info("dropped")
	log.Warn("kept")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d records, want 1:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"level":"warn"`) || !strings.Contains(lines[0], "kept") {
		t.Errorf("unexpected record: %s", lines[0])
	}
}

func TestReplaceAttrTextTimeUsesRFC3339Nano(t *testing.T) {
	ts := time.Date(2026, 11, 19, 16, 25, 21, 123456789, time.UTC)
	got := replaceAttr(false)(nil, slog.Time(slog.TimeKey, ts))

	if got.Value.Kind() != slog.KindString {
		t.Fatalf("kind = %v, want string", got.Value.Kind())
	}
	if want := "2026-11-19T16:25:21.123456789Z"; got.Value.String() != want {
		t.Errorf("time = %q, want %q", got.Value.String(), want)
	}
}

// JSON 处理器已经写出 RFC 3339 时间戳，因此该值必须原样到达编码器
// （time.Time 值，而不是预先格式化的字符串）。
func TestReplaceAttrJSONKeepsTimeValue(t *testing.T) {
	ts := time.Date(2026, 11, 19, 16, 25, 21, 123456789, time.UTC)
	got := replaceAttr(true)(nil, slog.Time(slog.TimeKey, ts))

	if got.Value.Kind() != slog.KindTime {
		t.Fatalf("kind = %v, want time", got.Value.Kind())
	}
	if !got.Value.Time().Equal(ts) {
		t.Errorf("time = %v, want %v", got.Value.Time(), ts)
	}
}

func TestReplaceAttrLevelNames(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
		{slog.LevelWarn + 2, "warn+2"},
	}
	for _, tc := range cases {
		got := replaceAttr(false)(nil, slog.Any(slog.LevelKey, tc.level))
		if got.Value.String() != tc.want {
			t.Errorf("level %v = %q, want %q", tc.level, got.Value.String(), tc.want)
		}
	}
}

// 分组内的字段属于调用方，必须原样通过，即使它们复用了 level 这类内置键。
func TestReplaceAttrLeavesGroupedFields(t *testing.T) {
	attr := slog.Any(slog.LevelKey, slog.LevelInfo)
	got := replaceAttr(false)([]string{"request"}, attr)
	if got.Value.Any() != slog.LevelInfo {
		t.Errorf("grouped level = %v, want the original slog.Level", got.Value)
	}

	got = replaceAttr(false)(nil, slog.String(slog.LevelKey, "custom"))
	if got.Value.String() != "custom" {
		t.Errorf("string level = %q, want %q", got.Value.String(), "custom")
	}
}

func TestNewHandlerGroupIsNotRewritten(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler(Options{Level: "info", Format: "json"}, &buf)
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	slog.New(handler).Info("hello", slog.Group("g", slog.String(slog.LevelKey, "keep")))

	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	group, ok := got["g"].(map[string]any)
	if !ok {
		t.Fatalf("g = %v, want an object", got["g"])
	}
	if group[slog.LevelKey] != "keep" {
		t.Errorf("g.level = %v, want %q", group[slog.LevelKey], "keep")
	}
}

func TestParseLevel(t *testing.T) {
	valid := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range valid {
		got, err := parseLevel(in)
		if err != nil {
			t.Errorf("parseLevel(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"", "trace", "verbose"} {
		if _, err := parseLevel(in); err == nil {
			t.Errorf("parseLevel(%q) = nil error, want error", in)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := From(ctx); got != slog.Default() {
		t.Errorf("From(empty) = %v, want slog.Default()", got)
	}

	scoped := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx = IntoContext(ctx, scoped)
	if got := From(ctx); got != scoped {
		t.Errorf("From(ctx) = %v, want the scoped logger", got)
	}

	ctx = IntoContext(context.Background(), nil)
	if got := From(ctx); got != slog.Default() {
		t.Errorf("From(nil logger) = %v, want slog.Default()", got)
	}
}
