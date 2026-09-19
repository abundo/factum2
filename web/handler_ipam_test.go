package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/internal/ipam"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

func setupIPAM(t *testing.T) *Controller {
	t.Helper()
	db := newTestDB(t)
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	on := true
	s.IpamEnabled = &on
	if err := db.Save(s).Error; err != nil {
		t.Fatalf("enable ipam: %v", err)
	}
	return &Controller{DB: db}
}

func TestRequireIpamEnabled_OffIs404(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	c, rec := jsonRequest(t, http.MethodGet, "/api/ipam/namespaces", nil, nil, nil)
	called := false
	err := ctrl.RequireIpamEnabled(func(c *echo.Context) error {
		called = true
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if called {
		t.Fatal("next ran while IPAM is disabled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireIpamEnabled_OnPasses(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodGet, "/api/ipam/namespaces", nil, nil, nil)
	err := ctrl.RequireIpamEnabled(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	})(c)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestIpamNamespaceLifecycle(t *testing.T) {
	ctrl := setupIPAM(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "global", Description: "prod"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ns.ID == 0 || ns.Name != "global" {
		t.Fatalf("unexpected namespace: %+v", ns)
	}
	if len(ns.VRFs) != 1 || !ns.VRFs[0].IsDefault || ns.VRFs[0].Name != "default" {
		t.Fatalf("expected default VRF, got %+v", ns.VRFs)
	}

	id := strconv.FormatUint(uint64(ns.ID), 10)
	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/namespaces/x", nil, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamNamespaceGet(c); err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "global"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatalf("dup create: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
}

func TestIpamAllocateAndTree(t *testing.T) {
	ctrl := setupIPAM(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "core"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatalf("create ns: %v", err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)
	defaultVRF := ns.VRFs[0].ID

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "cust-a"}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatalf("vrf: %v", err)
	}
	var custA models.IpamVRF
	if err := json.Unmarshal(rec.Body.Bytes(), &custA); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/16", VRFID: defaultVRF, Description: "core",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatalf("alloc: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("alloc status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Child in same VRF is fine.
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.1.0/24", VRFID: defaultVRF,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatalf("child: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("child status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Same space in another VRF is not.
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.1.0/24", VRFID: custA.ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("reuse status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	// Unrelated prefix in the root is fine (no allowed-prefix fence).
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "192.168.0.0/16", VRFID: defaultVRF,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatalf("root sibling: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("root sibling status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatalf("forest: %v", err)
	}
	var forest []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &forest); err != nil {
		t.Fatal(err)
	}
	if len(forest) != 1 {
		t.Fatalf("forest = %+v", forest)
	}
	var sawRoot, sawVRF bool
	for _, ch := range forest[0].Children {
		if ch.Type == "allocated" && ch.Title == "10.0.0.0/16" {
			sawRoot = true
			if ch.Data.VRFName != "" {
				t.Errorf("root prefix VRF name = %q, want empty", ch.Data.VRFName)
			}
		}
		if ch.Type == "vrf" && ch.Title == "cust-a" {
			sawVRF = true
		}
		if ch.Type == "vrf" && ch.Title == "default" {
			t.Fatal("default VRF must not appear as a tree node")
		}
	}
	if !sawRoot || !sawVRF {
		t.Fatalf("ns children = %+v", forest[0].Children)
	}
}

func TestIpamDeleteRules(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "lab"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)

	c, rec = jsonRequest(t, http.MethodDelete, "/api/ipam/namespaces/x/vrfs/x", nil,
		[]string{"id", "vrfId"}, []string{id, strconv.FormatUint(uint64(ns.VRFs[0].ID), 10)})
	if err := ctrl.ApiIpamVRFDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete default VRF status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/24", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/ipam/namespaces/x", nil, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamNamespaceDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete ns with allocs status = %d, want 409", rec.Code)
	}
}

func TestIpamVRFNameIsGloballyUnique(t *testing.T) {
	ctrl := setupIPAM(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "ns-a"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var nsA ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &nsA); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "ns-b"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var nsB ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &nsB); err != nil {
		t.Fatal(err)
	}

	idA := strconv.FormatUint(uint64(nsA.ID), 10)
	idB := strconv.FormatUint(uint64(nsB.ID), 10)
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "cust-a"}, []string{"id"}, []string{idA})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "cust-a"}, []string{"id"}, []string{idB})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup across namespaces status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "default"}, []string{"id"}, []string{idA})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reserved name status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestIpamVRFRouteTargetsAndNetboxReadonly(t *testing.T) {
	ctrl := setupIPAM(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{
		Name: "cust-a", Description: "a", RD: "65000:1", ImportRT: "65000:1", ExportRT: "65000:2",
	}, []string{"id"}, []string{"0"})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var local models.IpamVRF
	if err := json.Unmarshal(rec.Body.Bytes(), &local); err != nil {
		t.Fatal(err)
	}
	if local.RD != "65000:1" || local.ImportRT != "65000:1" || local.ExportRT != "65000:2" {
		t.Fatalf("created = %+v", local)
	}
	if local.Source != models.VRFSourceFactum {
		t.Fatalf("source = %q, want factum", local.Source)
	}

	nsID := strconv.FormatUint(uint64(local.NamespaceID), 10)
	vrfID := strconv.FormatUint(uint64(local.ID), 10)
	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/vrfs/x", ipamVRFBody{
		Name: "cust-a", Description: "a2", RD: "65000:9", ImportRT: "65000:9", ExportRT: "65000:9",
	}, []string{"id", "vrfId"}, []string{nsID, vrfID})
	if err := ctrl.ApiIpamVRFUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &local); err != nil {
		t.Fatal(err)
	}
	if local.Description != "a2" || local.RD != "65000:9" {
		t.Fatalf("updated = %+v", local)
	}

	nb := models.IpamVRF{
		NamespaceID: local.NamespaceID,
		Name:        "from-netbox",
		RD:          "1:1",
		ImportRT:    "1:1",
		ExportRT:    "1:1",
		Source:      models.VRFSourceNetbox,
		NetboxID:    42,
	}
	if err := ctrl.DB.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	nbID := strconv.FormatUint(uint64(nb.ID), 10)
	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/vrfs/x", ipamVRFBody{
		Name: "from-netbox", Description: "nope",
	}, []string{"id", "vrfId"}, []string{nsID, nbID})
	if err := ctrl.ApiIpamVRFUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("netbox update status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodDelete, "/api/ipam/namespaces/x/vrfs/x", nil,
		[]string{"id", "vrfId"}, []string{nsID, nbID})
	if err := ctrl.ApiIpamVRFDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("netbox delete status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var roots []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	var sawLocal, sawNB bool
	for _, n := range roots {
		if n.Type == "vrf" && n.Title == "cust-a" {
			sawLocal = true
			if n.Data.RD != "65000:9" {
				t.Fatalf("local tree rd = %q", n.Data.RD)
			}
		}
		if n.Type == "vrf" && n.Title == "from-netbox" {
			sawNB = true
			if n.Data.Source != models.VRFSourceNetbox {
				t.Fatalf("netbox tree source = %q", n.Data.Source)
			}
		}
	}
	if !sawLocal || !sawNB {
		t.Fatalf("forest = %+v", roots)
	}
}

