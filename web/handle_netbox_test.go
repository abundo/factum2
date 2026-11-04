package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
)

const webhookTestSecret = "webhook-test-secret"

func TestApiNetboxWebhook_DeleteDevice(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	device := seedNetboxDevice(t, db, "rtr1", 42)

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 42, "name": "rtr1"},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "deleted" {
		t.Errorf("status = %v, want deleted", body["status"])
	}
	if got := uintFromJSON(body["netbox_id"]); got != 42 {
		t.Errorf("netbox_id = %v, want 42", body["netbox_id"])
	}

	if err := db.First(&models.Device{}, device.ID).Error; err != gorm.ErrRecordNotFound {
		t.Fatalf("device still present: %v", err)
	}
}

func TestApiNetboxWebhook_DeleteDevice_UnknownID(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 99, "name": "ghost"},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestApiNetboxWebhook_DeleteDevice_LeavesNonNetboxSource(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)

	d := models.Device{Name: "manual", NetboxID: 42, CfSource: "manual"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 42, "name": "manual"},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := db.First(&models.Device{}, d.ID).Error; err != nil {
		t.Fatalf("non-netbox device was deleted: %v", err)
	}
}

func TestApiNetboxWebhook_DeleteDevice_MissingID(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	device := seedNetboxDevice(t, db, "rtr1", 42)

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"name": "rtr1"},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ignored" {
		t.Errorf("status = %v, want ignored", body["status"])
	}
	if err := db.First(&models.Device{}, device.ID).Error; err != nil {
		t.Fatalf("device was deleted despite missing id: %v", err)
	}
}

func TestApiNetboxWebhook_DeleteInterface_DoesNotDeleteDevice(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	device := seedNetboxDevice(t, db, "rtr1", 42)

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.interface",
		"data": map[string]any{
			"id":     100,
			"name":   "eth0",
			"device": map[string]any{"id": 42, "name": "rtr1"},
		},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if err := db.First(&models.Device{}, device.ID).Error; err != nil {
		t.Fatalf("interface-delete webhook removed the device: %v", err)
	}
}

func TestApiNetboxWebhook_DeleteCable(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	dev := seedNetboxDevice(t, db, "rtr1", 1)
	ifa := models.Interface{DeviceID: dev.ID, NetboxID: 11, Name: "eth0"}
	ifb := models.Interface{DeviceID: dev.ID, NetboxID: 22, Name: "eth1"}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}
	conn := models.Connection{NetboxID: 9, DeviceAID: dev.ID, InterfaceAID: ifa.ID, DeviceBID: dev.ID, InterfaceBID: ifb.ID}
	if err := db.Create(&conn).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.cable",
		"data":        map[string]any{"id": 9},
	})
	if err := (&Controller{DB: db}).ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := db.First(&models.Connection{}, conn.ID).Error; err != gorm.ErrRecordNotFound {
		t.Fatalf("connection still present: %v", err)
	}
}

func TestApiNetboxWebhook_DeleteSite(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	site := models.Site{NetboxID: 4, Name: "STO", Latitude: 59.3, Longitude: 18.0}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.site",
		"data":        map[string]any{"id": 4, "name": "STO"},
	})
	if err := (&Controller{DB: db}).ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := db.First(&models.Site{}, site.ID).Error; err != gorm.ErrRecordNotFound {
		t.Fatalf("site still present: %v", err)
	}
}

func TestApiNetboxWebhook_TenantIgnored(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "updated",
		"object_type": "tenancy.tenant",
		"data":        map[string]any{"id": 1, "name": "Acme"},
	})
	if err := (&Controller{DB: db}).ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ignored" {
		t.Errorf("status = %v, want ignored", body["status"])
	}
}

func TestApiNetboxWebhook_ContactIgnored(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "updated",
		"object_type": "tenancy.contact",
		"data":        map[string]any{"id": 1, "name": "Ada"},
	})
	if err := (&Controller{DB: db}).ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ignored" {
		t.Errorf("status = %v, want ignored", body["status"])
	}
}

