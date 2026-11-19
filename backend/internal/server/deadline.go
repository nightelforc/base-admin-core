package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/nightelforc/base-admin-core/backend/internal/middleware"
)

// newDeadlineHandler 给每个请求注入一个带截止时间的 context，除此之外不做任何
// 干预：不缓冲响应，也不替处理器写超时响应。
//
// 这是与业内一致的做法——截止时间只是「预算」，由下游自己兑现：数据库层靠
// statement_timeout 止损，Redis 靠连接级读写超时，处理器靠 context 取消立即
// 返回。谁都不该依赖最外层把整个响应缓存起来再统一裁决，那样长连接（SSE、
// WebSocket）与流式下载都无法工作。
//
// 超时后处理器仍会正常写响应（通常是一个 504 信封，见 pkg/response.Fail 对
// context.DeadlineExceeded 的映射）；如果处理器完全不看 context，客户端最终
// 由反向代理或 http.Server 的写超时断开，这属于实现缺陷而不是本层的责任。
//
// timeout <= 0 表示不注入截止时间。
func newDeadlineHandler(next http.Handler, timeout time.Duration, log *slog.Logger) http.Handler {
	if timeout <= 0 {
		return next
	}
	if log == nil {
		log = slog.Default()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		next.ServeHTTP(w, r.WithContext(ctx))

		// 处理器已经返回，此时 ctx.Err() 仍为 DeadlineExceeded 说明它跑过了
		// 预算，记一条日志便于定位慢接口；响应内容如何由处理器决定。
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"timeout", timeout,
			}
			// RequestIDMiddleware writes the id on the response, so this warning
			// can carry the same correlation id as the access line.
			if id := w.Header().Get(middleware.RequestIDHeader); id != "" {
				attrs = append(attrs, "request_id", id)
			}
			log.Warn("request exceeded deadline", attrs...)
		}
	})
}
