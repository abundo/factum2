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

// netboxWebhookDebounce is how long webhooks must stay quiet before one
// delta sync starts. The wait is shared: a burst resets one timer, and
// a single changelog pass runs only after that timer fires.
const netboxWebhookDebounce = 3 * time.Second

// netboxSyncDelta is the sync the webhook debounce calls. Tests replace
// it so they can assert GUI log lines without a live NetBox.
var netboxSyncDelta = netbox.SyncDeltaDB

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
// Create and update events share one quiet period, then one delta sync
// reads the changelog since the last cursor. That covers the device the
// webhook named and anything else that changed in the same window
// (interfaces, addresses, cables, sites, VMs, racks, VRFs, L2VPNs).
// A wide device set, or a missing cursor, makes that delta run a full
// sync instead.
//
// Deletes of a device, VM, cable, site, region, or location are applied
// immediately from the payload id — the object is already gone, so it
// cannot be re-fetched — and a deleted device or VM is dropped from the
// quiet-period queue.
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
			return ctrl.netboxWebhookDeleteDevice(c, payload, false)
		}
		return ctrl.netboxWebhookSyncDevice(c, payload)
	case "virtualization.virtualmachine":
		if payload.Event == "deleted" {
			return ctrl.netboxWebhookDeleteDevice(c, payload, true)
		}
		return ctrl.netboxWebhookSyncDevice(c, payload)
	case "dcim.interface", "virtualization.vminterface", "ipam.ipaddress":
		return ctrl.netboxWebhookSyncDevice(c, payload)
	case "dcim.cable":
		return ctrl.netboxWebhookCable(c, payload)
	case "dcim.site":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.site")
	case "dcim.region":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.region")
	case "dcim.location":
		return ctrl.netboxWebhookTreeItem(c, payload, "dcim.location")
	case "dcim.rack", "dcim.devicetype", "ipam.vrf", "vpn.l2vpn", "vpn.l2vpntermination":
		return ctrl.netboxWebhookQueueDelta(c, payload.ObjectType, payload.ObjectType)
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

	return ctrl.netboxWebhookQueueDelta(c, deviceName, "device "+deviceName)
}

// netboxWebhookQueueDelta arms the shared quiet window. logName is the
// GUI log phrase ("device sw1", "dcim.cable"). key is the debounce set
// member so a repeated event for the same object only logs once.
func (ctrl *Controller) netboxWebhookQueueDelta(c *echo.Context, key, logName string) error {
	if key == "" {
		key = "delta"
	}
	// Netbox's webhook delivery only waits on the HTTP response, not on
	// the sync itself. After netboxWebhookDebounce with no further event,
	// one delta sync reads the changelog.
	fresh := ctrl.netboxDeviceSyncDebounce.schedule(key, ctrl.netboxWebhookSyncQueued)
	if fresh {
		slog.Info("Netbox webhook sync: "+logName+" queued", "source", "netbox", "device", key)
	}
	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued"})
}

// netboxWebhookSyncQueued runs one delta sync for the quiet window.
func (ctrl *Controller) netboxWebhookSyncQueued(names []string) {
	if len(names) == 0 {
		return
	}
	slog.Info(fmt.Sprintf("Netbox webhook sync: %d queued, running delta sync", len(names)),
		"source", "netbox", "queued", len(names))
	if err := netboxSyncDelta(ctrl.DB, jobevent.NewSlogReporter("source", "netbox")); err != nil {
		slog.Error("netbox webhook sync", "err", err)
	}
}

// netboxWebhookDeleteDevice applies a dcim.device "deleted" event: Netbox
// has already dropped the object, so we delete the factum row by the
// payload's id rather than trying to re-fetch it. Local DB only, so this
// runs in-request (unlike the create/update path, which waits on Netbox).
func (ctrl *Controller) netboxWebhookDeleteDevice(c *echo.Context, payload NetboxWebhookPayload, vm bool) error {
	netboxID, ok := netboxWebhookObjectID(payload.Data)
	if !ok {
		slog.Debug("netbox webhook", "delete", "missing id")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}

	deviceName := netboxWebhookDeviceName(payload.ObjectType, payload.Data)
	ctrl.netboxDeviceSyncDebounce.cancel(deviceName)
	deleted, err := netbox.DeleteDeviceByNetboxID(ctrl.DB, netboxID, vm)
	if err != nil {
		slog.Error("netbox webhook delete", "device", deviceName, "netbox_id", netboxID, "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	slog.Info(fmt.Sprintf("Netbox sync: device %s: %d new, %d updated, %d deleted", deviceName, 0, 0, deleted),
		"source", "netbox", "device", deviceName)
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
	return ctrl.netboxWebhookQueueDelta(c, fmt.Sprintf("cable:%d", netboxID), fmt.Sprintf("cable %d", netboxID))
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
	return ctrl.netboxWebhookQueueDelta(c, fmt.Sprintf("%s:%d", objectType, netboxID), objectType+" "+fmt.Sprint(netboxID))
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
	if objectType == "dcim.device" || objectType == "virtualization.virtualmachine" {
		if name, ok := data["name"].(string); ok {
			return name
		}
	}
	if dev, ok := data["device"].(map[string]any); ok {
		if name, ok := dev["name"].(string); ok {
			return name
		}
	}
	if vm, ok := data["virtual_machine"].(map[string]any); ok {
		if name, ok := vm["name"].(string); ok {
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