func TestApiNetboxWebhook_InvalidSignature(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)

	c, rec := signedWebhookRequest(t, "wrong-secret", map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 42, "name": "rtr1"},
	})
	ctrl := &Controller{DB: db}
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("ApiNetboxWebhook: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestApiNetboxWebhook_QuietUntilDelta(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	hub := NewLogHub()
	prev := slog.Default()
	slog.SetDefault(slog.New(newHubHandler(slog.NewTextHandler(io.Discard, nil), hub)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctrl := &Controller{DB: db, LogHub: hub}
	ctrl.netboxDeviceSyncDebounce.delay = time.Hour
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	post := func() {
		t.Helper()
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.device",
			"data":        map[string]any{"id": 1, "name": "sw1"},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook: %v", err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	}
	post()
	post()

	_, history, unsub := hub.Subscribe()
	t.Cleanup(unsub)
	for _, e := range history {
		if e.Source == "netbox" {
			t.Fatalf("webhook logged before the quiet window: %+v", e)
		}
	}
}

func TestApiNetboxWebhook_NoChangesIsOneLine(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	hub := NewLogHub()
	prev := slog.Default()
	slog.SetDefault(slog.New(newHubHandler(slog.NewTextHandler(io.Discard, nil), hub)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	orig := netboxSyncDelta
	netboxSyncDelta = func(_ *gorm.DB, reporter jobevent.Reporter) error {
		reporter.Emit(jobevent.Info, "Netbox delta sync started")
		reporter.Emit(jobevent.Info, "Netbox delta sync: no changes")
		return nil
	}
	t.Cleanup(func() { netboxSyncDelta = orig })

	ctrl := &Controller{DB: db, LogHub: hub}
	ctrl.netboxDeviceSyncDebounce.delay = 20 * time.Millisecond
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	for _, name := range []string{"sw1", "sw2"} {
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.device",
			"data":        map[string]any{"id": 1, "name": name},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook: %v", err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, history, unsub := hub.Subscribe()
		unsub()
		var lines []LogEvent
		for _, e := range history {
			if e.Source == "netbox" {
				lines = append(lines, e)
			}
		}
		if len(lines) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if len(lines) != 1 {
			t.Fatalf("GUI log lines = %d, want 1: %+v", len(lines), lines)
		}
		got := lines[0]
		if got.Message != "Netbox webhook sync: device sw1, device sw2, no changes" {
			t.Fatalf("message = %q", got.Message)
		}
		if got.Level != "INFO" || got.Attrs["queued"] != "2" {
			t.Fatalf("line = %+v", got)
		}
		return
	}
	t.Fatalf("missing GUI log")
}

func TestApiNetboxWebhook_ChangeAndErrorStayInTheLog(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)

	orig := netboxSyncDelta
	t.Cleanup(func() { netboxSyncDelta = orig })

	run := func(t *testing.T, want int, sync func(*gorm.DB, jobevent.Reporter) error) []LogEvent {
		t.Helper()
		hub := NewLogHub()
		prev := slog.Default()
		slog.SetDefault(slog.New(newHubHandler(slog.NewTextHandler(io.Discard, nil), hub)))
		t.Cleanup(func() { slog.SetDefault(prev) })
		netboxSyncDelta = sync

		ctrl := &Controller{DB: db, LogHub: hub}
		ctrl.netboxDeviceSyncDebounce.delay = 15 * time.Millisecond
		t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.device",
			"data":        map[string]any{"id": 1, "name": "sw1"},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook: %v", err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}

		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			_, history, unsub := hub.Subscribe()
			unsub()
			var lines []LogEvent
			for _, e := range history {
				if e.Source == "netbox" {
					lines = append(lines, e)
				}
			}
			if len(lines) >= want {
				return lines
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("missing GUI log")
		return nil
	}

	t.Run("change", func(t *testing.T) {
		lines := run(t, 2, func(_ *gorm.DB, reporter jobevent.Reporter) error {
			reporter.Emit(jobevent.Info, "Netbox delta sync started")
			reporter.Emit(jobevent.Info, "Netbox delta sync: %d new, %d updated, %d deleted", 0, 1, 0)
			reporter.Emit(jobevent.Info, "Netbox delta sync: cursor %s", "2026-11-04T13:50:24Z")
			return nil
		})
		if len(lines) != 2 {
			t.Fatalf("lines = %+v", lines)
		}
		if lines[0].Message != "Netbox delta sync: 0 new, 1 updated, 0 deleted" {
			t.Fatalf("first = %q", lines[0].Message)
		}
		if lines[0].Attrs["device"] != "sw1" {
			t.Fatalf("device attr = %q", lines[0].Attrs["device"])
		}
		if lines[1].Message != "Netbox delta sync: cursor 2026-11-04T13:50:24Z" {
			t.Fatalf("second = %q", lines[1].Message)
		}
	})

	t.Run("error", func(t *testing.T) {
		lines := run(t, 1, func(_ *gorm.DB, reporter jobevent.Reporter) error {
			reporter.Emit(jobevent.Info, "Netbox delta sync started")
			err := fmt.Errorf("changelog down")
			reporter.EmitErr(err)
			return err
		})
		if len(lines) != 1 || lines[0].Message != "changelog down" || lines[0].Level != "ERROR" {
			t.Fatalf("lines = %+v", lines)
		}
	})
}

func TestWebhookKeyPhrase(t *testing.T) {
	cases := map[string]string{
		"al11-ce2":    "device al11-ce2",
		"cable:12":    "cable 12",
		"dcim.rack":   "dcim.rack",
		"dcim.site:5": "dcim.site 5",
	}
	for key, want := range cases {
		if got := webhookKeyPhrase(key); got != want {
			t.Errorf("webhookKeyPhrase(%q) = %q, want %q", key, got, want)
		}
	}
	keys := []string{"a", "b", "c", "d", "e", "f", "g"}
	got := webhookNoChangePhrase(keys)
	want := "device a, device b, device c, device d, device e, and 2 more"
	if got != want {
		t.Fatalf("phrase = %q, want %q", got, want)
	}
}

func TestNetboxWebhookDebouncer_CoalescesAcrossDevices(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 40 * time.Millisecond}
	t.Cleanup(d.stopAll)

	var mu sync.Mutex
	var batches [][]string
	run := func(names []string) {
		mu.Lock()
		batches = append(batches, append([]string(nil), names...))
		mu.Unlock()
	}
	if fresh := d.schedule("sw1", run); !fresh {
		t.Fatal("first sw1 event should be fresh")
	}
	if fresh := d.schedule("sw1", run); fresh {
		t.Fatal("second sw1 event should only reset the shared timer")
	}
	if fresh := d.schedule("sw2", run); !fresh {
		t.Fatal("sw2 should be fresh")
	}
	time.Sleep(20 * time.Millisecond)
	d.schedule("sw1", run) // resets the window for sw2 as well
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	early := len(batches)
	mu.Unlock()
	if early != 0 {
		t.Fatalf("fired %d batches before the shared quiet period", early)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(batches)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 {
		t.Fatalf("batches = %v, want 1", batches)
	}
	if len(batches[0]) != 2 || batches[0][0] != "sw1" || batches[0][1] != "sw2" {
		t.Fatalf("names = %v, want [sw1 sw2]", batches[0])
	}
}

func TestNetboxWebhookDebouncer_SecondBurstDoesNotOverlap(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 30 * time.Millisecond}
	t.Cleanup(d.stopAll)

	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var batches [][]string
	var overlap atomic.Bool
	var inFlight atomic.Int32
	run := func(names []string) {
		if inFlight.Add(1) != 1 {
			overlap.Store(true)
		}
		defer inFlight.Add(-1)
		mu.Lock()
		batches = append(batches, append([]string(nil), names...))
		n := len(batches)
		mu.Unlock()
		if n == 1 {
			close(started)
			<-release
		}
	}
	d.schedule("sw1", run)
	select {
	case <-started:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("first batch did not start")
	}
	d.schedule("sw2", run)
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	if len(batches) != 1 {
		t.Fatalf("second device started during the first batch: %v", batches)
	}
	mu.Unlock()
	close(release)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(batches)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if overlap.Load() {
		t.Fatal("batches overlapped")
	}
	if len(batches) != 2 || len(batches[0]) != 1 || batches[0][0] != "sw1" || len(batches[1]) != 1 || batches[1][0] != "sw2" {
		t.Fatalf("batches = %v, want [sw1] then [sw2]", batches)
	}
}

func TestNetboxWebhookDebouncer_Cancel(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 30 * time.Millisecond}
	t.Cleanup(d.stopAll)

	var mu sync.Mutex
	var batches [][]string
	run := func(names []string) {
		mu.Lock()
		batches = append(batches, append([]string(nil), names...))
		mu.Unlock()
	}
	d.schedule("sw1", run)
	d.schedule("sw2", run)
	d.cancel("sw1")
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0] != "sw2" {
		t.Fatalf("batches = %v, want [sw2]", batches)
	}
}

func TestNetboxWebhookDebouncer_CancelLastStopsTimer(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 30 * time.Millisecond}
	t.Cleanup(d.stopAll)

	var fired atomic.Int32
	d.schedule("sw1", func([]string) { fired.Add(1) })
	d.cancel("sw1")
	time.Sleep(80 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Fatalf("fired %d times after cancel, want 0", got)
	}
}

func TestApiNetboxWebhook_QueuesPerDevice(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	ctrl := &Controller{DB: db}
	ctrl.netboxDeviceSyncDebounce.delay = time.Hour
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	post := func(name string) {
		t.Helper()
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.interface",
			"data": map[string]any{
				"id":     1,
				"name":   "eth0",
				"device": map[string]any{"id": 1, "name": name},
			},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook %s: %v", name, err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook %s status = %d, want %d, body=%s", name, rec.Code, http.StatusAccepted, rec.Body.String())
		}
	}
	post("sw1")
	post("sw1")
	post("sw2")

	ctrl.netboxDeviceSyncDebounce.mu.Lock()
	n := len(ctrl.netboxDeviceSyncDebounce.pending)
	_, sw1 := ctrl.netboxDeviceSyncDebounce.pending["sw1"]
	_, sw2 := ctrl.netboxDeviceSyncDebounce.pending["sw2"]
	timer := ctrl.netboxDeviceSyncDebounce.timer != nil
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if n != 2 || !sw1 || !sw2 || !timer {
		t.Fatalf("pending=%d sw1=%v sw2=%v timer=%v, want both names on one timer", n, sw1, sw2, timer)
	}
}

