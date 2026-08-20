package librenms

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
)

const (
	pendingReasonNoMatch      = "no_match"
	pendingReasonDisabled     = "disabled"
	pendingReasonNotMonitored = "not_monitored"

	defaultDelayedDeleteDays = 30
)

// pendingDeleteSuffixRE matches the display-name stamp sync adds while a
// device is quarantined, e.g. " (scheduled for deletion 2026-09-19)".
var pendingDeleteSuffixRE = regexp.MustCompile(` \(scheduled for deletion \d{4}-\d{2}-\d{2}\)$`)

func pendingDeleteDisplay(name string, when time.Time) string {
	name = stripPendingDeleteSuffix(name)
	if name == "" {
		name = "unknown"
	}
	return name + fmt.Sprintf(" (scheduled for deletion %s)", when.Format("2006-01-02"))
}

func stripPendingDeleteSuffix(name string) string {
	return pendingDeleteSuffixRE.ReplaceAllString(name, "")
}

func delayedDeleteDays(n int) int {
	if n < 1 {
		return defaultDelayedDeleteDays
	}
	return n
}

func isPersistentDevice(device *LibrenmsDevice, list string) bool {
	if device == nil {
		return false
	}
	display := stripPendingDeleteSuffix(device.Display)
	for _, name := range splitLines(list) {
		if strings.EqualFold(name, device.Hostname) ||
			strings.EqualFold(name, device.Display) ||
			strings.EqualFold(name, display) {
			return true
		}
	}
	return false
}

type pendingCandidate struct {
	Device *LibrenmsDevice
	Reason string
}

type pendingStore interface {
	Upsert(row *models.LibrenmsPendingDelete) (*models.LibrenmsPendingDelete, error)
	Remove(deviceID int) error
}

type deviceMutator interface {
	DeviceUpdate(deviceID int, updates map[string]string) (*string, error)
	DeviceDelete(deviceID int) (*string, error)
	DeviceGet(deviceID int) (*LibrenmsDevice, error)
}

// applyDeletePolicy quarantines, restores, or deletes LibreNMS devices that
// are (or were) sync delete-candidates. stillPending is the set of LibreNMS
// device_ids that remain quarantined afterwards, so the rest of Sync can
// skip rewriting their display/ignore flags.
func applyDeletePolicy(
	enabled bool,
	days int,
	candidates []pendingCandidate,
	existing []*models.LibrenmsPendingDelete,
	presentIDs map[int]bool,
	leaveAlone map[int]bool,
	now time.Time,
	nms deviceMutator,
	store pendingStore,
	reporter jobevent.Reporter,
) (stillPending map[int]struct{}, deleted int, err error) {
	days = delayedDeleteDays(days)
	stillPending = make(map[int]struct{})
	pendingByID := make(map[int]*models.LibrenmsPendingDelete, len(existing))
	for _, row := range existing {
		pendingByID[row.DeviceID] = row
	}
	candidateIDs := make(map[int]struct{}, len(candidates))

	for _, c := range candidates {
		device := c.Device
		candidateIDs[device.DeviceID] = struct{}{}
		row := pendingByID[device.DeviceID]
		if row == nil {
			if !enabled {
				reporter.Emit(jobevent.Info, "would delete device %s from LibreNMS (delayed deletion disabled)", device.Hostname)
				continue
			}
			if err := quarantineDevice(nms, store, device, c.Reason, now.AddDate(0, 0, days), reporter); err != nil {
				return stillPending, deleted, err
			}
			stillPending[device.DeviceID] = struct{}{}
			continue
		}

		due := !now.Before(row.ScheduledAt)
		if row.ForceDelete || (enabled && due) {
			if err := deleteQuarantinedDevice(nms, store, device.DeviceID, device.Hostname, row, reporter); err != nil {
				return stillPending, deleted, err
			}
			deleted++
			continue
		}
		if enabled {
			if err := ensureQuarantine(nms, device, row, reporter); err != nil {
				return stillPending, deleted, err
			}
		}
		stillPending[device.DeviceID] = struct{}{}
	}

	for _, row := range existing {
		if _, still := candidateIDs[row.DeviceID]; still {
			continue
		}
		if leaveAlone[row.DeviceID] {
			if err := store.Remove(row.DeviceID); err != nil {
				return stillPending, deleted, err
			}
			reporter.Emit(jobevent.Info, "dropping pending delete for persistent device %s", row.Hostname)
			continue
		}
		if !presentIDs[row.DeviceID] {
			if err := store.Remove(row.DeviceID); err != nil {
				return stillPending, deleted, err
			}
			reporter.Emit(jobevent.Info, "dropping pending delete for LibreNMS device_id %d (%s): device is already gone", row.DeviceID, row.Hostname)
			continue
		}
		if err := restoreDevice(nms, store, row, reporter); err != nil {
			return stillPending, deleted, err
		}
	}
	return stillPending, deleted, nil
}

