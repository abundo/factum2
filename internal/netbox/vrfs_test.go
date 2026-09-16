package netbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
)

type fakeVRFAPI struct {
	vrfs []nbVRFREST
}

func (f *fakeVRFAPI) client(t *testing.T) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func (f *fakeVRFAPI) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/vrfs/") {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": f.vrfs, "next": nil})
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"detail":"not found"}`))
}

func rdPtr(s string) *string { return &s }

func TestSyncVRFs_ImportsIntoEmptyNamespace(t *testing.T) {
	db := newImportTestDB(t)
	local, err := ipam.CreateVRF(db, 0, ipam.VRFWrite{Name: "local-only", Description: "factum"})
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeVRFAPI{vrfs: []nbVRFREST{
		{
			ID: 10, Name: "CUST-A", RD: rdPtr("65000:1"), Description: "customer a",
			ImportTargets: []nbRouteTargetRef{{Name: "65000:1"}},
			ExportTargets: []nbRouteTargetRef{{Name: "65000:1"}, {Name: "65000:2"}},
		},
		{ID: 11, Name: "default", RD: rdPtr("65000:0")},
		{ID: 12, Name: "local-only"},
	}}

	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	var rows []models.IpamVRF
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byName := map[string]models.IpamVRF{}
	for _, row := range rows {
		byName[row.Name] = row
	}
	if byName["local-only"].ID != local.ID || byName["local-only"].Source != models.VRFSourceFactum {
		t.Fatalf("factum VRF overwritten: %+v", byName["local-only"])
	}
	got := byName["CUST-A"]
	if got.ID == 0 || got.Source != models.VRFSourceNetbox || got.NetboxID != 10 {
		t.Fatalf("CUST-A = %+v", got)
	}
	if got.RD != "65000:1" || got.ImportRT != "65000:1" || got.ExportRT != "65000:1, 65000:2" {
		t.Fatalf("CUST-A rd/rt = rd=%q import=%q export=%q", got.RD, got.ImportRT, got.ExportRT)
	}
	if _, ok := byName["default"]; ok && byName["default"].NetboxID == 11 {
		t.Fatal("NetBox VRF named default was imported")
	}

	roots, err := ipam.Roots(db)
	if err != nil {
		t.Fatal(err)
	}
	var sawCust, sawLocal, sawDefault bool
	for _, n := range roots {
		if n.Type == "vrf" && n.Title == "CUST-A" {
			sawCust = true
			if n.Data.Source != models.VRFSourceNetbox || n.Data.RD != "65000:1" {
				t.Fatalf("tree node = %+v", n.Data)
			}
		}
		if n.Type == "vrf" && n.Title == "local-only" {
			sawLocal = true
		}
		if n.Type == "vrf" && n.Title == "default" {
			sawDefault = true
		}
	}
	if !sawCust || !sawLocal {
		t.Fatalf("forest = %+v", roots)
	}
	if sawDefault {
		t.Fatal("default VRF must not appear as a tree node")
	}

	// Idempotent update of RD/RTs.
	fake.vrfs[0].RD = rdPtr("65000:9")
	fake.vrfs[0].ExportTargets = []nbRouteTargetRef{{Name: "65000:9"}}
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	var updated models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.ID != got.ID || updated.RD != "65000:9" || updated.ExportRT != "65000:9" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSyncVRFs_DeletesMissingAndKeepsPrefixed(t *testing.T) {
	db := newImportTestDB(t)
	fake := &fakeVRFAPI{vrfs: []nbVRFREST{
		{ID: 1, Name: "keep"},
		{ID: 2, Name: "drop"},
		{ID: 3, Name: "has-prefix"},
	}}
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	var hasPrefix models.IpamVRF
	if err := db.Where("netbox_id = ?", 3).First(&hasPrefix).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ipam.Allocate(db, hasPrefix.NamespaceID, hasPrefix.ID, "10.9.0.0/24", "", ipam.PrefixDHCP{}); err != nil {
		t.Fatal(err)
	}

	fake.vrfs = []nbVRFREST{{ID: 1, Name: "keep"}}
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	var rows []models.IpamVRF
	if err := db.Where("source = ?", models.VRFSourceNetbox).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byName := map[string]models.IpamVRF{}
	for _, row := range rows {
		byName[row.Name] = row
	}
	if _, ok := byName["keep"]; !ok {
		t.Fatal("keep missing")
	}
	if _, ok := byName["drop"]; ok {
		t.Fatal("drop should have been deleted")
	}
	if _, ok := byName["has-prefix"]; !ok {
		t.Fatal("VRF with prefixes should have been kept")
	}

	// Empty NetBox list must not wipe remaining synced rows.
	fake.vrfs = nil
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.IpamVRF{}).Where("source = ?", models.VRFSourceNetbox).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("netbox vrfs after empty fetch = %d, want 2", n)
	}
}
