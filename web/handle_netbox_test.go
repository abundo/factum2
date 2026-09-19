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

type captureReporter struct {
	lines []string
}

func (r *captureReporter) Emit(_ jobevent.Level, format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *captureReporter) EmitErr(err error) {
	if err != nil {
		r.lines = append(r.lines, err.Error())
	}
}

func TestWebhookReporter_IncludesDeviceNameOnStartAndSummary(t *testing.T) {
	var got captureReporter
	r := webhookReporter{next: &got, deviceName: "sw1"}
	r.Emit(jobevent.Info, "Netbox sync started")
	r.Emit(jobevent.Info, "Netbox sync: %d new, %d updated, %d deleted", 0, 1, 0)
	if len(got.lines) != 2 {
		t.Fatalf("lines = %#v, want 2", got.lines)
	}
	if got.lines[0] != "Netbox sync: device sw1 started" {
		t.Errorf("started = %q", got.lines[0])
	}
	if got.lines[1] != "Netbox sync: device sw1: 0 new, 1 updated, 0 deleted" {
		t.Errorf("summary = %q", got.lines[1])
	}
}

func TestApiNetboxWebhook_LogsQueuedToHub(t *testing.T) {
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
	var queued int
	for _, e := range history {
		if e.Message == "Netbox webhook sync: device sw1 queued" && e.Source == "netbox" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("queued log count = %d, want 1 (one line per burst), history=%v", queued, history)
	}
}

func TestApiNetboxWebhook_LogsSyncStartAndSummaryToHub(t *testing.T) {
	db := newTestDB(t)
	seedWebhookSecret(t, db, webhookTestSecret)
	hub := NewLogHub()
	prev := slog.Default()
	slog.SetDefault(slog.New(newHubHandler(slog.NewTextHandler(io.Discard, nil), hub)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	orig := netboxSyncDB
	netboxSyncDB = func(_ *gorm.DB, name string, reporter jobevent.Reporter) error {
		reporter.Emit(jobevent.Info, "Netbox sync started")
		reporter.Emit(jobevent.Info, "Netbox sync: %d new, %d updated, %d deleted", 0, 1, 0)
		if name != "sw1" {
			t.Errorf("synced %q, want sw1", name)
		}
		return nil
	}
	t.Cleanup(func() { netboxSyncDB = orig })

	ctrl := &Controller{DB: db, LogHub: hub}
	ctrl.netboxDeviceSyncDebounce.delay = 20 * time.Millisecond
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
	var started, summary bool
	for time.Now().Before(deadline) {
		_, history, unsub := hub.Subscribe()
		unsub()
		started, summary = false, false
		for _, e := range history {
			if e.Source != "netbox" {
				continue
			}
			if e.Message == "Netbox sync: device sw1 started" {
				started = true
			}
			if e.Message == "Netbox sync: device sw1: 0 new, 1 updated, 0 deleted" {
				summary = true
			}
		}
		if started && summary {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing GUI logs: started=%v summary=%v", started, summary)
}

func TestNetboxWebhookDebouncer_CoalescesPerDevice(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 40 * time.Millisecond}
	t.Cleanup(d.stopAll)

	var a, b atomic.Int32
	d.schedule("sw1", func() { a.Add(1) })
	d.schedule("sw1", func() { a.Add(1) })
	d.schedule("sw2", func() { b.Add(1) })
	time.Sleep(20 * time.Millisecond)
	d.schedule("sw1", func() { a.Add(1) })

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if a.Load() == 1 && b.Load() == 1 {
			time.Sleep(60 * time.Millisecond)
			if a.Load() != 1 || b.Load() != 1 {
				t.Fatalf("extra fires: sw1=%d sw2=%d", a.Load(), b.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: sw1=%d sw2=%d, want 1 each", a.Load(), b.Load())
}

func TestNetboxWebhookDebouncer_Cancel(t *testing.T) {
	d := netboxWebhookDebouncer{delay: 30 * time.Millisecond}
	t.Cleanup(d.stopAll)

	var fired atomic.Int32
	d.schedule("sw1", func() { fired.Add(1) })
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
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if n != 2 || !sw1 || !sw2 {
		t.Fatalf("pending=%d sw1=%v sw2=%v, want one timer each", n, sw1, sw2)
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
		t.Fatal("expected a pending debounce after the update webhook")
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
	ctrl.netboxDeviceSyncDebounce.mu.Unlock()
	if pending {
		t.Fatal("delete left a pending debounce")
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
