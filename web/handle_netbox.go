package web

import (
	"crypto/hmac"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// netboxWebhookDebounce is how long a device must go without another
// Device / Interface / IP webhook before the queued single-device SyncDB
// runs. NetBox typically fires a burst of those events for one edit.
const netboxWebhookDebounce = 3 * time.Second

// netboxSyncDB is the single-device sync the webhook debounce calls. Tests
// replace it so they can assert GUI log lines without a live NetBox.
var netboxSyncDB = netbox.SyncDB

// netboxWebhookDebouncer delays a callback until no further schedule()
// for the same key has arrived for delay. Keys are independent so two
// devices can be waiting at once. Zero delay uses netboxWebhookDebounce.
type netboxWebhookDebouncer struct {
	delay   time.Duration
	mu      sync.Mutex
	pending map[string]*netboxWebhookDebounceWait
}

type netboxWebhookDebounceWait struct {
	timer *time.Timer
}

func (d *netboxWebhookDebouncer) wait() time.Duration {
	if d.delay == 0 {
		return netboxWebhookDebounce
	}
	return d.delay
}

// schedule arms or resets the quiet timer for deviceName. It returns true
// if a wait was already in progress (this event only postponed the sync).
func (d *netboxWebhookDebouncer) schedule(deviceName string, run func()) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = make(map[string]*netboxWebhookDebounceWait)
	}
	delay := d.wait()
	_, existed := d.pending[deviceName]
	if existed {
		d.pending[deviceName].timer.Stop()
	}
	e := &netboxWebhookDebounceWait{}
	e.timer = time.AfterFunc(delay, func() {
		d.mu.Lock()
		if d.pending[deviceName] != e {
			d.mu.Unlock()
			return
		}
		delete(d.pending, deviceName)
		d.mu.Unlock()
		run()
	})
	d.pending[deviceName] = e
	return existed
}

func (d *netboxWebhookDebouncer) cancel(deviceName string) {
	if deviceName == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.pending[deviceName]; ok {
		e.timer.Stop()
		delete(d.pending, deviceName)
	}
}

func (d *netboxWebhookDebouncer) stopAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, e := range d.pending {
		e.timer.Stop()
		delete(d.pending, name)
	}
}

// NetboxConfigResponse is what internal/netbox's FetchRemoteConfig parses -
// keep the JSON tags in sync with that type.
type NetboxConfigResponse struct {
	util.CommonConfig
	URL   string `json:"url"`
	Token string `json:"token"`
}

// ApiNetboxConfig returns the Netbox API connection settings from the
// database-backed Settings row, so callers that can't reach the primary's
// Postgres DB directly - currently factum2-librenms-cli's Sync(), which
// typically runs on the LibreNMS host, not the primary - can fetch them over
// REST instead.
func (ctrl *Controller) ApiNetboxConfig(c *echo.Context) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, NetboxConfigResponse{
		CommonConfig: util.NewCommonConfig(settings),
		URL:          settings.NetboxApiURL,
		Token:        settings.NetboxApiToken,
	})
}

// NetboxWebhookPayload is the JSON body Netbox posts for its default webhook
// body template (https://netboxlabs.com/docs/netbox/integrations/webhooks/):
// event/object_type/etc plus the changed object's serialized representation
// in Data - object_type is "app_label.model_name" (e.g. "dcim.device"), per
// Netbox's send_webhook() (extras/webhooks.py), which builds the context
// dict from object_type.natural_key(). Only the fields ApiNetboxWebhook
// needs are declared.
type NetboxWebhookPayload struct {
	Event      string         `json:"event"`
	ObjectType string         `json:"object_type"`
	Data       map[string]any `json:"data"`
}

