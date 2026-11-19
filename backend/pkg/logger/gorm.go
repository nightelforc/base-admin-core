package logger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger adapts GORM's logging interface to slog so database diagnostics
// share the application log pipeline (and its level/format configuration).
//
// Records are written through the request-scoped logger when the context
// carries one, so SQL statements inherit request_id (and any other attribute)
// from the HTTP request that triggered them.
type GormLogger struct {
	log           *slog.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

var _ gormlogger.Interface = (*GormLogger)(nil)

// NewGormLogger builds a GORM logger writing through log.
// The level is one of silent, error, warn or info.
func NewGormLogger(log *slog.Logger, level string, slowThreshold time.Duration) *GormLogger {
	if log == nil {
		log = slog.Default()
	}
	var lvl gormlogger.LogLevel
	switch level {
	case "silent":
		lvl = gormlogger.Silent
	case "error":
		lvl = gormlogger.Error
	case "info":
		lvl = gormlogger.Info
	default:
		lvl = gormlogger.Warn
	}
	if slowThreshold <= 0 {
		slowThreshold = 200 * time.Millisecond
	}
	return &GormLogger{log: log.With("component", "gorm"), level: lvl, slowThreshold: slowThreshold}
}

// LogMode implements gormlogger.Interface.
func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

// logFor returns the logger for one record: the request-scoped logger when ctx
// carries one, otherwise the process logger. The component attribute keeps
// database records distinguishable from the request's own lines.
func (l *GormLogger) logFor(ctx context.Context) *slog.Logger {
	if scoped, ok := fromContext(ctx); ok {
		return scoped.With("component", "gorm")
	}
	return l.log
}

// Info implements gormlogger.Interface.
func (l *GormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.logFor(ctx).InfoContext(ctx, expand(msg, args...))
	}
}

// Warn implements gormlogger.Interface.
func (l *GormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.logFor(ctx).WarnContext(ctx, expand(msg, args...))
	}
}

// Error implements gormlogger.Interface.
func (l *GormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.logFor(ctx).ErrorContext(ctx, expand(msg, args...))
	}
}

// Trace implements gormlogger.Interface. Record-not-found is normal control
// flow and never logged as an error; slow statements are logged as warnings.
func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)

	switch {
	case err != nil && l.level >= gormlogger.Error && !errors.Is(err, gorm.ErrRecordNotFound):
		sql, rows := fc()
		l.logFor(ctx).ErrorContext(ctx, "database statement failed",
			"elapsed", elapsed.String(), "rows", rows, "sql", sql, "error", err.Error())
	case elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.logFor(ctx).WarnContext(ctx, "slow database statement",
			"elapsed", elapsed.String(), "threshold", l.slowThreshold.String(), "rows", rows, "sql", sql)
	case l.level >= gormlogger.Info:
		sql, rows := fc()
		l.logFor(ctx).DebugContext(ctx, "database statement",
			"elapsed", elapsed.String(), "rows", rows, "sql", sql)
	}
}

// expand renders GORM's printf-style message. Several GORM call sites pass
// format strings with arguments, so dropping the arguments would log a bare %v.
func expand(msg string, args ...any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
