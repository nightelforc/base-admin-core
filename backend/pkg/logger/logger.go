// Package logger 构建进程级结构化日志器，并通过 context.Context 传递请求级日志器。
//
// 它刻意不了解应用的配置类型：调用方把自己的设置映射到 Options，从而使本包
// 可被其他项目导入。
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

type contextKey struct{}

// Options 描述根日志器。
type Options struct {
	// Level 取 debug、info、warn、error 之一。
	Level string
	// Format 为 text（默认）或 json。
	Format string
	// AddSource 为每条记录标注调用点的 file:line。
	AddSource bool
}

// New 根据 opts 构建根日志器，将其设为 slog 默认日志器并返回。
func New(opts Options) (*slog.Logger, error) {
	handler, err := newHandler(opts, os.Stdout)
	if err != nil {
		return nil, err
	}

	log := slog.New(handler)
	slog.SetDefault(log)
	return log, nil
}

// newHandler 是 New 中可测试的那一半：它按 opts 构建一个写入 w 的处理器。
func newHandler(opts Options, w io.Writer) (slog.Handler, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	jsonFormat := opts.Format == "json"
	handlerOpts := &slog.HandlerOptions{
		AddSource:   opts.AddSource,
		Level:       level,
		ReplaceAttr: replaceAttr(jsonFormat),
	}
	if jsonFormat {
		return slog.NewJSONHandler(w, handlerOpts), nil
	}
	return slog.NewTextHandler(w, handlerOpts), nil
}

// replaceAttr 在编码前规范化内置属性：级别名转为小写，文本记录携带与 JSON
// 处理器默认写出相同的 RFC 3339 时间戳。
//
// JSON 处理器已经用 time.RFC3339Nano 编码 time.Time，且不额外分配，因此在那里
// 重写时间戳只会给每条记录多一次格式化与一次分配，输出却逐字节相同。
func replaceAttr(jsonFormat bool) func(groups []string, attr slog.Attr) slog.Attr {
	return func(groups []string, attr slog.Attr) slog.Attr {
		if len(groups) > 0 {
			return attr
		}
		switch attr.Key {
		case slog.TimeKey:
			if jsonFormat {
				return attr
			}
			if attr.Value.Kind() == slog.KindTime {
				attr.Value = slog.StringValue(attr.Value.Time().Format(time.RFC3339Nano))
			}
		case slog.LevelKey:
			if lvl, ok := attr.Value.Any().(slog.Level); ok {
				attr.Value = slog.StringValue(levelName(lvl))
			}
		}
		return attr
	}
}

// levelName 返回 lvl 的小写名称。两种标准处理器都输出大写名称；具名级别映射到
// 常量，使常见情况不发生分配，介于其间的级别则保留后缀（warn+2）。
func levelName(lvl slog.Level) string {
	switch lvl {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return strings.ToLower(lvl.String())
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", level)
	}
}

// IntoContext 把请求级日志器存入上下文。希望记录带上请求属性的层（例如 GORM）
// 用 From 解析它，而不是在构造时捕获一个日志器。
func IntoContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// From 返回请求级日志器；上下文未携带时返回默认日志器。
func From(ctx context.Context) *slog.Logger {
	if log, ok := fromContext(ctx); ok {
		return log
	}
	return slog.Default()
}

// fromContext 报告 ctx 是否携带请求级日志器。已经持有进程日志器的调用方用第二个
// 返回值在它与请求级日志器之间做选择，避免每条记录都付出一次 With 的代价。
func fromContext(ctx context.Context) (*slog.Logger, bool) {
	if ctx == nil {
		return nil, false
	}
	log, ok := ctx.Value(contextKey{}).(*slog.Logger)
	return log, ok && log != nil
}