// ApiNetboxWebhook receives change-event webhooks from Netbox.
//
// Device / interface / IP: create/update (and interface/IP delete) re-fetches
// the named device and upserts it. Device delete removes the matching
// netbox-sourced factum row by the payload's id — GetDevice would return
// nil once Netbox has already removed the object.
//
// Cable / site / region / location: create/update re-fetches that one
// object and upserts the Connection or hierarchical Site row; delete
// removes it by the payload's netbox_id. These are not "resync one named
// device" — they have no name lookup, and a deleted object cannot be
// re-fetched.
//
// Tenants and contacts are not applied here: customer→tenant and
// contact→contact sync are factum→Netbox.
//
// Netbox has no session/token auth for outgoing webhooks; instead it signs
// the request body with HMAC-SHA512 and sends the hex digest in
// "X-Hook-Signature", keyed by a shared secret configured on both sides
// (Settings.NetboxWebhookSecret here, the webhook's "secret" field on
// Netbox) - verified the same constant-time-compare way as
// checkServiceToken, and likewise fails closed if the secret isn't
// configured.
func (ctrl *Controller) ApiNetboxWebhook(c *echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "failed to read body"})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	signature := c.Request().Header.Get("X-Hook-Signature")
	if !validNetboxSignature(body, signature, settings.NetboxWebhookSecret) {
		slog.Debug("netbox webhook signature invalid", "signature", signature)
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid signature"})
	}

	var payload NetboxWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("netbox webhook", "error", "invalid payload")
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid payload"})
	}

	switch payload.ObjectType {
	case "dcim.device":
		if payload.Event == "deleted" {
			return ctrl.netboxWebhookDeleteDevice(c, payload)
		}
		return ctrl.netboxWebhookSyncDevice(c, payload)
	case "dcim.interface", "ipam.ipaddress":
		return ctrl.netboxWebhookSyncDevice(c, payload)
	case "dcim.cable":
		return ctrl.netboxWebhookCable(c, payload)
	case "dcim.site":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.site")
	case "dcim.region":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.region")
	case "dcim.location":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.location")
	default:
		slog.Debug("netbox webhook", "object_type", payload.ObjectType, "status", "ignored")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}
}

func (ctrl *Controller) netboxWebhookSyncDevice(c *echo.Context, payload NetboxWebhookPayload) error {
	deviceName := netboxWebhookDeviceName(payload.ObjectType, payload.Data)
	if deviceName == "" {
		slog.Debug("netbox webhook", "devicename", "not found")
		// A shape we can't map back to a device (e.g. a deleted interface
		// with no surviving device relation in the snapshot) - nothing to sync.
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}

	// Netbox's webhook delivery only waits on the HTTP response, not on the
	// sync itself. A single Netbox edit often posts several Device /
	// Interface / IP events for the same device; wait until that device has
	// been quiet for netboxWebhookDebounce, then SyncDB once.
	objectType := payload.ObjectType
	alreadyQueued := ctrl.netboxDeviceSyncDebounce.schedule(deviceName, func() {
		reporter := webhookReporter{next: jobevent.NewSlogReporter("source", "netbox"), deviceName: deviceName}
		if err := netboxSyncDB(ctrl.DB, deviceName, reporter); err != nil {
			slog.Error("netbox webhook sync", "device", deviceName, "object_type", objectType, "err", err)
		}
	})
	if !alreadyQueued {
		// One line at the start of a burst so the GUI log window shows
		// activity immediately; further events for this device only reset
		// the timer. webhookReporter emits started + summary when SyncDB
		// actually runs after the quiet period.
		slog.Info("Netbox webhook sync: device "+deviceName+" queued", "source", "netbox", "device", deviceName)
	}

	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "device": deviceName})
}

