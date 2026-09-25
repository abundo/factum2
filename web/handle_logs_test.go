package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"
)

func TestApiLogsWebSocketReplaysHistoryAsOneFrame(t *testing.T) {
	hub := NewLogHub()
	hub.Publish(LogEvent{Time: time.Unix(1, 0).UTC(), Level: "INFO", Message: "one", Source: "web"})
	hub.Publish(LogEvent{Time: time.Unix(2, 0).UTC(), Level: "INFO", Message: "two", Source: "web"})

	e := echo.New()
	ctrl := &Controller{LogHub: hub}
	e.GET("/api/logs/ws", ctrl.ApiLogsWebSocket)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/logs/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var frame logHistoryFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if frame.Type != logFrameHistory {
		t.Fatalf("type = %q, want %q", frame.Type, logFrameHistory)
	}
	if len(frame.Events) != 2 || frame.Events[0].Message != "one" || frame.Events[1].Message != "two" {
		t.Fatalf("events = %+v", frame.Events)
	}

	hub.Publish(LogEvent{Time: time.Unix(3, 0).UTC(), Level: "INFO", Message: "live", Source: "web"})
	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	var live LogEvent
	if err := json.Unmarshal(payload, &live); err != nil {
		t.Fatalf("unmarshal live: %v", err)
	}
	if live.Message != "live" || live.Level != "INFO" {
		t.Fatalf("live = %+v", live)
	}
}
