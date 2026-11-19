package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newGormTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func decodeLogRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log record was written")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("record is not valid JSON: %v\n%s", err, line)
	}
	return record
}

func TestGormLoggerUsesRequestScopedLogger(t *testing.T) {
	base, buf := newGormTestLogger(t)
	gormLog := NewGormLogger(base, "info", time.Hour)
	ctx := IntoContext(context.Background(), base.With("request_id", "req-123"))

	gormLog.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)

	record := decodeLogRecord(t, buf)
	if record["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want %q", record["request_id"], "req-123")
	}
	if record["component"] != "gorm" {
		t.Errorf("component = %v, want %q", record["component"], "gorm")
	}
	if record["sql"] != "SELECT 1" {
		t.Errorf("sql = %v, want %q", record["sql"], "SELECT 1")
	}
}

func TestGormLoggerFallsBackToProcessLogger(t *testing.T) {
	base, buf := newGormTestLogger(t)
	gormLog := NewGormLogger(base, "info", time.Hour)

	gormLog.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)

	record := decodeLogRecord(t, buf)
	if _, ok := record["request_id"]; ok {
		t.Errorf("request_id = %v, want no request_id outside a request", record["request_id"])
	}
	if record["component"] != "gorm" {
		t.Errorf("component = %v, want %q", record["component"], "gorm")
	}
}

func TestGormLoggerErrorKeepsRequestScope(t *testing.T) {
	base, buf := newGormTestLogger(t)
	gormLog := NewGormLogger(base, "error", time.Hour)
	ctx := IntoContext(context.Background(), base.With("request_id", "req-123"))

	gormLog.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT broken", 0 }, errors.New("boom"))

	record := decodeLogRecord(t, buf)
	if record["level"] != "ERROR" {
		t.Errorf("level = %v, want %q", record["level"], "ERROR")
	}
	if record["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want %q", record["request_id"], "req-123")
	}
	if record["msg"] != "database statement failed" {
		t.Errorf("msg = %v, want %q", record["msg"], "database statement failed")
	}
}

func TestGormLoggerFormatsMessageArguments(t *testing.T) {
	base, buf := newGormTestLogger(t)
	gormLog := NewGormLogger(base, "error", time.Hour)

	gormLog.Error(context.Background(), "failed to initialize database, got error %v", errors.New("boom"))

	record := decodeLogRecord(t, buf)
	if got, want := record["msg"], "failed to initialize database, got error boom"; got != want {
		t.Errorf("msg = %v, want %q", got, want)
	}
}

func TestNewGormLoggerNilUsesDefault(t *testing.T) {
	previous := slog.Default()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	NewGormLogger(nil, "info", time.Hour).Info(context.Background(), "hello")

	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("record was not written to the default logger: %s", buf.String())
	}
}
