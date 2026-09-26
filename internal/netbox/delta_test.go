package netbox

import (
	"testing"
	"time"

	"github.com/abundo/factum2/internal/netboxtool"
)

func TestPreferFullSync(t *testing.T) {
	cases := []struct {
		pending, known int
		full           bool
	}{
		{pending: 9, known: 9, full: false},
		{pending: 2, known: 4, full: false},
		{pending: 10, known: 50, full: true},
		{pending: 10, known: 51, full: false},
		{pending: 10, known: 0, full: true},
		{pending: 9, known: 0, full: false},
		{pending: 30, known: 100, full: true},
		{pending: 100, known: 1000, full: false},
	}
	for _, tc := range cases {
		got := preferFullSync(tc.pending, tc.known)
		if got != tc.full {
			t.Errorf("preferFullSync(%d, %d) = %v, want %v", tc.pending, tc.known, got, tc.full)
		}
	}
}

func TestDeltaPlanDevicesInterfacesAndDeletes(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	plan, err := newDeltaPlan([]netboxtool.ObjectChange{
		{ID: 1, Time: t0, Action: "update", ChangedType: "dcim.interface", ChangedID: 9, RelatedType: "dcim.device", RelatedID: 4},
		{ID: 2, Time: t0.Add(time.Second), Action: "delete", ChangedType: "dcim.device", ChangedID: 4},
		{ID: 3, Time: t0.Add(2 * time.Second), Action: "update", ChangedType: "virtualization.vminterface", ChangedID: 3, RelatedType: "virtualization.virtualmachine", RelatedID: 8},
		{ID: 4, Time: t0.Add(3 * time.Second), Action: "create", ChangedType: "dcim.cable", ChangedID: 15},
		{ID: 5, Time: t0.Add(4 * time.Second), Action: "delete", ChangedType: "dcim.cable", ChangedID: 15},
		{ID: 6, Time: t0.Add(5 * time.Second), Action: "update", ChangedType: "ipam.vrf", ChangedID: 2},
		{ID: 7, Time: t0.Add(6 * time.Second), Action: "update", ChangedType: "ipam.ipaddress", ChangedID: 20, RelatedType: "dcim.interface", RelatedID: 30},
	}, func(vmIface bool, ifaceID uint) (uint, bool, error) {
		if vmIface || ifaceID != 30 {
			return 0, false, nil
		}
		return 7, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.devices[4]; ok {
		t.Fatal("device 4 was deleted after its interface edit and should not be refetched")
	}
	if _, ok := plan.delDevices[4]; !ok {
		t.Fatal("device 4 should be deleted")
	}
	if _, ok := plan.vms[8]; !ok {
		t.Fatal("VM 8 should be refetched from its interface change")
	}
	if plan.cables[15] != "delete" {
		t.Fatalf("cable 15 action = %q, want delete", plan.cables[15])
	}
	if !plan.vrfs {
		t.Fatal("vrf change should be planned")
	}
	if _, ok := plan.devices[7]; !ok {
		t.Fatal("IP address change should refetch device 7")
	}
}

func TestAfterCursorSkipsAppliedRow(t *testing.T) {
	at := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	same := netboxtool.ObjectChange{ID: 5, Time: at}
	newerID := netboxtool.ObjectChange{ID: 6, Time: at}
	later := netboxtool.ObjectChange{ID: 1, Time: at.Add(time.Microsecond)}
	if afterCursor(same, at, 5) {
		t.Fatal("cursor row should be skipped")
	}
	if !afterCursor(newerID, at, 5) || !afterCursor(later, at, 5) {
		t.Fatal("later id or time should be included")
	}
}