func TestIpamForestTree(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "core"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/16", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var roots []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(roots) != 1 || roots[0].Type != "namespace" || roots[0].Title != "core" {
		t.Fatalf("roots = %+v", roots)
	}
	if !roots[0].Expanded || len(roots[0].Children) == 0 {
		t.Fatalf("namespace should come with first-level children, got %+v", roots[0])
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree?parent="+roots[0].Key, nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var kids []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &kids); err != nil {
		t.Fatal(err)
	}
	var sawPfx bool
	for _, k := range kids {
		if k.Type == "allocated" && k.Title == "10.0.0.0/16" {
			sawPfx = true
		}
		if k.Type == "vrf" && k.Title == "default" {
			t.Fatal("default VRF must not appear as a tree node")
		}
		if k.Type == "pool" {
			t.Fatal("allowed-prefix nodes must not appear")
		}
	}
	if !sawPfx {
		t.Fatalf("ns children = %+v", kids)
	}
	if len(roots[0].Children) != len(kids) {
		t.Fatalf("embedded children = %d, lazy children = %d", len(roots[0].Children), len(kids))
	}
}

func TestIpamEmptyNamespaceRoot(t *testing.T) {
	ctrl := setupIPAM(t)

	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/16",
	}, []string{"id"}, []string{"0"})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create root prefix status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pfx models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &pfx); err != nil {
		t.Fatal(err)
	}
	if pfx.NamespaceID == 0 || pfx.VRFID == 0 {
		t.Fatalf("root prefix should land in the empty namespace: %+v", pfx)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "cust-a"}, []string{"id"}, []string{"0"})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create root vrf status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var roots []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	var sawPfx, sawVrf, sawNS bool
	for _, n := range roots {
		if n.Type == "namespace" {
			sawNS = true
		}
		if n.Type == "allocated" && n.Title == "10.0.0.0/16" {
			sawPfx = true
			if n.Data.NamespaceID != pfx.NamespaceID {
				t.Fatalf("prefix namespace_id = %d, want %d", n.Data.NamespaceID, pfx.NamespaceID)
			}
		}
		if n.Type == "vrf" && n.Title == "cust-a" {
			sawVrf = true
		}
		if n.Type == "vrf" && n.Title == "default" {
			t.Fatal("default VRF must not appear as a tree node")
		}
	}
	if !sawPfx || !sawVrf {
		t.Fatalf("empty-ns root children = %+v", roots)
	}
	if sawNS {
		t.Fatalf("empty namespace must not appear as a tree node: %+v", roots)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "core"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	sawPfx, sawVrf, sawNS = false, false, false
	for _, n := range roots {
		if n.Type == "allocated" && n.Title == "10.0.0.0/16" {
			sawPfx = true
		}
		if n.Type == "vrf" && n.Title == "cust-a" {
			sawVrf = true
		}
		if n.Type == "namespace" && n.Title == "core" {
			sawNS = true
		}
		if n.Type == "namespace" && n.Title == "" {
			t.Fatal("empty namespace must not appear as a tree node")
		}
	}
	if !sawPfx || !sawVrf || !sawNS {
		t.Fatalf("mixed forest = %+v", roots)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.1.0.0/16",
	}, []string{"id"}, []string{"0"})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	var pfx2 models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &pfx2); err != nil {
		t.Fatal(err)
	}
	if pfx2.NamespaceID != pfx.NamespaceID {
		t.Fatalf("second root prefix namespace = %d, want %d", pfx2.NamespaceID, pfx.NamespaceID)
	}
}

