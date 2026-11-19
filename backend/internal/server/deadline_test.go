package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// 处理器必须能读到截止时间，否则下游无法据此设置自己的超时预算。
func TestDeadlineIsVisibleToHandler(t *testing.T) {
	timeout := 200 * time.Millisecond
	var remaining time.Duration
	var ok bool

	handler := newDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, hasDeadline := r.Context().Deadline()
		ok = hasDeadline
		if hasDeadline {
			remaining = time.Until(deadline)
		}
		w.WriteHeader(http.StatusNoContent)
	}), timeout, discardLogger())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))

	if !ok {
		t.Fatal("request context has no deadline, want one")
	}
	if remaining <= 0 || remaining > timeout {
		t.Fatalf("remaining = %s, want between 0 and %s", remaining, timeout)
	}
}

// 超时后 context 必须被取消，这样阻塞在 IO 上的下游调用才能返回。
func TestDeadlineCancelsContext(t *testing.T) {
	timeout := 20 * time.Millisecond
	var ctxErr error

	handler := newDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		ctxErr = r.Context().Err()
		w.WriteHeader(http.StatusGatewayTimeout)
	}), timeout, discardLogger())

	start := time.Now()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	elapsed := time.Since(start)

	if !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", ctxErr)
	}
	if elapsed > time.Second {
		t.Fatalf("handler took %s, want it released by the deadline", elapsed)
	}
	// 本层不替处理器决定响应，状态码应原样保留。
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
}

// 关闭时不注入截止时间，行为与未加中间件完全一致。
func TestDeadlineDisabled(t *testing.T) {
	var hasDeadline bool

	handler := newDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusNoContent)
	}), 0, discardLogger())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))

	if hasDeadline {
		t.Fatal("request context has a deadline, want none when disabled")
	}
}

// 关键回归：流式响应不会被本层缓冲，也不会在超过截止时间后被替换成 504。
// 这保证 SSE / WebSocket 在 deadline 只作为预算的模型下依然可用。
func TestStreamingResponseIsNotBuffered(t *testing.T) {
	timeout := 20 * time.Millisecond

	handler := newDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: tick\n\n")
		_ = http.NewResponseController(w).Flush()

		// 明显越过 deadline：看第一帧是否已经发出、第二帧是否还能继续写。
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, "event: tock\n\n")
	}), timeout, discardLogger())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/events", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"tick", "tock"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want it to contain %q", body, want)
		}
	}
	if strings.Contains(body, "50400") {
		t.Fatalf("body = %q, want no timeout envelope", body)
	}
	if !rec.Flushed {
		t.Fatal("response was not flushed, want streaming writes to reach the client")
	}
}

// 超时告警要能和访问日志用同一个 request_id 串联。
func TestDeadlineLogsRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	handler := newDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-123")
		<-r.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	}), 20*time.Millisecond, logger)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))

	if !strings.Contains(buf.String(), "request_id=req-123") {
		t.Fatalf("log = %q, want it to carry the request id", buf.String())
	}
}
