package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func useIPAMNetbox(t *testing.T, ctrl *Controller, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		t.Fatal(err)
	}
	settings.NetboxApiURL = srv.URL
	settings.NetboxApiToken = "t"
	if err := ctrl.DB.Save(settings).Error; err != nil {
		t.Fatal(err)
	}
}

type ipamNetboxFake struct {
	mu       sync.Mutex
	nextRT   uint
	nextVRF  uint
	nextPfx  uint
	targets  map[string]uint
	patched  map[string]any
	created  map[string]any
	prefix   map[string]any
	pfxPatch map[string]any
	failVRF  bool
}

func newIPAMNetboxFake() *ipamNetboxFake {
	return &ipamNetboxFake{
		nextRT:  1,
		nextVRF: 100,
		nextPfx: 500,
		targets: map[string]uint{},
	}
}

func (f *ipamNetboxFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Body != nil && r.Method != http.MethodGet {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/ipam/route-targets/"):
		name := r.URL.Query().Get("name")
		var results []map[string]any
		if id, ok := f.targets[name]; ok {
			results = append(results, map[string]any{"id": id, "name": name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case r.Method == http.MethodPost && r.URL.Path == "/api/ipam/route-targets/":
		name, _ := body["name"].(string)
		if id, ok := f.targets[name]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})
			return
		}
		f.nextRT++
		f.targets[name] = f.nextRT
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.nextRT, "name": name})
	case r.Method == http.MethodPost && r.URL.Path == "/api/ipam/vrfs/":
		if f.failVRF {
			http.Error(w, `{"name":["already exists"]}`, http.StatusBadRequest)
			return
		}
		f.created = body
		f.nextVRF++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.nextVRF})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/ipam/vrfs/"):
		if f.failVRF {
			http.Error(w, `{"rd":["invalid"]}`, http.StatusBadRequest)
			return
		}
		f.patched = body
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42})
	case r.Method == http.MethodPost && r.URL.Path == "/api/ipam/prefixes/":
		f.prefix = body
		f.nextPfx++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.nextPfx})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/ipam/prefixes/"):
		f.pfxPatch = body
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
	}
}

func uintsFrom(v any) []uint {
	raw, _ := v.([]any)
	out := make([]uint, 0, len(raw))
	for _, item := range raw {
		switch n := item.(type) {
		case float64:
			out = append(out, uint(n))
		}
	}
	return out
}

func TestIpamNetboxNamespace_WritesVRFAndPrefix(t *testing.T) {
	ctrl := setupIPAM(t)
	fake := newIPAMNetboxFake()
	useIPAMNetbox(t, ctrl, fake.serve)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "Netbox"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("namespace status = %d body=%s", rec.Code, rec.Body.String())
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	nsID := strconv.FormatUint(uint64(ns.ID), 10)

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{
		Name: "CUST-A", Description: "customer", RD: "65000:1", ImportRT: "65000:1", ExportRT: "65000:1, 65000:2",
	}, []string{"id"}, []string{nsID})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("vrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	var vrf models.IpamVRF
	if err := json.Unmarshal(rec.Body.Bytes(), &vrf); err != nil {
		t.Fatal(err)
	}
	if vrf.Source != models.VRFSourceNetbox || vrf.NetboxID == 0 {
		t.Fatalf("created vrf = %+v", vrf)
	}
	if fake.created["name"] != "CUST-A" || fake.created["rd"] != "65000:1" {
		t.Fatalf("posted vrf = %#v", fake.created)
	}
	if len(uintsFrom(fake.created["import_targets"])) != 1 || len(uintsFrom(fake.created["export_targets"])) != 2 {
		t.Fatalf("posted targets = %#v", fake.created)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/vrfs/x", ipamVRFBody{
		Name: "CUST-A", Description: "customer 2", RD: "65000:9", ImportRT: "65000:9", ExportRT: "65000:1",
	}, []string{"id", "vrfId"}, []string{nsID, strconv.FormatUint(uint64(vrf.ID), 10)})
	if err := ctrl.ApiIpamVRFUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("vrf update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.patched["rd"] != "65000:9" || fake.patched["description"] != "customer 2" {
		t.Fatalf("patched vrf = %#v", fake.patched)
	}
	if len(uintsFrom(fake.patched["import_targets"])) != 1 || len(uintsFrom(fake.patched["export_targets"])) != 1 {
		t.Fatalf("patched targets = %#v", fake.patched)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.9.0.0/24", VRFID: vrf.ID, Description: "pool",
	}, []string{"id"}, []string{nsID})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("prefix status = %d body=%s", rec.Code, rec.Body.String())
	}
	var pfx models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &pfx); err != nil {
		t.Fatal(err)
	}
	if pfx.Source != models.VRFSourceNetbox || pfx.NetboxID == 0 {
		t.Fatalf("created prefix = %+v", pfx)
	}
	if fake.prefix["prefix"] != "10.9.0.0/24" || fake.prefix["description"] != "pool" {
		t.Fatalf("posted prefix = %#v", fake.prefix)
	}
	vrfPosted, _ := fake.prefix["vrf"].(float64)
	if uint(vrfPosted) != vrf.NetboxID {
		t.Fatalf("prefix vrf = %v, want %d", fake.prefix["vrf"], vrf.NetboxID)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/prefixes/x", ipamPrefixBody{
		Prefix: "10.9.0.0/24", VRFID: vrf.ID, Description: "pool 2",
	}, []string{"id", "prefixId"}, []string{nsID, strconv.FormatUint(uint64(pfx.ID), 10)})
	if err := ctrl.ApiIpamPrefixUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("prefix update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.pfxPatch["description"] != "pool 2" {
		t.Fatalf("patched prefix = %#v", fake.pfxPatch)
	}

	// A VRF outside the Netbox namespace is not written to NetBox.
	fake.created = nil
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "local-only"}, []string{"id"}, []string{"0"})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("local vrf status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.created != nil {
		t.Fatalf("local VRF was posted to NetBox: %#v", fake.created)
	}

	// A failed NetBox write leaves the Factum row unchanged.
	fake.failVRF = true
	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/vrfs/x", ipamVRFBody{
		Name: "CUST-A", Description: "nope", RD: "65000:8",
	}, []string{"id", "vrfId"}, []string{nsID, strconv.FormatUint(uint64(vrf.ID), 10)})
	if err := ctrl.ApiIpamVRFUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("failed update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var kept models.IpamVRF
	if err := ctrl.DB.First(&kept, vrf.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.Description != "customer 2" || kept.RD != "65000:9" {
		t.Fatalf("VRF changed after NetBox failure: %+v", kept)
	}
}
