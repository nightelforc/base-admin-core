package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
)

func newLoggingTestEngine(t *testing.T) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	engine := gin.New()
	engine.Use(RequestIDMiddleware(), AccessLog(base))
	return engine, &buf
}

func requestWithID(method, path, id string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(RequestIDHeader, id)
	return req
}

func logLines(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	body := strings.TrimSpace(buf.String())
	if body == "" {
		t.Fatal("no log record was written")
	}
	return strings.Split(body, "\n")
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, line)
	}
	return record
}

func TestAccessLogInstallsRequestScopedLogger(t *testing.T) {
	engine, buf := newLoggingTestEngine(t)
	engine.GET("/ping", func(c *gin.Context) {
		logger.From(c.Request.Context()).Info("handler ran")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, requestWithID(http.MethodGet, "/ping", "req-123"))

	lines := logLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2:\n%s", len(lines), buf.String())
	}

	handlerRecord := decodeLine(t, lines[0])
	if handlerRecord["msg"] != "handler ran" {
		t.Errorf("first msg = %v, want %q", handlerRecord["msg"], "handler ran")
	}
	if handlerRecord["request_id"] != "req-123" {
		t.Errorf("handler request_id = %v, want %q", handlerRecord["request_id"], "req-123")
	}

	accessRecord := decodeLine(t, lines[1])
	if accessRecord["msg"] != "http request" {
		t.Errorf("second msg = %v, want %q", accessRecord["msg"], "http request")
	}
	if accessRecord["request_id"] != "req-123" {
		t.Errorf("access request_id = %v, want %q", accessRecord["request_id"], "req-123")
	}
	if accessRecord["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want %d", accessRecord["status"], http.StatusOK)
	}
	// request_id comes from the scoped logger, not from the attribute list.
	if got := strings.Count(lines[1], `"request_id"`); got != 1 {
		t.Errorf("access line has %d request_id fields, want 1: %s", got, lines[1])
	}
}

func TestAccessLogScopesProbesWithoutAccessLine(t *testing.T) {
	engine, buf := newLoggingTestEngine(t)
	engine.GET("/healthz", func(c *gin.Context) {
		logger.From(c.Request.Context()).Info("probe served")
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, requestWithID(http.MethodGet, "/healthz", "req-123"))

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want only the handler line:\n%s", len(lines), buf.String())
	}
	record := decodeLine(t, lines[0])
	if record["msg"] != "probe served" {
		t.Errorf("msg = %v, want %q", record["msg"], "probe served")
	}
	if record["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want %q", record["request_id"], "req-123")
	}
}

func TestRecoveryLogsWithRequestScope(t *testing.T) {
	engine, buf := newLoggingTestEngine(t)
	engine.Use(Recovery())
	engine.GET("/panic", func(c *gin.Context) { panic("boom") })

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, requestWithID(http.MethodGet, "/panic", "req-123"))

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var panicLine string
	for _, line := range logLines(t, buf) {
		if strings.Contains(line, "panic recovered") {
			panicLine = line
			break
		}
	}
	if panicLine == "" {
		t.Fatalf("no panic record was written:\n%s", buf.String())
	}
	record := decodeLine(t, panicLine)
	if record["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want %q", record["request_id"], "req-123")
	}
	if record["panic"] != "boom" {
		t.Errorf("panic = %v, want %q", record["panic"], "boom")
	}
	if got := strings.Count(panicLine, `"request_id"`); got != 1 {
		t.Errorf("panic line has %d request_id fields, want 1: %s", got, panicLine)
	}
}
