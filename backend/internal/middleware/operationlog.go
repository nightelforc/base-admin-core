package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/internal/repository"
)

// audit redaction: any request body field whose name contains one of these
// fragments is stored as "***" so credentials never reach the audit table.
var sensitiveKeys = []string{"password", "secret", "token", "authorization", "credential"}

// OperationLogger records mutating API calls asynchronously. A bounded queue
// keeps a slow database from blocking request handling; entries are dropped and
// reported when the queue is full.
type OperationLogger struct {
	logs    *repository.LogRepository
	log     *slog.Logger
	enabled bool
	limit   int
	queue   chan *model.OperationLog
	done    chan struct{}
}

// NewOperationLogger starts the background writer.
func NewOperationLogger(logs *repository.LogRepository, log *slog.Logger, enabled bool, bodyLimit int) *OperationLogger {
	logger := &OperationLogger{
		logs:    logs,
		log:     log,
		enabled: enabled,
		limit:   bodyLimit,
		queue:   make(chan *model.OperationLog, 256),
		done:    make(chan struct{}),
	}
	if enabled {
		go logger.consume()
	}
	return logger
}

// Close stops accepting entries and waits for the queue to drain.
func (o *OperationLogger) Close() {
	if !o.enabled {
		return
	}
	close(o.queue)
	<-o.done
}

// Middleware records method, path, status, latency and a redacted body.
func (o *OperationLogger) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !o.enabled || !isAuditable(c.Request.Method) {
			c.Next()
			return
		}

		body := o.readAndRestoreBody(c)
		started := time.Now()
		c.Next()

		entry := &model.OperationLog{
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			IP:          c.ClientIP(),
			UserAgent:   truncate(c.Request.UserAgent(), 512),
			StatusCode:  c.Writer.Status(),
			LatencyMS:   time.Since(started).Milliseconds(),
			RequestBody: redactJSON(body),
			CreatedAt:   time.Now().UTC(),
		}
		if principal, ok := CurrentPrincipal(c); ok {
			entry.UserID = principal.UserID
			entry.Username = principal.Username
		}

		select {
		case o.queue <- entry:
		default:
			o.log.Warn("operation log queue is full, entry dropped", "method", entry.Method, "path", entry.Path)
		}
	}
}

func (o *OperationLogger) consume() {
	defer close(o.done)
	for entry := range o.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := o.logs.CreateOperation(ctx, entry); err != nil {
			o.log.Error("write operation log", "error", err, "path", entry.Path)
		}
		cancel()
	}
}

func (o *OperationLogger) readAndRestoreBody(c *gin.Context) []byte {
	if c.Request.Body == nil {
		return nil
	}

	// limit <= 0 表示不记录请求体，也不读取它，避免无谓地占用内存。
	if o.limit <= 0 {
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(o.limit)))
	// 无论读取是否完整，都要把已读部分还给处理器。
	c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), c.Request.Body))
	if err != nil {
		return nil
	}
	return raw
}

func isAuditable(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// redactJSON blanks sensitive fields recursively. Truncated or non-JSON bodies
// are not stored verbatim: a cut-off JSON fragment can no longer be redacted and
// would otherwise leak passwords or tokens into the audit table.
func redactJSON(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "[非 JSON 或已截断的请求体，已省略]"
	}

	sanitized, err := json.Marshal(redactValue(payload))
	if err != nil {
		return ""
	}
	return truncate(string(sanitized), 4096)
}

// redactValue 递归处理对象与数组，避免嵌套结构里的 password/token 逃过脱敏。
func redactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if isSensitiveKey(key) {
				v[key] = "***"
				continue
			}
			v[key] = redactValue(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = redactValue(item)
		}
		return v
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range sensitiveKeys {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
