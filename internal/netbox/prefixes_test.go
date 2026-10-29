package netbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
)

type fakePrefixAPI struct {
	prefixes []nbPrefixREST
}

func (f *fakePrefixAPI) client(t *testing.T) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/prefixes/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": f.prefixes, "next": nil})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func TestSyncPrefixes_DefaultNamespaceAndVRF(t *testing.T) {
	db := newImportTestDB(t)
	vrfs := &fakeVRFAPI{vrfs: []nbVRFREST{{ID: 10, Name: "CUST-A", Description: "customer"}}}
	if err := syncVRFs(db, vrfs.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}

	fake := &fakePrefixAPI{prefixes: []nbPrefixREST{
		{ID: 1, Prefix: "10.0.0.1/8", Description: "aggregate"},
		{ID: 2, Prefix: "10.1.0.0/24", Description: "customer", VRF: &nbPrefixVRF{ID: 10, Name: "CUST-A"}},
		{ID: 3, Prefix: "10.1.0.0/24"},
		{ID: 4, Prefix: "2001:db8::1/32", VRF: &nbPrefixVRF{ID: 10, Name: "CUST-A"}},
		{ID: 5, Prefix: "192.0.2.0/24", VRF: &nbPrefixVRF{ID: 99, Name: "missing"}},
		{ID: 6, Prefix: "not-a-prefix"},
	}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}

	var rows []models.IpamPrefix
	if err := db.Order("prefix").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("prefixes = %+v, want 4", rows)
	}
	for _, row := range rows {
		if row.Source != models.VRFSourceNetbox {
			t.Fatalf("source = %q", row.Source)
		}
	}
	var global models.IpamPrefix
	if err := db.Where("prefix = ? AND netbox_id = ?", "10.0.0.0/8", 1).First(&global).Error; err != nil {
		t.Fatal(err)
	}
	var def models.IpamVRF
	if err := db.Where("is_default = ? AND namespace_id = ?", true, global.NamespaceID).First(&def).Error; err != nil {
		t.Fatal(err)
	}
	if global.VRFID != def.ID || global.Description != "aggregate" || global.Family != 4 {
		t.Fatalf("global = %+v, default vrf %d", global, def.ID)
	}
	var globalNS models.IpamNamespace
	if err := db.First(&globalNS, global.NamespaceID).Error; err != nil {
		t.Fatal(err)
	}
	if globalNS.Name != "Netbox" {
		t.Fatalf("global namespace = %q", globalNS.Name)
	}
	var custVRF models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&custVRF).Error; err != nil {
		t.Fatal(err)
	}
	var same []models.IpamPrefix
	if err := db.Where("prefix = ?", "10.1.0.0/24").Find(&same).Error; err != nil {
		t.Fatal(err)
	}
	if len(same) != 2 {
		t.Fatalf("10.1.0.0/24 rows = %+v, want global and VRF", same)
	}
	sawGlobalCIDR, sawVRFCIDR := false, false
	for _, row := range same {
		if row.NetboxID == 3 && row.VRFID == def.ID && row.NamespaceID == def.NamespaceID {
			sawGlobalCIDR = true
		}
		if row.NetboxID == 2 && row.VRFID == custVRF.ID && row.NamespaceID == custVRF.NamespaceID && row.NamespaceID == def.NamespaceID {
			sawVRFCIDR = true
		}
	}
	if !sawGlobalCIDR || !sawVRFCIDR {
		t.Fatalf("10.1.0.0/24 = %+v", same)
	}
	var v6 models.IpamPrefix
	if err := db.Where("prefix = ?", "2001:db8::/32").First(&v6).Error; err != nil {
		t.Fatal(err)
	}
	if v6.Family != 6 || v6.VRFID != custVRF.ID {
		t.Fatalf("ipv6 = %+v", v6)
	}

	roots, err := ipam.Roots(db)
	if err != nil {
		t.Fatal(err)
	}
	var nbNode *ipam.TreeNode
	for i := range roots {
		if roots[i].Type == "namespace" && roots[i].Title == "Netbox" {
			nbNode = &roots[i]
		}
		if roots[i].Type == "allocated" {
			t.Fatalf("prefix leaked to the forest root: %+v", roots[i])
		}
	}
	if nbNode == nil {
		t.Fatalf("forest = %+v", roots)
	}
	var sawGlobal bool
	var vrfNode *ipam.TreeNode
	for i := range nbNode.Children {
		ch := &nbNode.Children[i]
		if ch.Type == "allocated" && ch.Title == "10.0.0.0/8" {
			sawGlobal = true
			if ch.Data.Source != models.VRFSourceNetbox || ch.Data.VRFName != "" {
				t.Fatalf("root prefix = %+v", ch.Data)
			}
		}
		if ch.Type == "vrf" && ch.Title == "CUST-A" {
			vrfNode = ch
		}
	}
	if !sawGlobal || vrfNode == nil {
		t.Fatalf("Netbox children = %+v", nbNode.Children)
	}
	kids, err := ipam.Children(db, vrfNode.Key)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, k := range kids {
		got[k.Title] = true
		if k.Data.Source != models.VRFSourceNetbox || k.Data.VRFName != "CUST-A" {
			t.Fatalf("vrf child = %+v", k.Data)
		}
	}
	if !got["10.1.0.0/24"] || !got["2001:db8::/32"] {
		t.Fatalf("vrf children = %+v", kids)
	}

	if _, err = ipam.UpdatePrefix(db, global.NamespaceID, global.ID, "edited", ipam.PrefixDHCP{}); err != nil {
		t.Fatal(err)
	}
	var se *ipam.StatusError
	if err := ipam.DeletePrefix(db, global.NamespaceID, global.ID); !errors.As(err, &se) || se.Status != 403 {
		t.Fatalf("delete = %v", err)
	}

	// Idempotent description update. The global duplicate stays skipped.
	fake.prefixes[0].Description = "aggregate-2"
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	var updated models.IpamPrefix
	if err := db.First(&updated, global.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Description != "aggregate-2" || updated.NetboxID != 1 {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSyncPrefixes_KeepsFactumPrefixAndDeletesMissing(t *testing.T) {
	db := newImportTestDB(t)
	local, err := ipam.Allocate(db, 0, 0, "192.0.2.0/24", "local", ipam.PrefixDHCP{})
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakePrefixAPI{prefixes: []nbPrefixREST{
		{ID: 1, Prefix: "192.0.2.0/24", Description: "from netbox"},
		{ID: 2, Prefix: "198.51.100.0/24"},
		{ID: 3, Prefix: "203.0.113.0/24"},
	}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	var clash models.IpamPrefix
	if err := db.First(&clash, local.ID).Error; err != nil {
		t.Fatal(err)
	}
	if clash.Source != models.VRFSourceFactum || clash.NetboxID != 0 || clash.Description != "local" {
		t.Fatalf("local overwritten: %+v", clash)
	}

	var drop models.IpamPrefix
	if err := db.Where("netbox_id = ?", 3).First(&drop).Error; err != nil {
		t.Fatal(err)
	}
	addr := models.Address{Address: "203.0.113.1/24", PrefixID: &drop.ID}
	if err := db.Create(&addr).Error; err != nil {
		t.Fatal(err)
	}

	fake.prefixes = []nbPrefixREST{{ID: 2, Prefix: "198.51.100.0/24", Description: "kept"}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("netbox_id = ?", 1).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("skipped netbox id 1 should not have been stored")
	}
	if err := db.Where("netbox_id = ?", 3).First(&drop).Error; err != nil {
		t.Fatal("prefix with an address should have been kept")
	}
	var kept models.IpamPrefix
	if err := db.Where("netbox_id = ?", 2).First(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if kept.Description != "kept" {
		t.Fatalf("kept = %+v", kept)
	}

	fake.prefixes = nil
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.IpamPrefix{}).Where("source = ?", models.VRFSourceNetbox).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("netbox prefixes after empty fetch = %d, want 2", n)
	}
}

func TestSyncPrefixes_ReplacesGoneNetboxID(t *testing.T) {
	db := newImportTestDB(t)
	fake := &fakePrefixAPI{prefixes: []nbPrefixREST{{ID: 3, Prefix: "10.1.0.0/24"}}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	fake.prefixes = []nbPrefixREST{{ID: 2, Prefix: "10.1.0.0/24", VRF: &nbPrefixVRF{ID: 10, Name: "CUST-A"}}}
	vrfs := &fakeVRFAPI{vrfs: []nbVRFREST{{ID: 10, Name: "CUST-A"}}}
	if err := syncVRFs(db, vrfs.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	var rows []models.IpamPrefix
	if err := db.Where("prefix = ?", "10.1.0.0/24").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].NetboxID != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	var vrf models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&vrf).Error; err != nil {
		t.Fatal(err)
	}
	if rows[0].VRFID != vrf.ID {
		t.Fatalf("vrf = %d, want %d", rows[0].VRFID, vrf.ID)
	}
}

func TestSyncPrefixes_UniqueIPSpace(t *testing.T) {
	db := newImportTestDB(t)
	off := false
	on := true
	vrfs := &fakeVRFAPI{vrfs: []nbVRFREST{
		{ID: 1, Name: "OPEN", EnforceUnique: &off},
		{ID: 2, Name: "CLOSED", EnforceUnique: &on},
	}}
	if err := syncVRFs(db, vrfs.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	fake := &fakePrefixAPI{prefixes: []nbPrefixREST{
		{ID: 11, Prefix: "10.0.0.0/24", VRF: &nbPrefixVRF{ID: 1, Name: "OPEN"}},
		{ID: 12, Prefix: "10.0.0.0/24", VRF: &nbPrefixVRF{ID: 1, Name: "OPEN"}},
		{ID: 21, Prefix: "10.0.0.0/24", VRF: &nbPrefixVRF{ID: 2, Name: "CLOSED"}},
		{ID: 22, Prefix: "10.0.0.0/24", VRF: &nbPrefixVRF{ID: 2, Name: "CLOSED"}},
		{ID: 31, Prefix: "10.0.0.0/24"},
		{ID: 32, Prefix: "10.0.0.0/24"},
	}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}

	var rows []models.IpamPrefix
	if err := db.Where("prefix = ?", "10.0.0.0/24").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %+v, want two in OPEN, one in CLOSED, one global", rows)
	}
	var open, closed models.IpamVRF
	if err := db.Where("netbox_id = ?", 1).First(&open).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("netbox_id = ?", 2).First(&closed).Error; err != nil {
		t.Fatal(err)
	}
	if open.EnforceUnique || !closed.EnforceUnique {
		t.Fatalf("open=%v closed=%v", open.EnforceUnique, closed.EnforceUnique)
	}
	counts := map[uint]int{}
	var nsID uint
	for _, row := range rows {
		counts[row.VRFID]++
		if nsID == 0 {
			nsID = row.NamespaceID
		}
		if row.NamespaceID != nsID {
			t.Fatalf("prefix left the Netbox namespace: %+v", row)
		}
	}
	var def models.IpamVRF
	if err := db.Where("namespace_id = ? AND is_default = ?", nsID, true).First(&def).Error; err != nil {
		t.Fatal(err)
	}
	if counts[open.ID] != 2 || counts[closed.ID] != 1 || counts[def.ID] != 1 {
		t.Fatalf("counts open=%d closed=%d global=%d", counts[open.ID], counts[closed.ID], counts[def.ID])
	}

	kids, err := ipam.Children(db, "vrf:"+strconv.FormatUint(uint64(open.ID), 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 {
		t.Fatalf("OPEN children = %+v", kids)
	}
}

func TestSyncPrefixes_SkipsFactumPrefixInSameVRF(t *testing.T) {
	db := newImportTestDB(t)
	vrfs := &fakeVRFAPI{vrfs: []nbVRFREST{{ID: 10, Name: "CUST-A"}}}
	if err := syncVRFs(db, vrfs.client(t), jobevent.NewSlogReporter("test", "vrfs")); err != nil {
		t.Fatal(err)
	}
	var vrf models.IpamVRF
	if err := db.Where("netbox_id = ?", 10).First(&vrf).Error; err != nil {
		t.Fatal(err)
	}
	local, err := ipam.Allocate(db, vrf.NamespaceID, vrf.ID, "192.0.2.0/24", "local", ipam.PrefixDHCP{})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakePrefixAPI{prefixes: []nbPrefixREST{
		{ID: 1, Prefix: "192.0.2.0/24", VRF: &nbPrefixVRF{ID: 10, Name: "CUST-A"}},
		{ID: 2, Prefix: "192.0.2.0/24"},
	}}
	if err := syncPrefixes(db, fake.client(t), jobevent.NewSlogReporter("test", "prefixes")); err != nil {
		t.Fatal(err)
	}
	var kept models.IpamPrefix
	if err := db.First(&kept, local.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.Source != models.VRFSourceFactum || kept.NetboxID != 0 {
		t.Fatalf("local overwritten: %+v", kept)
	}
	var n int64
	if err := db.Model(&models.IpamPrefix{}).Where("netbox_id = ?", 1).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("NetBox prefix should not replace the Factum prefix in the same VRF")
	}
	if err := db.Model(&models.IpamPrefix{}).Where("netbox_id = ?", 2).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the same CIDR in the global table should be stored")
	}
}