func TestIpamNestedRootPrefix(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "ns"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/8", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/16", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree", nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var roots []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || !roots[0].Expanded {
		t.Fatalf("roots = %+v", roots)
	}
	var top *ipam.TreeNode
	for i := range roots[0].Children {
		ch := &roots[0].Children[i]
		if ch.Type == "allocated" && ch.Title == "10.0.0.0/8" {
			top = ch
		}
		if ch.Type == "allocated" && ch.Title == "10.0.0.0/16" {
			t.Fatal("nested 10.0.0.0/16 must not appear as a sibling of 10.0.0.0/8")
		}
	}
	if top == nil {
		t.Fatalf("missing 10.0.0.0/8 in %+v", roots[0].Children)
	}
	if !top.Lazy {
		t.Fatal("10.0.0.0/8 should be expandable")
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/tree?parent="+top.Key, nil, nil, nil)
	if err := ctrl.ApiIpamForest(c); err != nil {
		t.Fatal(err)
	}
	var kids []ipam.TreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &kids); err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].Title != "10.0.0.0/16" || kids[0].Lazy {
		t.Fatalf("prefix children = %+v", kids)
	}
}

func TestIpamDisableDoesNotDeleteData(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "keep"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	off := false
	s, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		t.Fatal(err)
	}
	s.IpamEnabled = &off
	if err := ctrl.DB.Save(s).Error; err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/namespaces", nil, nil, nil)
	if err := ctrl.RequireIpamEnabled(ctrl.ApiIpamNamespaceList)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled list status = %d, want 404", rec.Code)
	}

	var n int64
	if err := ctrl.DB.Model(&models.IpamNamespace{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("namespaces after disable = %d, want 1", n)
	}
}

