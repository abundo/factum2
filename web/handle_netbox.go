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

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/labstack/echo/v5"
)

// NetboxConfigResponse is what internal/netbox's FetchRemoteConfig parses -
// keep the JSON tags in sync with that type.
type NetboxConfigResponse struct {
	util.CommonConfig
	URL   string `json:"url"`
	Token string `json:"token"`
}

// ApiNetboxConfig returns the Netbox API connection settings from the
// database-backed Settings row, so callers that can't reach the primary's
// Postgres DB directly - currently factum-librenms-cli's Sync(), which
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
// Cable / site: create/update re-fetches that one object and upserts the
// Connection/Site row; delete removes it by the payload's netbox_id. These
// are not "resync one named device" — they have no name lookup, and a
// deleted cable/site cannot be re-fetched.
//
// Tenants are not applied here: customer→tenant sync is factum→Netbox.
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
		return ctrl.netboxWebhookSite(c, payload)
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
	// sync itself - run it after responding so a slow Netbox API round-trip
	// can't make the delivery time out and retry.
	go func() {
		reporter := webhookReporter{next: jobevent.NewSlogReporter(), deviceName: deviceName}
		if err := netbox.SyncDB(ctrl.DB, deviceName, reporter); err != nil {
			slog.Error("netbox webhook sync", "device", deviceName, "object_type", payload.ObjectType, "err", err)
		}
	}()

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
	deleted, err := netbox.DeleteDeviceByNetboxID(ctrl.DB, netboxID, false)
	if err != nil {
		slog.Error("netbox webhook delete", "device", deviceName, "netbox_id", netboxID, "err", err)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	reporter := webhookReporter{next: jobevent.NewSlogReporter(), deviceName: deviceName}
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
		if err := netbox.SyncCable(ctrl.DB, netboxID, jobevent.NewSlogReporter()); err != nil {
			slog.Error("netbox webhook cable sync", "netbox_id", netboxID, "err", err)
		}
	}()
	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "object_type": "dcim.cable", "netbox_id": netboxID})
}

func (ctrl *Controller) netboxWebhookSite(c *echo.Context, payload NetboxWebhookPayload) error {
	netboxID, ok := netboxWebhookObjectID(payload.Data)
	if !ok {
		slog.Debug("netbox webhook", "site", "missing id")
		return c.JSON(http.StatusOK, map[string]any{"status": "ignored"})
	}
	if payload.Event == "deleted" {
		deleted, err := netbox.DeleteSiteByNetboxID(ctrl.DB, netboxID)
		if err != nil {
			slog.Error("netbox webhook delete site", "netbox_id", netboxID, "err", err)
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		slog.Info("netbox webhook deleted site", "netbox_id", netboxID, "deleted", deleted)
		return c.JSON(http.StatusOK, map[string]any{"status": "deleted", "netbox_id": netboxID})
	}
	go func() {
		if err := netbox.SyncSite(ctrl.DB, netboxID, jobevent.NewSlogReporter()); err != nil {
			slog.Error("netbox webhook site sync", "netbox_id", netboxID, "err", err)
		}
	}()
	return c.JSON(http.StatusAccepted, map[string]any{"status": "queued", "object_type": "dcim.site", "netbox_id": netboxID})
}

// webhookReporter wraps the reporter given to netbox.SyncDB for the webhook
// path, which always syncs exactly one device. SyncDB is shared with the
// CLI tools' console/JSON reporters, which still want the separate
// "started" progress line, so rather than changing SyncDB itself this
// drops that line and folds the device name into the final summary -
// producing exactly one line, emitted once the sync is done, in the web
// GUI's log window (fed by slog, see web/logstream.go) per webhook call.
type webhookReporter struct {
	next       jobevent.Reporter
	deviceName string
}

func (r webhookReporter) Emit(level jobevent.Level, format string, args ...any) {
	switch format {
	case "Netbox sync started":
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