func TestApiNetboxWebhook_DeleteDevice_CancelsPendingSync(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	seedNetboxDevice(t, db, "rtr1", 42)

	ctrl := &Controller{DB: db}
	ctrl.netboxDeviceSyncDebounce.delay = time.Hour
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "updated",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 42, "name": "rtr1"},
	})
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("update webhook: %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("update status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	ctrl.netboxDeviceSyncDebounce.mu.Lock()
	_, pending := ctrl.netboxDeviceSyncDebounce.pending["rtr1"]
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if !pending {
		t.Fatal("expected rtr1 in the shared quiet window after the update webhook")
	}

	c, rec = signedWebhookRequest(t, webhookTestSecret, map[string]any{
		"event":       "deleted",
		"object_type": "dcim.device",
		"data":        map[string]any{"id": 42, "name": "rtr1"},
	})
	if err := ctrl.ApiNetboxWebhook(c); err != nil {
		t.Fatalf("delete webhook: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	ctrl.netboxDeviceSyncDebounce.mu.Lock()
	_, pending = ctrl.netboxDeviceSyncDebounce.pending["rtr1"]
	timer := ctrl.netboxDeviceSyncDebounce.timer != nil
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if pending || timer {
		t.Fatal("delete left a pending device or timer")
	}
}

func TestApiNetboxWebhook_SyncsQueuedDevicesOneAtATime(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)

	var mu sync.Mutex
	var got int
	orig := netboxSyncDelta
	netboxSyncDelta = func(_ *gorm.DB, _ jobevent.Reporter) error {
		mu.Lock()
		got++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { netboxSyncDelta = orig })

	ctrl := &Controller{DB: db}
	ctrl.netboxDeviceSyncDebounce.delay = 30 * time.Millisecond
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	for _, name := range []string{"sw2", "sw1", "sw2"} {
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.device",
			"data":        map[string]any{"id": 1, "name": name},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook %s: %v", name, err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook %s status = %d, body=%s", name, rec.Code, rec.Body.String())
		}
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := got
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if got != 1 {
		t.Fatalf("delta calls = %d, want 1", got)
	}
}

