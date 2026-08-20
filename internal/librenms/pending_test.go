package librenms

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
)

func TestPendingDeleteDisplay(t *testing.T) {
	when := time.Date(2026, 9, 19, 15, 4, 0, 0, time.UTC)
	got := pendingDeleteDisplay("rtr1", when)
	want := "rtr1 (scheduled for deletion 2026-09-19)"
	if got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
	if stripPendingDeleteSuffix(got) != "rtr1" {
		t.Fatalf("strip = %q, want rtr1", stripPendingDeleteSuffix(got))
	}
	doubled := pendingDeleteDisplay(got, when)
	if doubled != want {
		t.Fatalf("restamp = %q, want %q", doubled, want)
	}
}

func TestDelayedDeleteDays(t *testing.T) {
	if got := delayedDeleteDays(0); got != 30 {
		t.Fatalf("0 -> %d, want 30", got)
	}
	if got := delayedDeleteDays(-1); got != 30 {
		t.Fatalf("-1 -> %d, want 30", got)
	}
	if got := delayedDeleteDays(7); got != 7 {
		t.Fatalf("7 -> %d, want 7", got)
	}
}

func TestIsPersistentDevice(t *testing.T) {
	d := &LibrenmsDevice{Hostname: "10.0.0.1", Display: "rtr1 (scheduled for deletion 2026-09-19)"}
	if !isPersistentDevice(d, "rtr1\nother") {
		t.Fatal("expected match on original display")
	}
	if !isPersistentDevice(d, "10.0.0.1") {
		t.Fatal("expected match on hostname")
	}
	if isPersistentDevice(d, "unrelated") {
		t.Fatal("unexpected match")
	}
}

type memStore struct {
	rows map[int]*models.LibrenmsPendingDelete
}

func (s *memStore) Upsert(row *models.LibrenmsPendingDelete) (*models.LibrenmsPendingDelete, error) {
	cp := *row
	s.rows[row.DeviceID] = &cp
	return &cp, nil
}
func (s *memStore) Remove(deviceID int) error {
	delete(s.rows, deviceID)
	return nil
}

type memNMS struct {
	devices map[int]*LibrenmsDevice
	deleted []int
	updates []int
}

func (n *memNMS) DeviceUpdate(deviceID int, updates map[string]string) (*string, error) {
	d, ok := n.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("missing device %d", deviceID)
	}
	n.updates = append(n.updates, deviceID)
	if v, ok := updates["disabled"]; ok && v == "1" {
		d.Disabled = 1
	}
	if v, ok := updates["disabled"]; ok && v == "0" {
		d.Disabled = 0
	}
	if v, ok := updates["ignore"]; ok && v == "1" {
		d.Ignore = 1
	}
	if v, ok := updates["display_template"]; ok {
		d.Display = v
	}
	okStr := "ok"
	return &okStr, nil
}
func (n *memNMS) DeviceDelete(deviceID int) (*string, error) {
	if _, ok := n.devices[deviceID]; !ok {
		return nil, fmt.Errorf("missing device %d", deviceID)
	}
	delete(n.devices, deviceID)
	n.deleted = append(n.deleted, deviceID)
	okStr := "ok"
	return &okStr, nil
}
func (n *memNMS) DeviceGet(deviceID int) (*LibrenmsDevice, error) {
	d, ok := n.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("missing device %d", deviceID)
	}
	return d, nil
}

type recReporter struct{ msgs []string }

func (r *recReporter) Emit(_ jobevent.Level, format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *recReporter) EmitErr(err error) {
	if err != nil {
		r.Emit(jobevent.Error, "%s", err)
	}
}

