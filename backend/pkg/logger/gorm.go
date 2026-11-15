package logger

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger adapts GORM's logging interface to slog so database diagnostics
// share the application log pipeline (and its level/format configuration).
type GormLogger struct {
	log           *slog.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

var _ gormlogger.Interface = (*GormLogger)(nil)

// NewGormLogger builds a GORM logger writing through log.
// The level is one of silent, error, warn or info.
func NewGormLogger(log *slog.Logger, level string, slowThreshold time.Duration) *GormLogger {
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

// Info implements gormlogger.Interface.
func (l *GormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.log.InfoContext(ctx, expand(msg, args...))
	}
}

// Warn implements gormlogger.Interface.
func (l *GormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.log.WarnContext(ctx, expand(msg, args...))
	}
}

// Error implements gormlogger.Interface.
func (l *GormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.log.ErrorContext(ctx, expand(msg, args...))
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
		l.log.ErrorContext(ctx, "database statement failed",
			"elapsed", elapsed.String(), "rows", rows, "sql", sql, "error", err.Error())
	case elapsed > l.slowThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.log.WarnContext(ctx, "slow database statement",
			"elapsed", elapsed.String(), "threshold", l.slowThreshold.String(), "rows", rows, "sql", sql)
	case l.level >= gormlogger.Info:
		sql, rows := fc()
		l.log.DebugContext(ctx, "database statement",
			"elapsed", elapsed.String(), "rows", rows, "sql", sql)
	}
}

func expand(msg string, args ...any) string {
	// gormlogger callers already formatted their arguments with fmt.Sprintf.
	_ = args
	return msg
}
