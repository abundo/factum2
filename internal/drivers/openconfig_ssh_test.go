package drivers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitIdleAlreadyQuiet(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now().Add(-idleWindow)}
	if err := s.waitIdle(context.Background(), nil); err != nil {
		t.Fatalf("waitIdle: %v", err)
	}
}

func TestWaitIdleCancel(t *testing.T) {
	s := &sshShellSession{lastActivity: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := s.waitIdle(ctx, nil)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("waitIdle: got %v, want errWaitTimeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waitIdle on cancelled ctx took %v, want immediate return", time.Since(start))
	}
}
