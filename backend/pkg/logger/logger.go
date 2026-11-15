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
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	handlerOpts := &slog.HandlerOptions{
		AddSource: opts.AddSource,
		Level:     level,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return attr
			}
			switch attr.Key {
			case slog.TimeKey:
				if t, ok := attr.Value.Any().(time.Time); ok {
					attr.Value = slog.StringValue(t.Format(time.RFC3339Nano))
				}
			case slog.LevelKey:
				if lvl, ok := attr.Value.Any().(slog.Level); ok {
					attr.Value = slog.StringValue(strings.ToLower(lvl.String()))
				}
			}
			return attr
		},
	}

	var handler slog.Handler
	if opts.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	log := slog.New(handler)
	slog.SetDefault(log)
	return log, nil
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
