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

// GormLogger 把 GORM 的日志接口适配到 slog，使数据库诊断与应用日志管线
// （及其级别/格式配置）共用同一条通路。
//
// 当上下文携带请求级日志器时，记录经它写出，因此 SQL 语句会继承触发它的
// HTTP 请求上的 request_id（以及任何其他属性）。
type GormLogger struct {
	log           *slog.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

var _ gormlogger.Interface = (*GormLogger)(nil)

// NewGormLogger 构建一个经 log 写出的 GORM 日志器。
// level 取 silent、error、warn 或 info 之一。
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

// LogMode 实现 gormlogger.Interface。
func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

// logFor 返回单条记录使用的日志器：ctx 携带请求级日志器时用它，否则用进程日志器。
// component 属性使数据库记录与请求自身的日志行可以区分。
func (l *GormLogger) logFor(ctx context.Context) *slog.Logger {
	if scoped, ok := fromContext(ctx); ok {
		return scoped.With("component", "gorm")
	}
	return l.log
}

// Info 实现 gormlogger.Interface。
func (l *GormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.logFor(ctx).InfoContext(ctx, expand(msg, args...))
	}
}

// Warn 实现 gormlogger.Interface。
func (l *GormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.logFor(ctx).WarnContext(ctx, expand(msg, args...))
	}
}

// Error 实现 gormlogger.Interface。
func (l *GormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.logFor(ctx).ErrorContext(ctx, expand(msg, args...))
	}
}

// Trace 实现 gormlogger.Interface。记录未找到属于正常控制流，绝不记为错误；
// 慢语句记为警告。
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

// expand 渲染 GORM 的 printf 风格消息。若干 GORM 调用点会传入带参数的格式串，
// 丢掉参数就会把裸 %v 写进日志。
func expand(msg string, args ...any) string {
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