func originalDisplay(device *LibrenmsDevice) string {
	name := stripPendingDeleteSuffix(device.Display)
	if name == "" {
		name = device.Hostname
	}
	return name
}

func quarantineDevice(nms deviceMutator, store pendingStore, device *LibrenmsDevice, reason string, scheduledAt time.Time, reporter jobevent.Reporter) error {
	display := originalDisplay(device)
	updates := map[string]string{
		"disabled":         "1",
		"ignore":           "1",
		"display_template": pendingDeleteDisplay(display, scheduledAt),
	}
	if _, err := nms.DeviceUpdate(device.DeviceID, updates); err != nil {
		return fmt.Errorf("quarantine device %s: %w", device.Hostname, err)
	}
	row := &models.LibrenmsPendingDelete{
		DeviceID:    device.DeviceID,
		Hostname:    device.Hostname,
		Display:     display,
		Reason:      reason,
		ScheduledAt: scheduledAt,
	}
	if _, err := store.Upsert(row); err != nil {
		return fmt.Errorf("record pending delete for %s: %w", device.Hostname, err)
	}
	reporter.Emit(jobevent.Info, "quarantining device %s, scheduled for deletion %s (%s)", device.Hostname, scheduledAt.Format("2006-01-02"), reason)
	return nil
}

func ensureQuarantine(nms deviceMutator, device *LibrenmsDevice, row *models.LibrenmsPendingDelete, reporter jobevent.Reporter) error {
	display := row.Display
	if display == "" {
		display = originalDisplay(device)
	}
	want := pendingDeleteDisplay(display, row.ScheduledAt)
	if device.Disabled == 1 && device.Ignore == 1 && device.Display == want {
		return nil
	}
	updates := map[string]string{
		"disabled":         "1",
		"ignore":           "1",
		"display_template": want,
	}
	if _, err := nms.DeviceUpdate(device.DeviceID, updates); err != nil {
		return fmt.Errorf("refresh quarantine on %s: %w", device.Hostname, err)
	}
	reporter.Emit(jobevent.Info, "keeping device %s quarantined until %s", device.Hostname, row.ScheduledAt.Format("2006-01-02"))
	return nil
}

func deleteQuarantinedDevice(nms deviceMutator, store pendingStore, deviceID int, hostname string, row *models.LibrenmsPendingDelete, reporter jobevent.Reporter) error {
	why := "scheduled " + row.ScheduledAt.Format("2006-01-02")
	if row.ForceDelete {
		why = "requested by user"
	}
	if _, err := nms.DeviceDelete(deviceID); err != nil {
		if _, getErr := nms.DeviceGet(deviceID); getErr == nil {
			return fmt.Errorf("delete device %s: %w", hostname, err)
		}
		// Device is already gone - treat as success so the pending row
		// can still be dropped.
	}
	if err := store.Remove(deviceID); err != nil {
		return fmt.Errorf("clear pending delete for %s: %w", hostname, err)
	}
	reporter.Emit(jobevent.Info, "deleting device %s from LibreNMS (%s)", hostname, why)
	return nil
}

func restoreDevice(nms deviceMutator, store pendingStore, row *models.LibrenmsPendingDelete, reporter jobevent.Reporter) error {
	display := stripPendingDeleteSuffix(row.Display)
	if display == "" {
		display = row.Hostname
	}
	updates := map[string]string{
		"disabled":         "0",
		"display_template": display,
	}
	if _, err := nms.DeviceUpdate(row.DeviceID, updates); err != nil {
		return fmt.Errorf("restore device %s: %w", row.Hostname, err)
	}
	if err := store.Remove(row.DeviceID); err != nil {
		return fmt.Errorf("clear pending delete for %s: %w", row.Hostname, err)
	}
	reporter.Emit(jobevent.Info, "restoring device %s, no longer a delete candidate", row.Hostname)
	return nil
}
