package cfgmgmt

import (
	"fmt"
	"testing"

	"github.com/abundo/factum2/models"
	"gorm.io/gorm"
)

func mustCreateResource(t *testing.T, db *gorm.DB, parentID uint, name string, cidrs ...string) *models.ConfigScope {
	t.Helper()
	node, err := CreateScope(db, &models.ConfigScope{
		ParentID: &parentID, Name: name, Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: cidrs},
	})
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestResourceParentKinds(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, ifaceScope, _ := seedTree(t, db)
	svcRow := models.Service{ServiceID: "CN00901", ServiceType: "ELINE"}
	mustCreate(t, db, &svcRow)
	sid := svcRow.ID
	svcScope, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: svcRow.ServiceID, Kind: models.ConfigScopeKindService, ServiceID: &sid,
	})
	if err != nil {
		t.Fatal(err)
	}
	param, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	site, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "site-a", Kind: models.ConfigScopeKindSite,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []models.ConfigScope{folder, deviceScope, ifaceScope, *site} {
		if _, err := CreateScope(db, &models.ConfigScope{
			ParentID: &parent.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
			Payload: models.ConfigScopePayload{CIDRs: []string{"10.0.0.0/31"}},
		}); err != nil {
			t.Fatalf("resource under %s: %v", parent.Kind, err)
		}
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &svcScope.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
	})
	wantStatus(t, err, 400)
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &param.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
	})
	wantStatus(t, err, 400)
}

