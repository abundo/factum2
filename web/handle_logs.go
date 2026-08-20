package web

import (
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
)

// logsUpgrader's CheckOrigin is left at gorilla's default (Origin header's
// host, if present, must match the request host) - the log window has no
// cross-site use case, so same-origin-only is the right default here.
var logsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

const (
	logsWriteWait  = 10 * time.Second
	logsPingPeriod = 30 * time.Second
)

// ApiLogsWebSocket streams live slog records (published via hubHandler in
// logstream.go) to the frontend's log window. Gated by RequireAdmin in
// web.go: log lines can carry details (hostnames, internal error text) not
// meant for a non-admin user.
func (ctrl *Controller) ApiLogsWebSocket(c *echo.Context) error {
	conn, err := logsUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, history, unsubscribe := ctrl.LogHub.Subscribe()
	defer unsubscribe()

	// The client never sends application data, but a read pump is still
	// required: without one we'd never observe a close frame or dropped
	// connection, and the write loop below would block forever.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	writeEvent := func(e LogEvent) error {
		_ = conn.SetWriteDeadline(time.Now().Add(logsWriteWait))
		return conn.WriteJSON(e)
	}

	for _, e := range history {
		if err := writeEvent(e); err != nil {
			return nil
		}
	}

	ticker := time.NewTicker(logsPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-closed:
			return nil
		case e, ok := <-ch:
			if !ok {
				return nil
			}
			if err := writeEvent(e); err != nil {
				return nil
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(logsWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return nil
			}
		}
	}
}
