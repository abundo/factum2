package netbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
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
	var custNS models.IpamNamespace
	if err := db.First(&custNS, got.NamespaceID).Error; err != nil {
		t.Fatal(err)
	}
	if custNS.Name != "Netbox" {
		t.Fatalf("CUST-A namespace = %q, want Netbox", custNS.Name)
	}
	var emptyNS models.IpamNamespace
	if err := db.Where("name = ?", "").First(&emptyNS).Error; err != nil {
		t.Fatal(err)
	}
	if got.NamespaceID == emptyNS.ID {
		t.Fatal("NetBox VRF was stored in the empty namespace")
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
		if n.Type == "namespace" && n.Title == "Netbox" {
			sawCust = true
			if len(n.Children) != 1 || n.Children[0].Type != "vrf" || n.Children[0].Title != "CUST-A" {
				t.Fatalf("CUST-A children = %+v", n.Children)
			}
			if n.Children[0].Data.Source != models.VRFSourceNetbox || n.Children[0].Data.RD != "65000:1" {
				t.Fatalf("tree node = %+v", n.Children[0].Data)
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
	var dropNS int64
	if err := db.Model(&models.IpamNamespace{}).Where("name = ?", "drop").Count(&dropNS).Error; err != nil {
		t.Fatal(err)
	}
	if dropNS != 0 {
		t.Fatal("empty namespace for a deleted VRF should have been removed")
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

func TestSyncVRFs_SkipsNameUsedInAnotherNamespace(t *testing.T) {
	db := newImportTestDB(t)
	ns, err := ipam.CreateNamespace(db, "core", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ipam.CreateVRF(db, ns.ID, ipam.VRFWrite{Name: "CUST-A"}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeVRFAPI{vrfs: []nbVRFREST{{ID: 10, Name: "CUST-A"}}}
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	var n int64
	if err := db.Model(&models.IpamVRF{}).Where("netbox_id = ?", 10).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("imported colliding NetBox VRF, count=%d", n)
	}
}

func TestSyncVRFs_MovesLegacyNamespaceIntoNetbox(t *testing.T) {
	db := newImportTestDB(t)
	legacy, err := ipam.CreateNamespace(db, "CUST-A", "")
	if err != nil {
		t.Fatal(err)
	}
	vrf := models.IpamVRF{
		NamespaceID: legacy.ID,
		Name:        "CUST-A",
		Source:      models.VRFSourceNetbox,
		NetboxID:    10,
	}
	if err := db.Create(&vrf).Error; err != nil {
		t.Fatal(err)
	}
	pfx := models.IpamPrefix{
		NamespaceID: legacy.ID,
		VRFID:       vrf.ID,
		Prefix:      "10.9.0.0/24",
		Family:      4,
		Source:      models.VRFSourceNetbox,
		NetboxID:    1,
	}
	if err := db.Create(&pfx).Error; err != nil {
		t.Fatal(err)
	}

	fake := &fakeVRFAPI{vrfs: []nbVRFREST{{ID: 10, Name: "CUST-A"}}}
	if err := syncVRFs(db, fake.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	var got models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	var ns models.IpamNamespace
	if err := db.First(&ns, got.NamespaceID).Error; err != nil {
		t.Fatal(err)
	}
	if ns.Name != "Netbox" || ns.ID == legacy.ID {
		t.Fatalf("namespace = %+v", ns)
	}
	var moved models.IpamPrefix
	if err := db.First(&moved, pfx.ID).Error; err != nil {
		t.Fatal(err)
	}
	if moved.NamespaceID != ns.ID {
		t.Fatalf("prefix namespace = %d, want %d", moved.NamespaceID, ns.ID)
	}
	var left int64
	if err := db.Model(&models.IpamNamespace{}).Where("name = ?", "CUST-A").Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatal("legacy per-VRF namespace should have been removed")
	}
}

func TestSplitRouteTargets(t *testing.T) {
	got := SplitRouteTargets(" 65000:1, 65000:2;65000:1\n65000:3 ")
	want := []string{"65000:1", "65000:2", "65000:3"}
	if len(got) != len(want) {
		t.Fatalf("split = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("split = %#v", got)
		}
	}
	if SplitRouteTargets("") != nil && len(SplitRouteTargets("")) != 0 {
		t.Fatalf("empty = %#v", SplitRouteTargets(""))
	}
}

func TestSyncVRFs_ResolvesRouteTargetIDs(t *testing.T) {
	db := newImportTestDB(t)
	const list = `{"results":[{"id":10,"name":"CUST-A","rd":"65000:1","description":"customer",
		"import_targets":[7],
		"export_targets":[{"id":8,"display":"65000:8"},{"id":9,"name":"65000:9"}]}],"next":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/ipam/vrfs/":
			_, _ = w.Write([]byte(list))
		case "/api/ipam/route-targets/7/":
			_, _ = w.Write([]byte(`{"id":7,"name":"65000:7"}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := syncVRFs(db, nb, jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	var got models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.RD != "65000:1" || got.ImportRT != "65000:7" || got.ExportRT != "65000:8, 65000:9" {
		t.Fatalf("rd/rt = rd=%q import=%q export=%q", got.RD, got.ImportRT, got.ExportRT)
	}
}
