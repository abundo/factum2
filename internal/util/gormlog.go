package util

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm/logger"
)

// gormSlowThreshold matches GORM's default. Queries under this are silent.
// Queries over it are slog.Debug, not a warning: a 38k-row interface load
// at ~230ms is expected and must not show up in the GUI log.
const gormSlowThreshold = 200 * time.Millisecond

// gormSlogLogger implements GORM's logger without writing to stdout.
// SQL errors stay at slog.Error. Slow SQL and per-statement traces stay at
// slog.Debug, so the default info level drops them before the worker can
// relay the line.
type gormSlogLogger struct {
	level                logger.LogLevel
	slow                 time.Duration
	ignoreRecordNotFound bool
}

func newGormLogger() logger.Interface {
	return &gormSlogLogger{
		level:                logger.Warn,
		slow:                 gormSlowThreshold,
		ignoreRecordNotFound: true,
	}
}

func (l *gormSlogLogger) LogMode(level logger.LogLevel) logger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

func (l *gormSlogLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level < logger.Info {
		return
	}
	slog.DebugContext(ctx, fmt.Sprintf(msg, args...))
}

func (l *gormSlogLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level < logger.Warn {
		return
	}
	slog.WarnContext(ctx, fmt.Sprintf(msg, args...))
}

func (l *gormSlogLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level < logger.Error {
		return
	}
	slog.ErrorContext(ctx, fmt.Sprintf(msg, args...))
}

func (l *gormSlogLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	switch {
	case err != nil && l.level >= logger.Error && (!errors.Is(err, logger.ErrRecordNotFound) || !l.ignoreRecordNotFound):
		sql, rows := fc()
		slog.ErrorContext(ctx, "sql", "err", err, "elapsed", elapsed, "rows", rows, "query", sql)
	case l.slow != 0 && elapsed > l.slow && l.level >= logger.Warn:
		sql, rows := fc()
		slog.DebugContext(ctx, "slow sql", "elapsed", elapsed, "rows", rows, "threshold", l.slow, "query", sql)
	case l.level >= logger.Info:
		sql, rows := fc()
		slog.DebugContext(ctx, "sql", "elapsed", elapsed, "rows", rows, "query", sql)
	}
}
