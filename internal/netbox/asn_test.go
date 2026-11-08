package netbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netboxtool"
	"github.com/abundo/factum2/models"
)

type fakeASNAPI struct {
	ranges  []nbASNRangeREST
	asns    []nbASNREST
	targets []nbRouteTargetREST
}

func (f *fakeASNAPI) client(t *testing.T) *netboxtool.NetboxClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/asn-ranges/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"results": f.ranges, "next": nil})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/asns/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"results": f.asns, "next": nil})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/route-targets/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"results": f.targets, "next": nil})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	nb, err := netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{URL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func TestSyncASNAndRouteTargets(t *testing.T) {
	db := newImportTestDB(t)
	rir := &nbBrief{ID: 1, Name: "RFC 6996"}
	tenant := &nbBrief{ID: 2, Name: "Acme"}
	fake := &fakeASNAPI{
		ranges: []nbASNRangeREST{
			{ID: 4, Name: "Private 16-bit", Slug: "private-16-bit", RIR: rir, Start: 64512, End: 65534, Tenant: tenant, Description: "docs"},
			{ID: 5, Name: "", Start: 1, End: 2},
			{ID: 6, Name: "Backwards", Start: 20, End: 10},
		},
		asns: []nbASNREST{
			{ID: 8, ASN: 65000, RIR: rir, Role: &nbBrief{Name: "customer"}, Tenant: tenant, Description: "lab", Sites: []nbBrief{{Name: "STO"}, {ID: 9}}},
			{ID: 9, ASN: 0},
		},
		targets: []nbRouteTargetREST{
			{ID: 3, Name: "65000:100", Tenant: tenant, Description: "cust"},
			{ID: 1, Name: "  "},
		},
	}
	rep := jobevent.NewSlogReporter("test", "asn")
	nb := fake.client(t)
	if err := syncASNRanges(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncASNs(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncRouteTargets(db, nb, rep); err != nil {
		t.Fatal(err)
	}

	var ranges []models.IpamASNRange
	if err := db.Find(&ranges).Error; err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges[0].Name != "Private 16-bit" || ranges[0].Start != 64512 || ranges[0].End != 65534 || ranges[0].RIR != "RFC 6996" || ranges[0].Tenant != "Acme" || ranges[0].NetboxID != 4 {
		t.Fatalf("ranges = %+v", ranges)
	}
	var asns []models.IpamASN
	if err := db.Find(&asns).Error; err != nil {
		t.Fatal(err)
	}
	if len(asns) != 1 || asns[0].ASN != 65000 || asns[0].Role != "customer" || asns[0].Sites != "STO" || asns[0].Source != models.VRFSourceNetbox {
		t.Fatalf("asns = %+v", asns)
	}
	var targets []models.IpamRouteTarget
	if err := db.Find(&targets).Error; err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Name != "65000:100" || targets[0].Description != "cust" {
		t.Fatalf("targets = %+v", targets)
	}

	local := models.IpamASN{ASN: 64496, Description: "local", Source: models.VRFSourceFactum}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}

	fake.ranges[0].Description = "renamed"
	fake.asns = []nbASNREST{{ID: 8, ASN: 65001, RIR: &nbBrief{ID: 1}, Description: "lab"}}
	fake.targets = []nbRouteTargetREST{{ID: 7, Name: "65000:200"}}
	nb = fake.client(t)
	if err := syncASNRanges(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncASNs(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncRouteTargets(db, nb, rep); err != nil {
		t.Fatal(err)
	}

	var gotRange models.IpamASNRange
	if err := db.Where("netbox_id = ?", 4).First(&gotRange).Error; err != nil {
		t.Fatal(err)
	}
	if gotRange.Description != "renamed" {
		t.Fatalf("description = %q", gotRange.Description)
	}
	var gotASN models.IpamASN
	if err := db.Where("netbox_id = ?", 8).First(&gotASN).Error; err != nil {
		t.Fatal(err)
	}
	if gotASN.ASN != 65001 || gotASN.RIR != "" || gotASN.Sites != "" || gotASN.Role != "" {
		t.Fatalf("updated asn = %+v", gotASN)
	}
	if err := db.Where("source = ? AND netbox_id = 0", models.VRFSourceFactum).First(&models.IpamASN{}).Error; err != nil {
		t.Fatal(err)
	}
	var left []models.IpamRouteTarget
	if err := db.Where("source = ?", models.VRFSourceNetbox).Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].NetboxID != 7 || left[0].Name != "65000:200" {
		t.Fatalf("targets after delete = %+v", left)
	}

	fake.ranges = nil
	fake.asns = nil
	fake.targets = nil
	nb = fake.client(t)
	if err := syncASNRanges(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncASNs(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	if err := syncRouteTargets(db, nb, rep); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.IpamASNRange{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("empty range fetch deleted rows: %d", n)
	}
	if err := db.Model(&models.IpamASN{}).Where("netbox_id = ?", 8).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("empty asn fetch deleted the mirror")
	}
	if err := db.Model(&models.IpamRouteTarget{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("empty route-target fetch deleted rows: %d", n)
	}
}

func TestDeltaPlanASNAndRouteTarget(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	plan, err := newDeltaPlan([]netboxtool.ObjectChange{
		{ID: 1, Time: t0, Action: "create", ChangedType: "ipam.asnrange", ChangedID: 4},
		{ID: 2, Time: t0.Add(time.Second), Action: "update", ChangedType: "ipam.asn", ChangedID: 8},
		{ID: 3, Time: t0.Add(2 * time.Second), Action: "delete", ChangedType: "ipam.routetarget", ChangedID: 3},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.asnRanges || !plan.asns || !plan.routeTargets {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.vrfs || plan.prefixes {
		t.Fatal("asn changes should not refresh VRFs or prefixes")
	}
}
