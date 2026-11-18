package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nightelforc/base-admin-core/backend/pkg/response"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequestTimeoutPassesThroughFastHandler(t *testing.T) {
	handler := newRequestTimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("done"))
	}), time.Second, testLogger())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/fast", nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if recorder.Header().Get("X-Test") != "ok" {
		t.Fatalf("response header not copied: %v", recorder.Header())
	}
	if recorder.Body.String() != "done" {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), "done")
	}
}

func TestRequestTimeoutReturnsGatewayTimeout(t *testing.T) {
	handler := newRequestTimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		// 让超时分支先返回，避免 done 与 ctx.Done 同时就绪导致断言偶发失败。
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}), 20*time.Millisecond, testLogger())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}

	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if envelope.Code != 50400 || envelope.Message != "请求处理超时" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestRequestTimeoutPropagatesCancellation(t *testing.T) {
	deadline := make(chan error, 1)
	handler := newRequestTimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		deadline <- r.Context().Err()
	}), 20*time.Millisecond, testLogger())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))

	select {
	case err := <-deadline:
		if err == nil {
			t.Fatal("request context was not cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not observe cancellation")
	}
}

func TestRequestTimeoutDisabled(t *testing.T) {
	handler := newRequestTimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), 0, testLogger())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}
