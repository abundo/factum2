package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