func TestApiNetboxWebhook_FullSyncWhenManyDevicesChange(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	// Ten device webhooks in one quiet window are one delta sync. Whether
	// that delta refetches or falls back to a full sync is decided from
	// the changelog, not from the webhook names.

	var mu sync.Mutex
	var got int
	orig := netboxSyncDelta
	netboxSyncDelta = func(_ *gorm.DB, _ jobevent.Reporter) error {
		mu.Lock()
		got++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { netboxSyncDelta = orig })

	ctrl := &Controller{DB: db}
	ctrl.netboxDeviceSyncDebounce.delay = 30 * time.Millisecond
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	for i := 1; i <= 10; i++ {
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": "dcim.device",
			"data":        map[string]any{"id": i, "name": fmt.Sprintf("nb-%02d", i)},
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook %d: %v", i, err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook %d status = %d, body=%s", i, rec.Code, rec.Body.String())
		}
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := got
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if got != 1 {
		t.Fatalf("delta calls = %d, want 1", got)
	}
}

func TestApiNetboxWebhook_QueuesVirtualMachine(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	ctrl := &Controller{DB: db}
	ctrl.netboxDeviceSyncDebounce.delay = time.Hour
	t.Cleanup(ctrl.netboxDeviceSyncDebounce.stopAll)

	post := func(objectType string, data map[string]any) {
		t.Helper()
		c, rec := signedWebhookRequest(t, webhookTestSecret, map[string]any{
			"event":       "updated",
			"object_type": objectType,
			"data":        data,
		})
		if err := ctrl.ApiNetboxWebhook(c); err != nil {
			t.Fatalf("webhook %s: %v", objectType, err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook %s status = %d, want %d, body=%s", objectType, rec.Code, http.StatusAccepted, rec.Body.String())
		}
	}
	post("virtualization.vminterface", map[string]any{
		"id":              9,
		"name":            "eth0",
		"virtual_machine": map[string]any{"id": 4, "name": "vm1"},
	})
	post("ipam.ipaddress", map[string]any{
		"id":      15,
		"address": "10.0.0.8/24",
		"assigned_object": map[string]any{
			"id":              9,
			"name":            "eth0",
			"virtual_machine": map[string]any{"id": 4, "name": "vm1"},
		},
	})

	ctrl.netboxDeviceSyncDebounce.mu.Lock()
	_, queued := ctrl.netboxDeviceSyncDebounce.pending["vm1"]
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if !queued {
		t.Fatal("vm1 was not queued")
	}
}

func TestNetboxWebhookDeviceName(t *testing.T) {
	vm := map[string]any{"id": float64(4), "name": "vm1"}
	if got := netboxWebhookDeviceName("virtualization.virtualmachine", map[string]any{"name": "vm1"}); got != "vm1" {
		t.Fatalf("vm name = %q", got)
	}
	if got := netboxWebhookDeviceName("virtualization.vminterface", map[string]any{"virtual_machine": vm}); got != "vm1" {
		t.Fatalf("vminterface name = %q", got)
	}
	if got := netboxWebhookDeviceName("ipam.ipaddress", map[string]any{
		"assigned_object": map[string]any{"virtual_machine": vm},
	}); got != "vm1" {
		t.Fatalf("address on vminterface name = %q", got)
	}
	if got := netboxWebhookDeviceName("ipam.ipaddress", map[string]any{
		"assigned_object": map[string]any{"device": map[string]any{"name": "sw1"}},
	}); got != "sw1" {
		t.Fatalf("address on interface name = %q", got)
	}
}

func TestNetboxWebhookObjectID(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		id   uint
		ok   bool
	}{
		{name: "integer", data: map[string]any{"id": float64(42)}, id: 42, ok: true},
		{name: "missing", data: map[string]any{"name": "rtr1"}, ok: false},
		{name: "zero", data: map[string]any{"id": float64(0)}, ok: false},
		{name: "negative", data: map[string]any{"id": float64(-1)}, ok: false},
		{name: "fraction", data: map[string]any{"id": 1.5}, ok: false},
		{name: "string", data: map[string]any{"id": "42"}, ok: false},
		{name: "nil map", data: nil, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := netboxWebhookObjectID(tc.data)
			if ok != tc.ok || got != tc.id {
				t.Errorf("netboxWebhookObjectID(%v) = %d, %v; want %d, %v", tc.data, got, ok, tc.id, tc.ok)
			}
		})
	}
}

func seedWebhookSecret(t *testing.T, db *gorm.DB, secret string) {
	t.Helper()
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	settings.NetboxWebhookSecret = secret
	if err := db.Save(settings).Error; err != nil {
		t.Fatalf("save settings: %v", err)
	}
}

func seedNetboxDevice(t *testing.T, db *gorm.DB, name string, netboxID uint) models.Device {
	t.Helper()
	d := models.Device{Name: name, NetboxID: netboxID, CfSource: "netbox"}
	if err := db.Create(&d).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	return d
}

func signedWebhookRequest(t *testing.T, secret string, payload any) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	body := buf.Bytes()
	req := httptest.NewRequest(http.MethodPost, "/api/netbox-webhook", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	req.Header.Set("X-Hook-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

func uintFromJSON(v any) uint {
	n, ok := v.(float64)
	if !ok {
		return 0
	}
	return uint(n)
}
