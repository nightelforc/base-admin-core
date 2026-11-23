package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/logger"
)

// AccessLog 安装请求级 logger，并为每个请求输出一行结构化日志，探针路径不记。
//
// 请求级 logger 带 request_id 并存入 request context，下游各层（handler、service、
// GORM）通过 logger.From 拿到同一个 logger，其记录因而能与这行访问日志按
// request_id 关联。
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		scoped := log.With("request_id", RequestID(c))
		c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), scoped))

		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}

		started := time.Now()
		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
		}
		// 只记参数名、不记取值：列表接口的 keyword 等过滤条件里会出现邮箱、手机号。
		if keys := queryKeys(c.Request.URL.RawQuery); keys != "" {
			attrs = append(attrs, "query_keys", keys)
		}
		attrs = append(attrs,
			"status", c.Writer.Status(),
			"bytes", c.Writer.Size(),
			"latency", time.Since(started).String(),
			"ip", c.ClientIP(),
		)
		if principal, ok := CurrentPrincipal(c); ok {
			attrs = append(attrs, "user_id", principal.UserID, "username", principal.Username)
		}
		if errors := c.Errors.ByType(gin.ErrorTypePrivate).String(); errors != "" {
			attrs = append(attrs, "errors", errors)
		}

		switch status := c.Writer.Status(); {
		case status >= http.StatusInternalServerError:
			scoped.Error("http request", attrs...)
		case status >= http.StatusBadRequest:
			scoped.Warn("http request", attrs...)
		default:
			scoped.Info("http request", attrs...)
		}
	}
}

const (
	// queryKeysLoggedLimit 限制一行访问日志最多记多少个参数名。
	queryKeysLoggedLimit = 8
	// queryKeyBytesLimit 限制单个参数名最多保留多少字节。
	queryKeyBytesLimit = 32
)

// queryKeys 把原始查询串还原成「这次请求用了哪些参数」。
//
// 访问日志曾经直接记录原始查询串，而列表接口的 keyword 会在 username、nickname、
// email、phone 上做模糊匹配，调用方经常把手机号、邮箱填进去，等于把个人数据写进
// 保留期远长于请求本身的日志汇。参数名仍能回答排障真正关心的问题——这次请求带了
// 哪些过滤条件；取值没有增量信息，只会泄漏。
//
// 参数名同样是客户端输入，所以这里去重、排序并设上限：最多 queryKeysLoggedLimit
// 个名字、每个 queryKeyBytesLimit 字节，避免一条恶意请求把日志行撑大。
func queryKeys(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil && len(values) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(values))
	for key := range values {
		if len(key) > queryKeyBytesLimit {
			key = strings.ToValidUTF8(key[:queryKeyBytesLimit], "")
		}
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > queryKeysLoggedLimit {
		keys = keys[:queryKeysLoggedLimit]
	}
	return strings.Join(keys, ",")
}