func TestIpamPrefixDHCP(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "lab"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "192.0.2.0/24", VRFID: ns.VRFs[0].ID, Description: "lab",
		DhcpEnabled: true, DhcpRangeStart: "192.0.2.100", DhcpRangeEnd: "192.0.2.200",
		DhcpGateway: "192.0.2.1", DhcpDnsServers: "192.0.2.53",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pfx models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &pfx); err != nil {
		t.Fatal(err)
	}
	if !pfx.DhcpEnabled || pfx.DhcpRangeStart != "192.0.2.100" || pfx.DhcpGateway != "192.0.2.1" {
		t.Fatalf("created: %+v", pfx)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "198.51.100.0/24", VRFID: ns.VRFs[0].ID,
		DhcpEnabled: true, DhcpRangeStart: "10.0.0.1", DhcpRangeEnd: "10.0.0.10",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("outside range status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/ipam/namespaces/x/prefixes/x", ipamPrefixBody{
		Description: "lab-dhcp", DhcpEnabled: true, DhcpRangeStart: "192.0.2.50", DhcpRangeEnd: "192.0.2.60",
	}, []string{"id", "prefixId"}, []string{id, strconv.FormatUint(uint64(pfx.ID), 10)})
	if err := ctrl.ApiIpamPrefixUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pfx); err != nil {
		t.Fatal(err)
	}
	if pfx.DhcpRangeStart != "192.0.2.50" || pfx.Description != "lab-dhcp" {
		t.Fatalf("updated: %+v", pfx)
	}
}

func TestIpamVRFListAllAndPrefixHosts(t *testing.T) {
	ctrl := setupIPAM(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/ipam/namespaces", ipamNamespaceBody{Name: "core"}, nil, nil)
	if err := ctrl.ApiIpamNamespaceCreate(c); err != nil {
		t.Fatal(err)
	}
	var ns ipam.NamespaceView
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(ns.ID), 10)
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/vrfs", ipamVRFBody{Name: "cust-a"}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamVRFCreate(c); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/vrfs", nil, nil, nil)
	if err := ctrl.ApiIpamVRFListAll(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list vrfs status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var vrfs []ipam.VRFListItem
	if err := json.Unmarshal(rec.Body.Bytes(), &vrfs); err != nil {
		t.Fatal(err)
	}
	if len(vrfs) != 2 {
		t.Fatalf("vrfs = %+v, want 2", vrfs)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.0.0/16", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	var p16 models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &p16); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPost, "/api/ipam/namespaces/x/prefixes", ipamPrefixBody{
		Prefix: "10.0.1.0/24", VRFID: ns.VRFs[0].ID,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiIpamPrefixCreate(c); err != nil {
		t.Fatal(err)
	}
	var p24 models.IpamPrefix
	if err := json.Unmarshal(rec.Body.Bytes(), &p24); err != nil {
		t.Fatal(err)
	}

	dev := models.Device{Name: "sw1"}
	if err := ctrl.DB.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "eth0"}
	if err := ctrl.DB.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	pfxID := p24.ID
	addr := models.Address{InterfaceID: iface.ID, Address: "10.0.1.10/24", PrefixID: &pfxID}
	if err := ctrl.DB.Create(&addr).Error; err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/prefixes/x/hosts?page=1", nil,
		[]string{"prefixId"}, []string{strconv.FormatUint(uint64(p16.ID), 10)})
	if err := ctrl.ApiIpamPrefixHosts(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("hosts status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page ipam.HostPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.PageCount != 256 || len(page.Hosts) != 256 {
		t.Fatalf("page = %+v", page)
	}
	if page.PagePrefix != "10.0.1.0/24" {
		t.Fatalf("page prefix = %s", page.PagePrefix)
	}
	var taken, free int
	var saw10 bool
	for _, h := range page.Hosts {
		if h.Allocated {
			taken++
		} else {
			free++
		}
		if h.Address == "10.0.1.10" {
			saw10 = true
			if !h.Allocated {
				t.Fatal("10.0.1.10 should be allocated")
			}
		}
	}
	if !saw10 {
		t.Fatal("missing 10.0.1.10")
	}
	if taken != 256 {
		t.Fatalf("child /24 should occupy the whole page, taken=%d free=%d", taken, free)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/ipam/prefixes/x/hosts", nil,
		[]string{"prefixId"}, []string{strconv.FormatUint(uint64(p24.ID), 10)})
	if err := ctrl.ApiIpamPrefixHosts(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.PageCount != 1 || len(page.Hosts) != 256 {
		t.Fatalf("/24 page = page_count=%d hosts=%d", page.PageCount, len(page.Hosts))
	}
	taken = 0
	for _, h := range page.Hosts {
		if h.Allocated {
			taken++
		}
	}
	if taken != 1 {
		t.Fatalf("/24 taken = %d, want 1", taken)
	}
}
