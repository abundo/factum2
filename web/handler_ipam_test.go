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
