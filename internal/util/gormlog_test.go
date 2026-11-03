package util

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func withSlog(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestGormLoggerSlowSQLIsDebugOnly(t *testing.T) {
	buf := withSlog(t, slog.LevelInfo)
	l := newGormLogger()
	l.Trace(context.Background(), time.Now().Add(-300*time.Millisecond), func() (string, int64) {
		return `SELECT "id" FROM "interfaces"`, 37926
	}, nil)
	if buf.Len() != 0 {
		t.Fatalf("slow sql logged at info:\n%s", buf.String())
	}

	buf.Reset()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	l.Trace(context.Background(), time.Now().Add(-300*time.Millisecond), func() (string, int64) {
		return `SELECT "id" FROM "interfaces"`, 37926
	}, nil)
	got := buf.String()
	if !strings.Contains(got, "slow sql") || !strings.Contains(got, "interfaces") || !strings.Contains(got, "level=DEBUG") {
		t.Fatalf("debug log:\n%s", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("slow sql still has ANSI codes:\n%s", got)
	}
}

func TestGormLoggerSQLErrorStaysVisible(t *testing.T) {
	buf := withSlog(t, slog.LevelInfo)
	l := newGormLogger()
	l.Trace(context.Background(), time.Now(), func() (string, int64) {
		return `SELECT 1`, 0
	}, errors.New("connection refused"))
	got := buf.String()
	if !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "connection refused") {
		t.Fatalf("sql error:\n%s", got)
	}
}

func TestGormLoggerIgnoresRecordNotFound(t *testing.T) {
	buf := withSlog(t, slog.LevelDebug)
	l := newGormLogger()
	l.Trace(context.Background(), time.Now(), func() (string, int64) {
		return `SELECT * FROM "settings"`, 0
	}, logger.ErrRecordNotFound)
	if buf.Len() != 0 {
		t.Fatalf("record not found logged:\n%s", buf.String())
	}
}

func TestGormLoggerSilentDropsSlowSQL(t *testing.T) {
	buf := withSlog(t, slog.LevelDebug)
	l := newGormLogger().LogMode(logger.Silent)
	l.Trace(context.Background(), time.Now().Add(-time.Second), func() (string, int64) {
		return `SELECT 1`, 1
	}, nil)
	if buf.Len() != 0 {
		t.Fatalf("silent logger wrote:\n%s", buf.String())
	}
}
