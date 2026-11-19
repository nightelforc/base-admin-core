// Package logger builds the process-wide structured logger and carries
// request-scoped loggers through context.Context.
//
// It deliberately knows nothing about the application's configuration types:
// callers map their own settings onto Options, which keeps the package
// importable from other projects.
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

// Options describes the root logger.
type Options struct {
	// Level is one of debug, info, warn, error.
	Level string
	// Format is text (default) or json.
	Format string
	// AddSource annotates every record with the file:line of the call site.
	AddSource bool
}

// New builds the root logger from opts, installs it as the slog default and
// returns it.
func New(opts Options) (*slog.Logger, error) {
	handler, err := newHandler(opts, os.Stdout)
	if err != nil {
		return nil, err
	}

	log := slog.New(handler)
	slog.SetDefault(log)
	return log, nil
}

// newHandler is the testable half of New: it builds a handler for opts that
// writes to w.
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

// replaceAttr normalises the built-in attributes before they are encoded:
// level names are lowercased, and text records carry the same RFC 3339
// timestamps the JSON handler writes by default.
//
// The JSON handler already encodes time.Time with time.RFC3339Nano and does so
// without allocating, so rewriting the timestamp there would add a formatting
// pass and an allocation to every record for byte-identical output.
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

// levelName returns the lowercase name of lvl. Both standard handlers emit
// upper case names; named levels are mapped to constants so the common case
// does not allocate, while levels in between keep their suffix (warn+2).
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

// IntoContext stores a request-scoped logger in the context.
func IntoContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// From returns the request-scoped logger, or the default logger when the
// context carries none.
func From(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}