func TestApplyDeletePolicy_QuarantineThenDeleteOnLaterRun(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1"}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{}}
	rep := &recReporter{}

	still, deleted, err := applyDeletePolicy(true, 30, []pendingCandidate{{Device: dev, Reason: pendingReasonNoMatch}}, nil, map[int]bool{7: true}, nil, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	if _, ok := still[7]; !ok {
		t.Fatal("device should still be pending")
	}
	if nms.devices[7].Disabled != 1 || nms.devices[7].Ignore != 1 {
		t.Fatalf("quarantine flags: %+v", nms.devices[7])
	}
	if !strings.Contains(nms.devices[7].Display, "scheduled for deletion 2026-09-19") {
		t.Fatalf("display = %q", nms.devices[7].Display)
	}
	if len(store.rows) != 1 {
		t.Fatalf("pending rows = %d", len(store.rows))
	}

	later := now.AddDate(0, 0, 30)
	existing := []*models.LibrenmsPendingDelete{store.rows[7]}
	still, deleted, err = applyDeletePolicy(true, 30, []pendingCandidate{{Device: nms.devices[7], Reason: pendingReasonNoMatch}}, existing, map[int]bool{7: true}, nil, later, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if len(still) != 0 {
		t.Fatalf("still pending = %v", still)
	}
	if _, ok := nms.devices[7]; ok {
		t.Fatal("device should have been deleted")
	}
	if len(store.rows) != 0 {
		t.Fatal("pending row should be gone")
	}
}

func TestApplyDeletePolicy_DisabledDoesNotDelete(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1"}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{}}
	rep := &recReporter{}

	_, deleted, err := applyDeletePolicy(false, 30, []pendingCandidate{{Device: dev, Reason: pendingReasonNoMatch}}, nil, map[int]bool{7: true}, nil, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 || len(store.rows) != 0 || dev.Disabled != 0 {
		t.Fatalf("disabled feature mutated state: deleted=%d rows=%d disabled=%d", deleted, len(store.rows), dev.Disabled)
	}
}

func TestApplyDeletePolicy_ForceDeleteEvenWhenDisabled(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1"}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	row := &models.LibrenmsPendingDelete{
		DeviceID:    7,
		Hostname:    "10.1.1.1",
		Display:     "rtr1",
		ScheduledAt: now.AddDate(0, 0, 30),
		ForceDelete: true,
	}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{7: row}}
	rep := &recReporter{}

	_, deleted, err := applyDeletePolicy(false, 30, []pendingCandidate{{Device: dev, Reason: pendingReasonNoMatch}}, []*models.LibrenmsPendingDelete{row}, map[int]bool{7: true}, nil, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, ok := nms.devices[7]; ok {
		t.Fatal("force-delete should remove the device")
	}
}

func TestApplyDeletePolicy_RestoreWhenNoLongerCandidate(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1 (scheduled for deletion 2026-09-19)", Disabled: 1, Ignore: 1}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	row := &models.LibrenmsPendingDelete{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1", ScheduledAt: now.AddDate(0, 0, 30)}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{7: row}}
	rep := &recReporter{}

	still, deleted, err := applyDeletePolicy(true, 30, nil, []*models.LibrenmsPendingDelete{row}, map[int]bool{7: true}, nil, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 || len(still) != 0 {
		t.Fatalf("deleted=%d still=%v", deleted, still)
	}
	if nms.devices[7].Disabled != 0 || nms.devices[7].Display != "rtr1" {
		t.Fatalf("restored device: %+v", nms.devices[7])
	}
	if len(store.rows) != 0 {
		t.Fatal("pending row should be gone after restore")
	}
}

func TestApplyDeletePolicy_PersistentDropsPendingWithoutTouchingDevice(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1 (scheduled for deletion 2026-09-19)", Disabled: 1, Ignore: 1}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	row := &models.LibrenmsPendingDelete{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1", ScheduledAt: now.AddDate(0, 0, 30)}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{7: row}}
	rep := &recReporter{}

	still, deleted, err := applyDeletePolicy(true, 30, nil, []*models.LibrenmsPendingDelete{row}, map[int]bool{7: true}, map[int]bool{7: true}, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 || len(still) != 0 {
		t.Fatalf("deleted=%d still=%v", deleted, still)
	}
	if nms.devices[7].Disabled != 1 || nms.devices[7].Display != "rtr1 (scheduled for deletion 2026-09-19)" {
		t.Fatalf("persistent device was touched: %+v", nms.devices[7])
	}
	if len(store.rows) != 0 {
		t.Fatal("pending row should be dropped")
	}
	if len(nms.updates) != 0 || len(nms.deleted) != 0 {
		t.Fatalf("LibreNMS mutations: updates=%v deleted=%v", nms.updates, nms.deleted)
	}
}

func TestApplyDeletePolicy_SameRunNeverDeletes(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dev := &LibrenmsDevice{DeviceID: 7, Hostname: "10.1.1.1", Display: "rtr1"}
	nms := &memNMS{devices: map[int]*LibrenmsDevice{7: dev}}
	store := &memStore{rows: map[int]*models.LibrenmsPendingDelete{}}
	rep := &recReporter{}

	// days=0 is clamped to 30, but even a due-now schedule on first sight
	// goes through the "no existing row" path and only quarantines.
	_, deleted, err := applyDeletePolicy(true, 0, []pendingCandidate{{Device: dev, Reason: pendingReasonNoMatch}}, nil, map[int]bool{7: true}, nil, now, nms, store, rep)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("same-run delete happened, deleted=%d", deleted)
	}
	if _, ok := nms.devices[7]; !ok {
		t.Fatal("device was deleted on first observation")
	}
}