func TestResourceUniqueSiblingName(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "other", Kind: models.ConfigScopeKindFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: []string{"10.0.0.0/31"}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
	})
	wantStatus(t, err, 409)
	dup, err := CreateScope(db, &models.ConfigScope{
		ParentID: &other.ID, Name: "peering-v4", Kind: models.ConfigScopeKindResource,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = MoveScope(db, dup.ID, root.ID, nil)
	wantStatus(t, err, 409)
	name := "peering-v4"
	_, err = UpdateScope(db, dup.ID, &models.ConfigScopeDTO{ParentID: &root.ID, Name: &name})
	wantStatus(t, err, 409)
	clash, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "other-pool", Kind: models.ConfigScopeKindResource,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpdateScope(db, clash.ID, &models.ConfigScopeDTO{Name: &name})
	wantStatus(t, err, 409)
}

func TestResourceCanonicalCIDRs(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	node, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "pool", Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: []string{"10.1.2.3/24", "2001:db8:1::1/64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Payload.CIDRs) != 2 || node.Payload.CIDRs[0] != "10.1.2.0/24" || node.Payload.CIDRs[1] != "2001:db8:1::/64" {
		t.Fatalf("cidrs = %#v", node.Payload.CIDRs)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "dup-mask", Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: []string{"10.0.0.1/8", "10.9.0.0/8"}},
	})
	wantStatus(t, err, 400)
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "empty", Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: []string{"10.0.0.0/31", ""}},
	})
	wantStatus(t, err, 400)
	tooMany := make([]string, maxResourceCIDRs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("198.51.100.%d/32", i%256)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "cap", Kind: models.ConfigScopeKindResource,
		Payload: models.ConfigScopePayload{CIDRs: tooMany},
	})
	wantStatus(t, err, 400)

	en := false
	disabled, err := UpdateScope(db, node.ID, &models.ConfigScopeDTO{Enabled: &en})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestAllocateWalkClosestAncestor(t *testing.T) {
	db := newTestDB(t)
	global, folder, deviceScope, ifaceScope, iface := seedTree(t, db)
	mustCreateResource(t, db, global.ID, "peering-v4", "10.0.0.0/31", "10.0.0.2/31")
	mustCreateResource(t, db, folder.ID, "peering-v4", "10.1.0.0/31")
	devicePool := mustCreateResource(t, db, deviceScope.ID, "peering-v4", "10.2.0.0/31")
	ifcPool := mustCreateResource(t, db, ifaceScope.ID, "peering-v4", "10.3.0.0/31")

	got, err := AllocateResource(db, iface.ID, 0, "peering-v4", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeID != ifcPool.ID {
		t.Fatalf("winner = %d, want interface pool %d", got.ScopeID, ifcPool.ID)
	}
	if len(got.CIDRs) != 1 || got.CIDRs[0].Prefix != "10.3.0.0/31" || !got.CIDRs[0].Free {
		t.Fatalf("cidrs = %#v", got.CIDRs)
	}

	en := false
	if _, err := UpdateScope(db, ifcPool.ID, &models.ConfigScopeDTO{Enabled: &en}); err != nil {
		t.Fatal(err)
	}
	got, err = AllocateResource(db, iface.ID, 0, "peering-v4", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeID != devicePool.ID {
		t.Fatalf("after disable, winner = %d, want device pool %d", got.ScopeID, devicePool.ID)
	}

	_, err = AllocateResource(db, iface.ID, 0, "missing", 0)
	wantStatus(t, err, 400)
}

func TestAllocateDeviceNotInTreeUsesGlobal(t *testing.T) {
	db := newTestDB(t)
	global, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	pool := mustCreateResource(t, db, global.ID, "peering-v4", "10.9.0.0/31", "2001:db8::/64")
	dev := models.Device{Name: "orphan-pe", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)

	got, err := AllocateResource(db, ifc.ID, 0, "peering-v4", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeID != pool.ID {
		t.Fatalf("winner = %d, want global %d", got.ScopeID, pool.ID)
	}
	if len(got.CIDRs) != 1 || got.CIDRs[0].Prefix != "10.9.0.0/31" {
		t.Fatalf("family filter cidrs = %#v", got.CIDRs)
	}

	got, err = AllocateResource(db, 0, dev.ID, "peering-v4", 6)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeID != pool.ID || len(got.CIDRs) != 1 || got.CIDRs[0].Prefix != "2001:db8::/64" {
		t.Fatalf("device_id fallback = %#v", got)
	}
}

func TestAllocateOccupancyIncludesListItems(t *testing.T) {
	db := newTestDB(t)
	global, _, _, _, iface := seedTree(t, db)
	pool := mustCreateResource(t, db, global.ID, "peering-v4", "10.0.0.0/31", "10.0.0.2/31", "10.0.0.4/31")
	st := models.ServiceType{
		Name: "POLARIX",
		Schema: []models.FieldSchema{
			{Name: "prefixes", Type: models.FieldTypeList, Items: &models.FieldSchema{
				Type: models.FieldTypeIPv4Prefix, Resource: "peering-v4",
			}},
			{Name: "other", Type: models.FieldTypeIPv4Prefix, Resource: "other-pool"},
		},
		Interfaces: models.ServiceInterfacesSpec{
			Min: 0, Max: 0,
			Fields: []models.FieldSchema{
				{Name: "peer", Type: models.FieldTypeIPv4Prefix, Resource: "peering-v4"},
			},
		},
	}
	mustCreate(t, db, &st)
	svc := models.Service{
		ServiceID:   "CN00910",
		ServiceType: st.Name,
		Fields:      jsonRaw(t, map[string]any{"prefixes": []any{"10.0.0.0/31"}, "other": "10.0.0.4/31"}),
	}
	mustCreate(t, db, &svc)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: iface.DeviceID, InterfaceID: iface.ID,
		Fields: jsonRaw(t, map[string]any{"peer": "10.0.0.2/31"}),
	})

	got, err := AllocateResource(db, iface.ID, 0, "peering-v4", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeID != pool.ID {
		t.Fatalf("scope = %d", got.ScopeID)
	}
	free := map[string]bool{}
	for _, c := range got.CIDRs {
		free[c.Prefix] = c.Free
	}
	if free["10.0.0.0/31"] {
		t.Fatal("list item prefix should be occupied")
	}
	if free["10.0.0.2/31"] {
		t.Fatal("endpoint prefix should be occupied")
	}
	if !free["10.0.0.4/31"] {
		t.Fatal("prefix on a different resource name must not occupy")
	}
}

func TestDeleteResourceAndDetachDevice(t *testing.T) {
	db := newTestDB(t)
	_, _, deviceScope, ifaceScope, _ := seedTree(t, db)
	res := mustCreateResource(t, db, ifaceScope.ID, "peering-v4", "10.0.0.0/31")
	if err := DeleteScope(db, res.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetScope(db, res.ID); err == nil {
		t.Fatal("resource still present")
	}
	res2 := mustCreateResource(t, db, deviceScope.ID, "peering-v4")
	if err := DetachDevice(db, deviceScope.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetScope(db, res2.ID); err == nil {
		t.Fatal("resource survived device detach")
	}
}