// netboxWebhookDeleteDevice applies a dcim.device "deleted" event: Netbox
// has already dropped the object, so we delete the factum row by the
// payload's id rather than trying to re-fetch it. Local DB only, so this
// runs in-request (unlike the create/update path, which waits on Netbox).
func (ctrl *Controller) netboxWebhookDeleteDevice(c *echo.Context, payload NetboxWebhookPayload) error {
	netboxID, ok := netboxWebhookObjectID(payload.Data)
	if !ok {
		slog.Debug("netbox webhook", "delete", "missing id")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}

	deviceName := netboxWebhookDeviceName(payload.ObjectType, payload.Data)
	ctrl.netboxDeviceSyncDebounce.cancel(deviceName)
	deleted, err := netbox.DeleteDeviceByNetboxID(ctrl.DB, netboxID, false)
	if err != nil {
		slog.Error("netbox webhook delete", "device", deviceName, "netbox_id", netboxID, "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	reporter := webhookReporter{next: jobevent.NewSlogReporter("source", "netbox"), deviceName: deviceName}
	reporter.Emit(jobevent.Info, "Netbox sync: %d new, %d updated, %d deleted", 0, 0, deleted)
	return c.JSON(http.StatusOK, map[string]any{
		"status":    "deleted",
		"device":    deviceName,
		"netbox_id": netboxID,
	})
}

func (ctrl *Controller) netboxWebhookCable(c *echo.Context, payload NetboxWebhookPayload) error {
	netboxID, ok := netboxWebhookObjectID(payload.Data)
	if !ok {
		slog.Debug("netbox webhook", "cable", "missing id")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}
	if payload.Event == "deleted" {
		deleted, err := netbox.DeleteConnectionByNetboxID(ctrl.DB, netboxID)
		if err != nil {
			slog.Error("netbox webhook delete cable", "netbox_id", netboxID, "err", err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		slog.Info("netbox webhook deleted cable", "netbox_id", netboxID, "deleted", deleted)
		return c.JSON(http.StatusOK, map[string]any{"status": "deleted", "netbox_id": netboxID})
	}
	go func() {
		if err := netbox.SyncCable(ctrl.DB, netboxID, jobevent.NewSlogReporter("source", "netbox")); err != nil {
			slog.Error("netbox webhook cable sync", "netbox_id", netboxID, "err", err)
		}
	}()
	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "object_type": "dcim.cable", "netbox_id": netboxID})
}

func netboxKindFromObjectType(objectType string) string {
	switch objectType {
	case "dcim.region":
		return models.SiteNetboxKindRegion
	case "dcim.location":
		return models.SiteNetboxKindLocation
	default:
		return models.SiteNetboxKindSite
	}
}

func (ctrl *Controller) netboxWebhookTreeItem(c *echo.Context, payload NetboxWebhookPayload, objectType string) error {
	netboxID, ok := netboxWebhookObjectID(payload.Data)
	if !ok {
		slog.Debug("netbox webhook", objectType, "missing id")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}
	kind := netboxKindFromObjectType(objectType)
	if payload.Event == "deleted" {
		deleted, err := netbox.DeleteSyncedSiteNode(ctrl.DB, kind, netboxID)
		if err != nil {
			slog.Error("netbox webhook delete "+objectType, "netbox_id", netboxID, "err", err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		slog.Info("netbox webhook deleted "+objectType, "netbox_id", netboxID, "deleted", deleted)
		return c.JSON(http.StatusOK, map[string]any{"status": "deleted", "netbox_id": netboxID})
	}
	go func() {
		if err := netbox.SyncDCIMTreeItem(ctrl.DB, kind, netboxID, jobevent.NewSlogReporter("source", "netbox")); err != nil {
			slog.Error("netbox webhook "+objectType+" sync", "netbox_id", netboxID, "err", err)
		}
	}()
	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "object_type": objectType, "netbox_id": netboxID})
}

// webhookReporter wraps the reporter given to netbox.SyncDB for the webhook
// path, which always syncs exactly one device. SyncDB is shared with the
// CLI tools' console/JSON reporters, which still want the unadorned
// "started" / summary lines, so rather than changing SyncDB itself this
// folds the device name into both lines for the web GUI's log window
// (fed by slog, see web/logstream.go).
type webhookReporter struct {
	next       jobevent.Reporter
	deviceName string
}

func (r webhookReporter) Emit(level jobevent.Level, format string, args ...any) {
	switch format {
	case "Netbox sync started":
		r.next.Emit(level, "Netbox sync: device %s started", r.deviceName)
		return
	case "Netbox sync: %d new, %d updated, %d deleted":
		r.next.Emit(level, "Netbox sync: device %s: %d new, %d updated, %d deleted", append([]any{r.deviceName}, args...)...)
		return
	}
	r.next.Emit(level, format, args...)
}

func (r webhookReporter) EmitErr(err error) {
	r.next.EmitErr(err)
}

func validNetboxSignature(body []byte, signature, secret string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1
}

// netboxWebhookDeviceName extracts the device name a webhook event applies
// to, regardless of which of the three subscribed object types fired: a
// dcim.device event carries it directly on Data; dcim.interface nests it
// one level down under Data["device"] (InterfaceSerializer's nested
// DeviceSerializer, which always includes "name"); ipam.ipaddress nests it
// two levels down under Data["assigned_object"]["device"] (assigned_object
// is Netbox's generic-FK nested serialization of whatever the address is
// assigned to - typically an interface, which nests "device" the same way).
func netboxWebhookDeviceName(objectType string, data map[string]any) string {
	if objectType == "dcim.device" {
		if name, ok := data["name"].(string); ok {
			return name
		}
	}
	if dev, ok := data["device"].(map[string]any); ok {
		if name, ok := dev["name"].(string); ok {
			return name
		}
	}
	if assignedObject, ok := data["assigned_object"].(map[string]any); ok {
		if dev, ok := assignedObject["device"].(map[string]any); ok {
			if name, ok := dev["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}

// netboxWebhookObjectID reads Data["id"] from a default-template webhook
// body. encoding/json unmarshals JSON numbers into map[string]any as
// float64; reject non-integers and non-positive IDs rather than truncate.
func netboxWebhookObjectID(data map[string]any) (uint, bool) {
	n, ok := data["id"].(float64)
	if !ok || n < 1 || n != float64(uint64(n)) {
		return 0, false
	}
	return uint(n), true
}
