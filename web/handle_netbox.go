package web

import (
	"crypto/hmac"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

// netboxWebhookDebounce is how long Device / Interface / IP webhooks must
// stay quiet before any queued device sync starts. The wait is shared
// across devices: a burst resets one timer, and the syncs run only after
// that timer fires.
const netboxWebhookDebounce = 3 * time.Second

// A full SyncDB reads NetBox once. Each single-device sync repeats the
// full IP-address walk (fetchAddressDNSNames) plus a per-device fetch, so
// a wide burst is cheaper as one inventory pass. The switch happens when
// the queue is at least netboxWebhookFullSyncMin devices and at least
// netboxWebhookFullSyncPercent of the NetBox devices already stored in
// factum. The floor keeps a small lab on the per-device path: one edit
// out of four is 25%, and a full sync also pulls cables, sites, racks,
// VRFs, customers, and L2VPNs.
const (
	netboxWebhookFullSyncPercent = 20
	netboxWebhookFullSyncMin     = 10
)

// netboxSyncDB is the sync the webhook debounce calls. Tests replace it
// so they can assert GUI log lines without a live NetBox. An empty name
// is a full sync.
var netboxSyncDB = netbox.SyncDB

// netboxWebhookPreferFullSync reports whether pending device names should
// be applied with one full SyncDB. known is the number of NetBox-sourced
// devices already in factum; zero means the queued names are not in the
// local inventory yet, so a large enough batch is still one full sync.
func netboxWebhookPreferFullSync(pending, known int) bool {
	if pending < netboxWebhookFullSyncMin {
		return false
	}
	if known <= 0 {
		return true
	}
	return int64(pending)*100 >= int64(known)*int64(netboxWebhookFullSyncPercent)
}

// netboxWebhookDebouncer collects device names and runs them once, after
// delay with no further schedule(). Devices that arrive while a batch is
// running join the next quiet window and do not start a second sync.
// Zero delay uses netboxWebhookDebounce.
type netboxWebhookDebouncer struct {
	delay time.Duration
	mu    sync.Mutex
	// pending is the set of device names waiting for the quiet window.
	pending map[string]struct{}
	timer   *time.Timer
	// generation invalidates a timer callback after the timer is reset.
	generation uint64
	// draining is true while run is executing a batch.
	draining bool
	// due is set when the quiet window elapses during a batch. That batch
	// finishes first; the next one starts only if due is still set.
	due bool
	run func([]string)
}

func (d *netboxWebhookDebouncer) wait() time.Duration {
	if d.delay == 0 {
		return netboxWebhookDebounce
	}
	return d.delay
}

// schedule adds deviceName to the shared quiet window and resets the
// timer. fresh is true when deviceName was not already waiting. run
// receives every device still waiting, sorted, and is not called
// concurrently with itself.
func (d *netboxWebhookDebouncer) schedule(deviceName string, run func([]string)) (fresh bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = make(map[string]struct{})
	}
	_, existed := d.pending[deviceName]
	d.pending[deviceName] = struct{}{}
	d.run = run
	d.armLocked()
	return !existed
}

// armLocked resets the quiet timer. Caller holds d.mu.
func (d *netboxWebhookDebouncer) armLocked() {
	d.due = false
	d.generation++
	gen := d.generation
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.wait(), func() {
		d.onQuiet(gen)
	})
}

func (d *netboxWebhookDebouncer) onQuiet(gen uint64) {
	d.mu.Lock()
	if gen != d.generation {
		d.mu.Unlock()
		return
	}
	d.timer = nil
	if d.draining {
		d.due = true
		d.mu.Unlock()
		return
	}
	d.drainLocked()
}

// drainLocked runs batches until a quiet window is no longer due.
// Caller holds d.mu. It releases the lock before returning.
func (d *netboxWebhookDebouncer) drainLocked() {
	for {
		names := d.takeLocked()
		if len(names) == 0 {
			d.due = false
			d.mu.Unlock()
			return
		}
		run := d.run
		d.draining = true
		d.due = false
		d.mu.Unlock()
		if run != nil {
			run(names)
		}
		d.mu.Lock()
		d.draining = false
		if !d.due {
			d.mu.Unlock()
			return
		}
	}
}

func (d *netboxWebhookDebouncer) takeLocked() []string {
	if len(d.pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(d.pending))
	for name := range d.pending {
		names = append(names, name)
	}
	d.pending = nil
	sort.Strings(names)
	return names
}

func (d *netboxWebhookDebouncer) cancel(deviceName string) {
	if deviceName == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.pending[deviceName]; !ok {
		return
	}
	delete(d.pending, deviceName)
	if len(d.pending) == 0 {
		if d.timer != nil {
			d.timer.Stop()
			d.timer = nil
		}
		d.generation++
		d.due = false
	}
}

func (d *netboxWebhookDebouncer) stopAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pending = nil
	d.due = false
	d.generation++
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
// Device / interface / IP: create/update (and interface/IP delete) queue
// the named device. After a shared quiet period the queued devices sync
// one at a time, or as one full sync when the burst is a large share of
// the stored NetBox inventory. Device delete removes the matching
// netbox-sourced factum row by the payload's id — GetDevice would return
// nil once Netbox has already removed the object — and drops that name
// from the queue.
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
	// sync itself. Device, interface, and IP events share one quiet window:
	// after netboxWebhookDebounce with no further event, the queued devices
	// sync one at a time (or as one full sync when the burst is wide).
	fresh := ctrl.netboxDeviceSyncDebounce.schedule(deviceName, ctrl.netboxWebhookSyncQueued)
	if fresh {
		// One line the first time a device joins the current window, so
		// the GUI log shows activity immediately. Further events for a
		// device already waiting only reset the shared timer.
		slog.Info("Netbox webhook sync: device "+deviceName+" queued", "source", "netbox", "device", deviceName)
	}

	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "device": deviceName})
}

// netboxWebhookSyncQueued applies a quiet-window snapshot. A wide burst
// becomes one full SyncDB; otherwise each device syncs in order, and a
// failure on one device does not skip the rest.
func (ctrl *Controller) netboxWebhookSyncQueued(names []string) {
	if len(names) == 0 {
		return
	}
	var known int64
	full := false
	if err := ctrl.DB.Model(&models.Device{}).Where("cf_source = ?", "netbox").Count(&known).Error; err != nil {
		slog.Error("netbox webhook sync", "err", err)
	} else {
		full = netboxWebhookPreferFullSync(len(names), int(known))
	}
	if full {
		slog.Info(fmt.Sprintf("Netbox webhook sync: %d devices changed, running full sync", len(names)),
			"source", "netbox", "devices", len(names), "known", known)
		if err := netboxSyncDB(ctrl.DB, "", jobevent.NewSlogReporter("source", "netbox")); err != nil {
			slog.Error("netbox webhook sync", "err", err)
		}
		return
	}
	if len(names) > 1 {
		slog.Info(fmt.Sprintf("Netbox webhook sync: %d devices, one at a time", len(names)),
			"source", "netbox", "devices", len(names))
	}
	for _, deviceName := range names {
		reporter := webhookReporter{next: jobevent.NewSlogReporter("source", "netbox"), deviceName: deviceName}
		if err := netboxSyncDB(ctrl.DB, deviceName, reporter); err != nil {
			slog.Error("netbox webhook sync", "device", deviceName, "err", err)
		}
	}
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
