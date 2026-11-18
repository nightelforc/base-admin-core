package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// timeoutBody 与 API 其余错误保持同一信封结构；50400 表示网关/处理超时。
const timeoutBody = `{"code":50400,"message":"请求处理超时"}`

// newRequestTimeoutHandler 给每个请求加处理时限。超时后立即返回 504，并取消
// 请求上下文，数据库、Redis 等下游调用会随之中断，避免慢请求长期占用连接与
// goroutine。timeout <= 0 表示关闭该限制。
//
// 响应先写入内存缓冲区，只有处理完成才发送给客户端；这样超时响应不会与处理器
// 后写的响应混在一起。
func newRequestTimeoutHandler(next http.Handler, timeout time.Duration, log *slog.Logger) http.Handler {
	if timeout <= 0 {
		return next
	}
	return &requestTimeoutHandler{next: next, timeout: timeout, log: log}
}

type requestTimeoutHandler struct {
	next    http.Handler
	timeout time.Duration
	log     *slog.Logger
}

func (h *requestTimeoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	r = r.WithContext(ctx)
	tw := &timeoutWriter{header: make(http.Header)}
	done := make(chan struct{})
	panicCh := make(chan any, 1)

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicCh <- recovered
			}
		}()
		h.next.ServeHTTP(tw, r)
		close(done)
	}()

	select {
	case recovered := <-panicCh:
		// 交给 net/http 与 gin 的 Recovery 处理，保持原有 panic 语义。
		panic(recovered)
	case <-done:
		tw.mu.Lock()
		defer tw.mu.Unlock()
		copyHeader(w.Header(), tw.header)
		status := tw.code
		if !tw.wroteHeader {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(tw.body.Bytes())
	case <-ctx.Done():
		// 客户端主动断开时父 context 先取消，此时没有再写 504 的必要。
		if ctx.Err() == context.Canceled {
			tw.mu.Lock()
			tw.err = context.Canceled
			tw.mu.Unlock()
			h.log.Debug("request canceled",
				"method", r.Method,
				"path", r.URL.Path,
			)
			return
		}

		tw.mu.Lock()
		tw.err = http.ErrHandlerTimeout
		tw.mu.Unlock()

		h.log.Warn("request timed out",
			"method", r.Method,
			"path", r.URL.Path,
			"timeout", h.timeout,
		)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(timeoutBody))
	}
}

// timeoutWriter 缓存处理器的响应，并阻止超时之后继续写入。
type timeoutWriter struct {
	mu          sync.Mutex
	header      http.Header
	body        bytes.Buffer
	code        int
	wroteHeader bool
	err         error
}

func (w *timeoutWriter) Header() http.Header { return w.header }

func (w *timeoutWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeaderLocked(code)
}

func (w *timeoutWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if !w.wroteHeader {
		w.writeHeaderLocked(http.StatusOK)
	}
	return w.body.Write(p)
}

func (w *timeoutWriter) writeHeaderLocked(code int) {
	if w.err != nil || w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.code = code
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
