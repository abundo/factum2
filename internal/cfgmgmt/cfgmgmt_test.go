package cfgmgmt

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/drivers/templates"
	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(
		&models.Device{},
		&models.Interface{},
		&models.Address{},
		&models.Customer{},
		&models.Service{},
		&models.ConfigScope{},
		&models.ConfigCLIFeature{},
		&models.ConfigVariableDef{},
		&models.ConfigAssignment{},
		&models.ServiceType{},
		&models.ConfigMacro{},
		&models.ServiceEndpoint{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Seed(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func jsonRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func mustCreate(t *testing.T, db *gorm.DB, v any) {
	t.Helper()
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
}

func seedTree(t *testing.T, db *gorm.DB) (global, folder, deviceScope, ifaceScope models.ConfigScope, iface models.Interface) {
	t.Helper()
	root, err := RootScope(db)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	global = *root

	dev := models.Device{Name: "pe1", Platform: "eos", Site: "lab"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	iface = ifc

	folder = models.ConfigScope{ParentID: &global.ID, Name: "lab", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &folder)

	did := dev.ID
	deviceScope = models.ConfigScope{ParentID: &folder.ID, Name: dev.Name, Kind: models.ConfigScopeKindDevice, DeviceID: &did}
	mustCreate(t, db, &deviceScope)

	iid := ifc.ID
	ifaceScope = models.ConfigScope{ParentID: &deviceScope.ID, Name: ifc.Name, Kind: models.ConfigScopeKindInterface, DeviceID: &did, InterfaceID: &iid}
	mustCreate(t, db, &ifaceScope)
	return
}

func TestInventoryMapsAndRolesForCount(t *testing.T) {
	types := []models.ServiceType{
		{Name: "ELINE", SyncSource: models.SyncSourceELINE, NetboxType: models.NetboxTypeEVPL, EndpointRoles: []models.EndpointRole{
			{Name: "a", Min: 1, Max: 1}, {Name: "b", Min: 1, Max: 1},
		}},
		{Name: "ELAN", SyncSource: models.SyncSourceELAN, NetboxType: models.NetboxTypeVPLS, EndpointRoles: []models.EndpointRole{
			{Name: "endpoint", Min: 1, Max: 0},
		}},
		{Name: "POLARIX", EndpointRoles: []models.EndpointRole{{Name: "endpoint", Min: 1, Max: 0}}},
	}
	got := InventoryMaps(types)
	if got[models.SyncSourceELINE] != models.NetboxTypeEVPL || got[models.SyncSourceELAN] != models.NetboxTypeVPLS {
		t.Errorf("InventoryMaps = %#v", got)
	}
	if _, ok := got[models.SyncSourceL3VPN]; ok {
		t.Errorf("unexpected l3vpn mapping: %#v", got)
	}
	eline := TypeForNetboxKind(types, models.NetboxTypeEVPL)
	if eline == nil || eline.Name != "ELINE" {
		t.Fatalf("TypeForNetboxKind evpl = %#v", eline)
	}
	roles := EndpointRolesForCount(&types[0], 2)
	if len(roles) != 2 || roles[0] != "a" || roles[1] != "b" {
		t.Errorf("ELINE roles = %v, want [a b]", roles)
	}
	elanRoles := EndpointRolesForCount(&types[1], 3)
	if len(elanRoles) != 3 || elanRoles[0] != "endpoint" || elanRoles[2] != "endpoint" {
		t.Errorf("ELAN roles = %v, want 3x endpoint", elanRoles)
	}
}

func scopeChild(t *testing.T, db *gorm.DB, parentID uint, name string) models.ConfigScope {
	t.Helper()
	var s models.ConfigScope
	if err := db.Where("parent_id = ? AND name = ?", parentID, name).First(&s).Error; err != nil {
		t.Fatalf("scope %s under %d: %v", name, parentID, err)
	}
	return s
}

func TestSeedReservedFolders(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := scopeChild(t, db, root.ID, models.ConfigCatalogName)
	if catalog.Kind != models.ConfigScopeKindFolder {
		t.Errorf("catalog kind = %s, want folder", catalog.Kind)
	}
	cli := scopeChild(t, db, catalog.ID, models.ConfigCatalogCLIName)
	if cli.Kind != models.ConfigScopeKindFolder {
		t.Errorf("cli kind = %s, want folder", cli.Kind)
	}
	services := scopeChild(t, db, root.ID, models.ConfigServicesFolderName)
	if services.Kind != models.ConfigScopeKindFolder {
		t.Errorf("services kind = %s, want folder", services.Kind)
	}
	catalogID, cliID, servicesID := catalog.ID, cli.ID, services.ID
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	catalog2 := scopeChild(t, db, root.ID, models.ConfigCatalogName)
	cli2 := scopeChild(t, db, catalog2.ID, models.ConfigCatalogCLIName)
	services2 := scopeChild(t, db, root.ID, models.ConfigServicesFolderName)
	if catalog2.ID != catalogID || cli2.ID != cliID || services2.ID != servicesID {
		t.Fatal("second Seed recreated reserved folders")
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("parent_id = ? AND name = ?", root.ID, models.ConfigCatalogName).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("catalog count = %d, want 1", n)
	}
}

func TestValidScopeKind(t *testing.T) {
	for _, k := range []string{
		models.ConfigScopeKindFolder,
		models.ConfigScopeKindSite,
		models.ConfigScopeKindLocation,
		models.ConfigScopeKindDevice,
		models.ConfigScopeKindInterface,
		models.ConfigScopeKindParameter,
		models.ConfigScopeKindCLI,
		models.ConfigScopeKindService,
		models.ConfigScopeKindServiceEndpoint,
	} {
		if !ValidScopeKind(k) {
			t.Errorf("ValidScopeKind(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"service_ref", "garbage", ""} {
		if ValidScopeKind(k) {
			t.Errorf("ValidScopeKind(%q) = true, want false", k)
		}
	}
}

func TestConfigCLIFeatureInsert(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	cli := models.ConfigScope{
		ParentID:    &root.ID,
		Name:        "eos",
		Kind:        models.ConfigScopeKindCLI,
		Platform:    "eos",
		PayloadKind: models.PayloadKindCLI,
		Enabled:     true,
	}
	mustCreate(t, db, &cli)
	feat := models.ConfigCLIFeature{
		ScopeID:     cli.ID,
		Name:        "apply",
		AddCommands: "interface Ethernet1",
	}
	mustCreate(t, db, &feat)
	var got models.ConfigCLIFeature
	if err := db.First(&got, feat.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.ScopeID != cli.ID || got.Name != "apply" || got.AddCommands != "interface Ethernet1" {
		t.Errorf("got %+v", got)
	}
}

func TestSeedCreatesRootAndELINECLI(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	if root.Name != models.ConfigRootName || root.Kind != models.ConfigScopeKindFolder {
		t.Errorf("root = %+v", root)
	}
	if ok, err := ServiceTypeExists(db, "ELINE"); err != nil || !ok {
		t.Fatalf("ELINE type missing: %v", err)
	}
	var eline, elan, l3 models.ServiceType
	if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
		t.Fatal(err)
	}
	if eline.SyncSource != models.SyncSourceELINE || eline.NetboxType != models.NetboxTypeEVPL {
		t.Errorf("ELINE mapping = %s/%s, want eline/evpl", eline.SyncSource, eline.NetboxType)
	}
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	if elan.SyncSource != models.SyncSourceELAN || elan.NetboxType != models.NetboxTypeVPLS {
		t.Errorf("ELAN mapping = %s/%s, want elan/vpls", elan.SyncSource, elan.NetboxType)
	}
	if !schemaHas(elan.Schema, "max_mac_addresses") {
		t.Errorf("ELAN schema missing max_mac_addresses: %+v", elan.Schema)
	}
	if !schemaHas(eline.Schema, models.SchemaFieldBandwidthMbps) {
		t.Errorf("ELINE schema missing bandwidth_mbps: %+v", eline.Schema)
	}
	if !schemaHas(elan.Schema, models.SchemaFieldBandwidthMbps) {
		t.Errorf("ELAN schema missing bandwidth_mbps: %+v", elan.Schema)
	}
	if err := db.Where("name = ?", "L3VPN").First(&l3).Error; err != nil {
		t.Fatal(err)
	}
	if !schemaHas(l3.Schema, models.SchemaFieldBandwidthMbps) {
		t.Errorf("L3VPN schema missing bandwidth_mbps: %+v", l3.Schema)
	}
	var polarix models.ServiceType
	if err := db.Where("name = ?", "POLARIX").First(&polarix).Error; err != nil {
		t.Fatal(err)
	}
	if !schemaHas(polarix.Schema, models.SchemaFieldBandwidthMbps) {
		t.Errorf("POLARIX schema missing bandwidth_mbps: %+v", polarix.Schema)
	}
	if l3.SyncSource != models.SyncSourceL3VPN || l3.NetboxType != models.NetboxTypeVRF {
		t.Errorf("L3VPN mapping = %s/%s, want l3vpn/vrf", l3.SyncSource, l3.NetboxType)
	}
	for _, plat := range []string{"eos", "ios-xr", "sros", "sros-md"} {
		obj, err := LookupCLIObject(db, "ELINE", plat)
		if err != nil || obj == nil {
			t.Fatalf("CLI %s: %v %#v", plat, err, obj)
		}
		if obj.Name != plat || obj.Platform != plat || obj.PayloadKind != models.PayloadKindCLI {
			t.Errorf("CLI %s = %+v", plat, obj)
		}
		if obj.Payload.Context != nil {
			t.Errorf("CLI %s context = %+v, want empty", plat, obj.Payload.Context)
		}
		if obj.SeedChecksum == "" {
			t.Errorf("CLI %s missing seed checksum", plat)
		}
		feats, err := ListCLIFeatures(db, obj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(feats) != 1 || feats[0].Name != "apply" {
			t.Fatalf("CLI %s features = %+v", plat, feats)
		}
		if strings.TrimSpace(feats[0].AddCommands) == "" || strings.TrimSpace(feats[0].RemoveCommands) == "" {
			t.Errorf("CLI %s apply feature missing add/remove", plat)
		}
		if strings.Contains(feats[0].AddCommands, `template "cleanup"`) {
			t.Errorf("CLI %s add still invokes cleanup", plat)
		}
	}
	root, err = RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := scopeChild(t, db, root.ID, models.ConfigCatalogName)
	cliFolder := scopeChild(t, db, catalog.ID, models.ConfigCatalogCLIName)
	elineFolder := scopeChild(t, db, cliFolder.ID, "ELINE")
	_ = cliChild(t, db, elineFolder.ID, "eos")
}

func TestSeedAddsMissingSchemaFields(t *testing.T) {
	db := newTestDB(t)
	var eline, elan models.ServiceType
	if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
		t.Fatal(err)
	}
	eline.Schema = nil
	if err := db.Save(&eline).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	elan.Schema = []models.FieldSchema{{Name: "custom", Type: models.VarTypeString}}
	if err := db.Save(&elan).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
		t.Fatal(err)
	}
	if !schemaHas(eline.Schema, models.SchemaFieldBandwidthMbps) {
		t.Errorf("ELINE schema missing bandwidth_mbps after reseed: %+v", eline.Schema)
	}
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	if !schemaHas(elan.Schema, models.SchemaFieldBandwidthMbps) || !schemaHas(elan.Schema, models.SchemaFieldMaxMacAddresses) {
		t.Errorf("ELAN schema missing seeded fields after reseed: %+v", elan.Schema)
	}
	if !schemaHas(elan.Schema, "custom") {
		t.Errorf("ELAN operator schema field was dropped: %+v", elan.Schema)
	}
}

func TestResolveWalkAndFirstMatch(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, ifaceScope, iface := seedTree(t, db)

	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)

	if _, err := UpsertAssignment(db, def.ID, folder.ID, jsonRaw(t, 1500)); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, def.ID, deviceScope.ID, jsonRaw(t, 9000)); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, def.ID, ifaceScope.ID, jsonRaw(t, 9100)); err != nil {
		t.Fatal(err)
	}
	ifaceChild := mustParametersChild(t, db, ifaceScope.ID)
	deviceChild := mustParametersChild(t, db, deviceScope.ID)

	v, src, err := Resolve(db, iface.ID, "mtu")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src == nil || src.ID != ifaceChild.ID {
		t.Fatalf("source = %+v, want interface parameters child", src)
	}
	if n, _ := asInt(v); n != 9100 {
		t.Errorf("value = %v, want 9100", v)
	}

	if err := db.Delete(&models.ConfigAssignment{}, "scope_id = ?", ifaceChild.ID).Error; err != nil {
		t.Fatal(err)
	}
	v, src, err = Resolve(db, iface.ID, "mtu")
	if err != nil {
		t.Fatalf("resolve device: %v", err)
	}
	if src == nil || src.ID != deviceChild.ID {
		t.Fatalf("source = %+v, want device parameters child", src)
	}
	if n, _ := asInt(v); n != 9000 {
		t.Errorf("value = %v, want 9000 (interface assignment hidden)", v)
	}
}

func TestResolveDeviceOnlyWhenNoInterfaceNode(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe2", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet2", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	did := dev.ID
	deviceScope := models.ConfigScope{ParentID: &root.ID, Name: dev.Name, Kind: models.ConfigScopeKindDevice, DeviceID: &did}
	mustCreate(t, db, &deviceScope)

	def := models.ConfigVariableDef{Name: "asn", Type: models.VarTypeInt, DefaultValue: jsonRaw(t, 1)}
	mustCreate(t, db, &def)
	if _, err := UpsertAssignment(db, def.ID, deviceScope.ID, jsonRaw(t, 65000)); err != nil {
		t.Fatal(err)
	}
	deviceChild := mustParametersChild(t, db, deviceScope.ID)

	v, src, err := Resolve(db, ifc.ID, "asn")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src == nil || src.ID != deviceChild.ID {
		t.Fatalf("source = %+v, want device parameters child", src)
	}
	if n, _ := asInt(v); n != 65000 {
		t.Errorf("value = %v", v)
	}
}

func TestResolveDefaultAndRequired(t *testing.T) {
	db := newTestDB(t)
	_, _, _, _, iface := seedTree(t, db)

	def := models.ConfigVariableDef{Name: "color", Type: models.VarTypeString, DefaultValue: jsonRaw(t, "blue")}
	mustCreate(t, db, &def)
	v, src, err := Resolve(db, iface.ID, "color")
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if src != nil {
		t.Errorf("source should be nil for default, got %+v", src)
	}
	if v != "blue" {
		t.Errorf("value = %v, want blue", v)
	}

	req := models.ConfigVariableDef{Name: "must", Type: models.VarTypeString, Required: true}
	mustCreate(t, db, &req)
	_, _, err = Resolve(db, iface.ID, "must")
	if err == nil {
		t.Fatal("expected required error")
	}

	_, _, err = Resolve(db, iface.ID, "no-such-var")
	if err == nil {
		t.Fatal("expected unknown variable error")
	}
}

func TestCycleRejected(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	a := models.ConfigScope{ParentID: &root.ID, Name: "a", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &a)
	b := models.ConfigScope{ParentID: &a.ID, Name: "b", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &b)
	cycle, err := WouldCycle(db, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !cycle {
		t.Fatal("expected cycle")
	}
	_, err = UpdateScope(db, a.ID, &models.ConfigScopeDTO{ParentID: &b.ID})
	if err == nil {
		t.Fatal("update should reject cycle")
	}
}

func TestTypeValidation(t *testing.T) {
	db := newTestDB(t)
	_, _, _, ifaceScope, iface := seedTree(t, db)

	vlan := models.ConfigVariableDef{Name: "svi", Type: models.VarTypeVLAN}
	mustCreate(t, db, &vlan)
	_, err := UpsertAssignment(db, vlan.ID, ifaceScope.ID, jsonRaw(t, 5000))
	if err == nil {
		t.Fatal("expected vlan range error")
	}
	if _, err := UpsertAssignment(db, vlan.ID, ifaceScope.ID, jsonRaw(t, 100)); err != nil {
		t.Fatalf("valid vlan: %v", err)
	}

	ip := models.ConfigVariableDef{Name: "peer", Type: models.VarTypeIP}
	mustCreate(t, db, &ip)
	if _, err := UpsertAssignment(db, ip.ID, ifaceScope.ID, jsonRaw(t, "not-an-ip")); err == nil {
		t.Fatal("expected bad ip error")
	}
	if _, err := UpsertAssignment(db, ip.ID, ifaceScope.ID, jsonRaw(t, "10.0.0.1")); err != nil {
		t.Fatalf("valid ip: %v", err)
	}

	en := models.ConfigVariableDef{
		Name:        "mode",
		Type:        models.VarTypeEnum,
		Constraints: jsonRaw(t, map[string]any{"enum": []string{"a", "b"}}),
	}
	mustCreate(t, db, &en)
	if _, err := UpsertAssignment(db, en.ID, ifaceScope.ID, jsonRaw(t, "z")); err == nil {
		t.Fatal("expected enum error")
	}
	if _, err := UpsertAssignment(db, en.ID, ifaceScope.ID, jsonRaw(t, "a")); err != nil {
		t.Fatalf("valid enum: %v", err)
	}

	v, _, err := Resolve(db, iface.ID, "svi")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := asInt(v); n != 100 {
		t.Errorf("vlan value = %v", v)
	}
}

func TestTypeCheckListItems(t *testing.T) {
	shorthand := &models.ConfigVariableDef{
		Name:        "peers",
		Type:        models.VarTypeList,
		Constraints: jsonRaw(t, map[string]any{"items": "ip"}),
	}
	got, err := TypeCheck(shorthand, []any{"10.0.0.1", "10.0.0.2"})
	if err != nil {
		t.Fatalf("valid ips: %v", err)
	}
	list, ok := got.([]any)
	if !ok || len(list) != 2 || list[0] != "10.0.0.1" {
		t.Fatalf("got %#v", got)
	}
	if _, err := TypeCheck(shorthand, []any{"10.0.0.1", "nope"}); err == nil {
		t.Fatal("expected invalid ip in list")
	}
	if _, err := TypeCheck(shorthand, "10.0.0.1"); err == nil {
		t.Fatal("expected reject scalar")
	}

	typed := &models.ConfigVariableDef{
		Name: "vlans",
		Type: models.VarTypeList,
		Constraints: jsonRaw(t, map[string]any{
			"items": map[string]any{"type": "int", "min": 1, "max": 10},
			"min":   1,
			"max":   3,
		}),
	}
	got, err = TypeCheck(typed, []any{1, 2.0, "3"})
	if err != nil {
		t.Fatalf("coerced ints: %v", err)
	}
	list, _ = got.([]any)
	if len(list) != 3 {
		t.Fatalf("len = %d", len(list))
	}
	for i, want := range []int64{1, 2, 3} {
		n, ok := asInt(list[i])
		if !ok || n != want {
			t.Errorf("[%d] = %#v, want %d", i, list[i], want)
		}
	}
	if _, err := TypeCheck(typed, []any{99}); err == nil {
		t.Fatal("expected item max error")
	}
	if _, err := TypeCheck(typed, []any{}); err == nil {
		t.Fatal("expected min length error")
	}
	if _, err := TypeCheck(typed, []any{1, 2, 3, 4}); err == nil {
		t.Fatal("expected max length error")
	}

	untyped := &models.ConfigVariableDef{Name: "any", Type: models.VarTypeList}
	if _, err := TypeCheck(untyped, []string{"a", "b"}); err != nil {
		t.Fatalf("untyped list: %v", err)
	}
}

func TestTypeCheckMapKeysValues(t *testing.T) {
	def := &models.ConfigVariableDef{
		Name: "communities",
		Type: models.VarTypeMap,
		Constraints: jsonRaw(t, map[string]any{
			"keys":   "string",
			"values": map[string]any{"type": "int", "min": 0},
		}),
	}
	got, err := TypeCheck(def, map[string]any{"public": 65000, "private": "1"})
	if err != nil {
		t.Fatalf("valid map: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %#v", got)
	}
	if n, _ := asInt(m["public"]); n != 65000 {
		t.Errorf("public = %#v", m["public"])
	}
	if n, _ := asInt(m["private"]); n != 1 {
		t.Errorf("private = %#v", m["private"])
	}
	if _, err := TypeCheck(def, map[string]any{"x": "nope"}); err == nil {
		t.Fatal("expected value type error")
	}
	if _, err := TypeCheck(def, []any{"x"}); err == nil {
		t.Fatal("expected reject list")
	}

	ipKeys := &models.ConfigVariableDef{
		Name:        "peer_as",
		Type:        models.VarTypeMap,
		Constraints: jsonRaw(t, map[string]any{"keys": "ip", "values": "int"}),
	}
	if _, err := TypeCheck(ipKeys, map[string]any{"10.0.0.1": 65000}); err != nil {
		t.Fatalf("ip keys: %v", err)
	}
	if _, err := TypeCheck(ipKeys, map[string]any{"not-an-ip": 1}); err == nil {
		t.Fatal("expected bad key")
	}

	enumKeys := &models.ConfigVariableDef{
		Name: "flags",
		Type: models.VarTypeMap,
		Constraints: jsonRaw(t, map[string]any{
			"keys":   map[string]any{"type": "enum", "enum": []string{"a", "b"}},
			"values": "bool",
		}),
	}
	if _, err := TypeCheck(enumKeys, map[string]any{"a": true}); err != nil {
		t.Fatalf("enum key: %v", err)
	}
	if _, err := TypeCheck(enumKeys, map[string]any{"z": true}); err == nil {
		t.Fatal("expected enum key error")
	}

	nested := &models.ConfigVariableDef{
		Name: "groups",
		Type: models.VarTypeMap,
		Constraints: jsonRaw(t, map[string]any{
			"keys":   "string",
			"values": map[string]any{"type": "list", "items": "ip"},
		}),
	}
	if _, err := TypeCheck(nested, map[string]any{"core": []any{"10.0.0.1"}}); err != nil {
		t.Fatalf("nested list values: %v", err)
	}
	if _, err := TypeCheck(nested, map[string]any{"core": []any{"bad"}}); err == nil {
		t.Fatal("expected nested item error")
	}
}

func TestValidateConstraintsListAndMap(t *testing.T) {
	if err := ValidateConstraints(models.VarTypeList, jsonRaw(t, map[string]any{"items": "ip"})); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := ValidateConstraints(models.VarTypeList, jsonRaw(t, map[string]any{"items": "nope"})); err == nil {
		t.Fatal("expected unknown item type")
	}
	if err := ValidateConstraints(models.VarTypeMap, jsonRaw(t, map[string]any{"keys": "list"})); err == nil {
		t.Fatal("expected reject list keys")
	}
	if err := ValidateConstraints(models.VarTypeString, jsonRaw(t, map[string]any{"items": "ip"})); err == nil {
		t.Fatal("expected items only on list")
	}
	def := &models.ConfigVariableDef{
		Name:         "peers",
		Type:         models.VarTypeList,
		Constraints:  jsonRaw(t, map[string]any{"items": "ip"}),
		DefaultValue: jsonRaw(t, []string{"not-an-ip"}),
	}
	if err := ValidateVariableDef(def); err == nil {
		t.Fatal("expected default type error")
	}
}

func TestUpsertTypedListAndMap(t *testing.T) {
	db := newTestDB(t)
	_, _, _, ifaceScope, iface := seedTree(t, db)

	peers := models.ConfigVariableDef{
		Name:        "ntp",
		Type:        models.VarTypeList,
		Constraints: jsonRaw(t, map[string]any{"items": "ip"}),
	}
	mustCreate(t, db, &peers)
	if _, err := UpsertAssignment(db, peers.ID, ifaceScope.ID, jsonRaw(t, []string{"not-an-ip"})); err == nil {
		t.Fatal("expected bad list item")
	}
	if _, err := UpsertAssignment(db, peers.ID, ifaceScope.ID, jsonRaw(t, []string{"10.0.0.1", "10.0.0.2"})); err != nil {
		t.Fatalf("valid list: %v", err)
	}
	v, _, err := Resolve(db, iface.ID, "ntp")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := v.([]any)
	if len(list) != 2 || list[0] != "10.0.0.1" {
		t.Errorf("ntp = %#v", v)
	}

	tags := models.ConfigVariableDef{
		Name:        "asn_map",
		Type:        models.VarTypeMap,
		Constraints: jsonRaw(t, map[string]any{"keys": "string", "values": "int"}),
	}
	mustCreate(t, db, &tags)
	if _, err := UpsertAssignment(db, tags.ID, ifaceScope.ID, jsonRaw(t, map[string]any{"east": "x"})); err == nil {
		t.Fatal("expected bad map value")
	}
	if _, err := UpsertAssignment(db, tags.ID, ifaceScope.ID, jsonRaw(t, map[string]any{"east": 65001})); err != nil {
		t.Fatalf("valid map: %v", err)
	}
}

func TestRenderELINECLIMatchesEmbed(t *testing.T) {
	db := newTestDB(t)
	intent := GenericRenderData{
		Name:        "CN00570",
		Description: "ID=CN00570 Acme AB",
		LocalIface:  "Ethernet1",
		LocalVLAN:   100,
		Remote: &ELINERemote{
			NeighborIP:   "172.27.250.28",
			PseudowireID: 1000570,
			MTU:          9100,
			ControlWord:  true,
		},
	}
	want, err := Render(db, templates.EOSEline, "", intent)
	if err != nil {
		t.Fatalf("embed render: %v", err)
	}
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	got, err := RenderCLIObject(db, obj, intent)
	if err != nil {
		t.Fatalf("CLI render: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CLI cmds =\n%v\nwant embed\n%v", got, want)
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(feats) != 1 {
		t.Fatalf("features: %v %+v", err, feats)
	}
	add, err := Render(db, feats[0].AddCommands, "", intent)
	if err != nil {
		t.Fatalf("add blob: %v", err)
	}
	for _, line := range add {
		if strings.Contains(line, "no pseudowire") || strings.Contains(line, "no patch") {
			t.Fatalf("add blob still includes cleanup: %v", add)
		}
	}
	foundIface := false
	for _, line := range add {
		if line == "interface Ethernet1" {
			foundIface = true
		}
	}
	if !foundIface {
		t.Fatalf("add blob missing interface config: %v", add)
	}
}

func TestRenderELINEFromServiceEndpoints(t *testing.T) {
	db := newTestDB(t)
	cust := models.Customer{Name: "Acme AB"}
	mustCreate(t, db, &cust)
	pe1 := models.Device{Name: "pe1", Platform: "eos", NetboxID: 501}
	pe2 := models.Device{Name: "pe2", Platform: "eos", NetboxID: 502}
	mustCreate(t, db, &pe1)
	mustCreate(t, db, &pe2)
	ifa := models.Interface{DeviceID: pe1.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 510}
	ifb := models.Interface{DeviceID: pe2.ID, Name: "Ethernet2", Type: "1000base-t", NetboxID: 520}
	mustCreate(t, db, &ifa)
	mustCreate(t, db, &ifb)
	lo1 := models.Interface{DeviceID: pe1.ID, Name: "Loopback0", Type: "virtual", NetboxID: 511}
	lo2 := models.Interface{DeviceID: pe2.ID, Name: "Loopback0", Type: "virtual", NetboxID: 521}
	mustCreate(t, db, &lo1)
	mustCreate(t, db, &lo2)
	mustCreate(t, db, &models.Address{InterfaceID: lo1.ID, Address: "10.0.0.1/32"})
	mustCreate(t, db, &models.Address{InterfaceID: lo2.ID, Address: "10.0.0.2/32"})

	svc := models.Service{
		CustomerID:   cust.ID,
		ServiceID:    "CN00570",
		ServiceType:  "ELINE",
		PseudowireID: 1000570,
	}
	mustCreate(t, db, &svc)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "a", DeviceID: pe1.ID, InterfaceID: ifa.ID,
		Fields: EncodeEndpointFields(100, 0, 0),
	})
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "b", DeviceID: pe2.ID, InterfaceID: ifb.ID,
		Fields: EncodeEndpointFields(200, 0, 0),
	})

	out, err := RenderService(db, svc.ID)
	if err != nil {
		t.Fatalf("RenderService: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no sources")
	}
	var joined string
	for _, src := range out {
		if src.Error != "" {
			t.Fatalf("render error: %s", src.Error)
		}
		joined += strings.Join(src.Commands, "\n") + "\n"
	}
	if !strings.Contains(joined, "neighbor 10.0.0.2") {
		t.Errorf("missing remote neighbor in %v", joined)
	}
	if !strings.Contains(joined, "interface Ethernet1.100") {
		t.Errorf("missing local subinterface in %v", joined)
	}
}

func TestRenderDeviceIncludesGlobalCLI(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe3", Platform: "eos"}
	mustCreate(t, db, &dev)
	def := models.ConfigVariableDef{Name: "color", Type: models.VarTypeString}
	mustCreate(t, db, &def)
	if _, err := UpsertAssignment(db, def.ID, root.ID, jsonRaw(t, "blue")); err != nil {
		t.Fatal(err)
	}
	cli, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigCLIFeature{
		ScopeID: cli.ID, Name: "body",
		AddCommands: "banner motd {{.Name}} {{index .Vars \"color\"}}",
	})

	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	found := false
	for _, s := range out.Sources {
		if s.Source == "cli:banner" {
			found = true
			if s.Error != "" {
				t.Fatalf("render error: %s", s.Error)
			}
			if !reflect.DeepEqual(s.Commands, []string{"banner motd pe3 blue"}) {
				t.Errorf("commands = %v", s.Commands)
			}
		}
	}
	if !found {
		t.Fatalf("baseline CLI missing from RenderDevice: %+v", out.Sources)
	}
}

func TestDeleteRootRejected(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteScope(db, root.ID); err == nil {
		t.Fatal("expected reject")
	}
	child := models.ConfigScope{ParentID: &root.ID, Name: "x", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &child)
	if err := DeleteScope(db, root.ID); err == nil {
		t.Fatal("expected reject root even with children")
	}
	if err := DeleteScope(db, child.ID); err != nil {
		t.Fatalf("delete child: %v", err)
	}
}

func TestAttachDeviceRejectsCycle(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-cycle", Platform: "eos", NetboxID: 9}
	mustCreate(t, db, &dev)
	node, err := AttachDevice(db, root.ID, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	folder := models.ConfigScope{ParentID: &node.ID, Name: "under-device", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &folder)
	if _, err := AttachDevice(db, folder.ID, dev.ID); err == nil {
		t.Fatal("expected cycle when reparenting device under its descendant")
	}
}

func TestUpdateScopeRejectsDuplicateDevice(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	d1 := models.Device{Name: "d1", Platform: "eos", NetboxID: 11}
	d2 := models.Device{Name: "d2", Platform: "eos", NetboxID: 12}
	mustCreate(t, db, &d1)
	mustCreate(t, db, &d2)
	n1, err := AttachDevice(db, root.ID, d1.ID)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := AttachDevice(db, root.ID, d2.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := d1.ID
	kind := models.ConfigScopeKindDevice
	if _, err := UpdateScope(db, n2.ID, &models.ConfigScopeDTO{DeviceID: &id, Kind: &kind}); err == nil {
		t.Fatal("expected duplicate device scope")
	}
	_ = n1
}

func TestRequiredNullIsMissing(t *testing.T) {
	db := newTestDB(t)
	_, _, _, ifaceScope, iface := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "must", Type: models.VarTypeString, Required: true}
	mustCreate(t, db, &def)
	if _, err := UpsertAssignment(db, def.ID, ifaceScope.ID, jsonRaw(t, nil)); err == nil {
		t.Fatal("expected upsert of null required var to fail")
	}
	opt := models.ConfigVariableDef{Name: "opt", Type: models.VarTypeString, Required: true}
	mustCreate(t, db, &opt)
	child, err := ensureParametersChild(db, ifaceScope.ID)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: opt.ID, ScopeID: child.ID, Value: []byte("null")})
	if _, _, err := Resolve(db, iface.ID, "opt"); err == nil {
		t.Fatal("expected required error for JSON null assignment")
	}
}

func TestValidateEndpointsInventory(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "pe-ep", Platform: "eos", NetboxID: 21}
	other := models.Device{Name: "pe-other", Platform: "eos", NetboxID: 22}
	mustCreate(t, db, &dev)
	mustCreate(t, db, &other)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 1}
	mustCreate(t, db, &ifc)
	st, err := LookupServiceType(db, "ELAN")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.EndpointRoles) == 0 {
		t.Fatal("ELAN should have seeded endpoint roles")
	}
	bad := []models.ServiceEndpoint{{Role: "endpoint", DeviceID: other.ID, InterfaceID: ifc.ID}}
	if err := ValidateEndpoints(db, st, bad); err == nil {
		t.Fatal("expected cross-device reject")
	}
	missing := []models.ServiceEndpoint{{Role: "endpoint", DeviceID: 99999, InterfaceID: ifc.ID}}
	if err := ValidateEndpoints(db, st, missing); err == nil {
		t.Fatal("expected missing device reject")
	}
	ok := []models.ServiceEndpoint{
		{Role: "endpoint", DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(100, 0, 0)},
	}
	if err := ValidateEndpoints(db, st, ok); err != nil {
		t.Fatalf("valid endpoint: %v", err)
	}
}

func TestRootCannotBeRenamed(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	name := "not-global"
	if _, err := UpdateScope(db, root.ID, &models.ConfigScopeDTO{Name: &name}); err == nil {
		t.Fatal("expected rename reject")
	}
	kind := models.ConfigScopeKindSite
	if _, err := UpdateScope(db, root.ID, &models.ConfigScopeDTO{Kind: &kind}); err == nil {
		t.Fatal("expected kind change reject")
	}
}

func TestPlatformsFilter(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "sros1", Platform: "sros", NetboxID: 31}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "1/1/1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	def := models.ConfigVariableDef{
		Name: "eos_only", Type: models.VarTypeString,
		Platforms:    jsonRaw(t, []string{"eos"}),
		DefaultValue: jsonRaw(t, "hi"),
	}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: root.ID, Value: jsonRaw(t, "hi")})
	all, err := ResolveAll(db, ifc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rv := range all {
		if rv.Name == "eos_only" {
			t.Fatal("eos-only var should be skipped on sros")
		}
	}
}

func TestSeedUpdatesUntouchedCLI(t *testing.T) {
	db := newTestDB(t)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(feats) != 1 {
		t.Fatalf("features: %v %+v", err, feats)
	}
	feats[0].AddCommands = "old-seed-body"
	feats[0].RemoveCommands = ""
	if err := db.Save(&feats[0]).Error; err != nil {
		t.Fatal(err)
	}
	obj.SeedChecksum = currentCLIChecksum(obj, []models.ConfigCLIFeature{feats[0]})
	if err := db.Save(obj).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(again) != 1 {
		t.Fatalf("after seed: %v %+v", err, again)
	}
	wantAdd, wantRemove := packToCLIBlobs(templates.EOSEline, "")
	if again[0].AddCommands != wantAdd || again[0].RemoveCommands != wantRemove {
		t.Fatal("untouched CLI was not refreshed from embed")
	}
}

func TestWalkParentsDepthError(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	parentID := root.ID
	var last models.ConfigScope
	for i := 0; i < maxScopeDepth+2; i++ {
		pid := parentID
		s := models.ConfigScope{ParentID: &pid, Name: "d", Kind: models.ConfigScopeKindFolder}
		mustCreate(t, db, &s)
		parentID = s.ID
		last = s
	}
	_, err = WalkParents(db, &last)
	if err == nil || err.Error() != "scope parent chain too deep" {
		t.Fatalf("err = %v, want depth error", err)
	}
}

func mustParametersChild(t *testing.T, db *gorm.DB, parentID uint) *models.ConfigScope {
	t.Helper()
	child, err := findParametersChild(db, parentID)
	if err != nil || child == nil {
		t.Fatalf("parameters child of %d: %v %#v", parentID, err, child)
	}
	return child
}

func assignmentAtScope(t *testing.T, db *gorm.DB, defID, scopeID uint) *models.ConfigAssignment {
	t.Helper()
	var a models.ConfigAssignment
	err := db.Where("variable_def_id = ? AND scope_id = ?", defID, scopeID).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return &a
}

func intValue(t *testing.T, raw json.RawMessage) int64 {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	n, ok := asInt(v)
	if !ok {
		t.Fatalf("not int: %#v", v)
	}
	return n
}

func TestMoveAssignmentsThenListWinning(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, ifaceScope, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: deviceScope.ID, Value: jsonRaw(t, 9000)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: ifaceScope.ID, Value: jsonRaw(t, 9100)})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	for _, s := range []models.ConfigScope{folder, deviceScope, ifaceScope} {
		if assignmentAtScope(t, db, def.ID, s.ID) != nil {
			t.Fatalf("original assignment still on %s scope %d", s.Kind, s.ID)
		}
		child := mustParametersChild(t, db, s.ID)
		cp := assignmentAtScope(t, db, def.ID, child.ID)
		if cp == nil {
			t.Fatalf("missing child copy on %s parameters child", s.Kind)
		}
	}
	child := mustParametersChild(t, db, folder.ID)
	var n int64
	if err := db.Model(&models.ConfigAssignment{}).Where("variable_def_id = ?", def.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("assignment count = %d, want 3 (one per parameters child; MOVE is idempotent)", n)
	}
	rows, err := ListAssignments(db, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("GET folder rows = %d, want 1", len(rows))
	}
	if rows[0].ScopeID != child.ID {
		t.Fatalf("winner scope_id = %d, want child %d", rows[0].ScopeID, child.ID)
	}
	v, src, err := resolveDefAt(db, &def, &folder, "")
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || src.ID != child.ID {
		t.Fatalf("resolve source = %+v, want child %d", src, child.ID)
	}
	if n, _ := asInt(v); n != 1500 {
		t.Errorf("resolve value = %v, want 1500", v)
	}
}

func TestMoveKeepsUnmatchedOriginalAndNamedExtra(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	copied := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &copied)
	orphan := models.ConfigVariableDef{Name: "asn", Type: models.VarTypeInt}
	mustCreate(t, db, &orphan)
	extra := models.ConfigVariableDef{Name: "ntp_server", Type: models.VarTypeInt}
	mustCreate(t, db, &extra)

	child, err := ensureParametersChild(db, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: copied.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: copied.ID, ScopeID: child.ID, Value: jsonRaw(t, 1500)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: orphan.ID, ScopeID: folder.ID, Value: jsonRaw(t, 65000)})

	ntp, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: extra.ID, ScopeID: ntp.ID, Value: jsonRaw(t, 1234)})

	if err := moveAssignmentsOntoParameterChildren(db); err != nil {
		t.Fatal(err)
	}
	if assignmentAtScope(t, db, copied.ID, folder.ID) != nil {
		t.Fatal("matched original should be deleted")
	}
	if assignmentAtScope(t, db, copied.ID, child.ID) == nil {
		t.Fatal("child copy should remain")
	}
	got := assignmentAtScope(t, db, orphan.ID, folder.ID)
	if got == nil || intValue(t, got.Value) != 65000 {
		t.Fatalf("unmatched original = %+v, want 65000 on folder", got)
	}
	named := assignmentAtScope(t, db, extra.ID, ntp.ID)
	if named == nil || intValue(t, named.Value) != 1234 {
		t.Fatalf("named extra assignment = %+v, want 1234", named)
	}
}

func TestUpsertFolderRemapsToChildAndResolve(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	row, err := UpsertAssignment(db, def.ID, folder.ID, jsonRaw(t, 9100))
	if err != nil {
		t.Fatal(err)
	}
	child := mustParametersChild(t, db, folder.ID)
	if row.ScopeID != child.ID {
		t.Fatalf("PUT folder returned scope %d, want child %d", row.ScopeID, child.ID)
	}
	v, src, err := resolveDefAt(db, &def, &folder, "")
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || src.ID != child.ID {
		t.Fatalf("resolve source = %+v, want child %d", src, child.ID)
	}
	if n, _ := asInt(v); n != 9100 {
		t.Errorf("resolve value = %v, want 9100", v)
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("PUT folder recreated an original on the folder")
	}
}

func TestUpsertReservedParametersChildDoesNotRecreateOriginal(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	child := mustParametersChild(t, db, folder.ID)
	if _, err := UpsertAssignment(db, def.ID, child.ID, jsonRaw(t, 9200)); err != nil {
		t.Fatal(err)
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("PUT to reserved child recreated an original")
	}
	cp := assignmentAtScope(t, db, def.ID, child.ID)
	if cp == nil || intValue(t, cp.Value) != 9200 {
		t.Fatalf("child = %+v, want 9200", cp)
	}
}

func TestDeleteWinnerRemovesOriginalAndResolve(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	rows, err := ListAssignments(db, folder.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %d err=%v", len(rows), err)
	}
	if err := DeleteAssignment(db, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, err = ListAssignments(db, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("GET folder after delete = %d, want 0", len(rows))
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("original still present after dual-delete")
	}
	v, src, err := resolveDefAt(db, &def, &folder, "")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil || src != nil {
		t.Fatalf("resolve after delete value=%v source=%+v", v, src)
	}
}

func TestUpsertSecretUnchanged(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "pw", Type: models.VarTypeSecret, Secret: true}
	mustCreate(t, db, &def)
	if _, err := UpsertAssignment(db, def.ID, folder.ID, jsonRaw(t, "s3cret")); err != nil {
		t.Fatal(err)
	}
	child, err := findParametersChild(db, folder.ID)
	if err != nil || child == nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, def.ID, folder.ID, jsonRaw(t, "***")); err != nil {
		t.Fatal(err)
	}
	cp := assignmentAtScope(t, db, def.ID, child.ID)
	if cp == nil {
		t.Fatal("expected child row")
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("secret PUT recreated an original")
	}
	var cv any
	if err := json.Unmarshal(cp.Value, &cv); err != nil {
		t.Fatal(err)
	}
	if cv != "s3cret" {
		t.Fatalf("stored child=%#v, want s3cret (*** must not persist)", cv)
	}
}

func TestUpsertNamedParameterObjectDoesNotWriteParent(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	ntp, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, def.ID, ntp.ID, jsonRaw(t, 1234)); err != nil {
		t.Fatal(err)
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("extra named parameter PUT wrote the parent original")
	}
	if child, _ := findParametersChild(db, folder.ID); child != nil {
		if assignmentAtScope(t, db, def.ID, child.ID) != nil {
			t.Fatal("extra named parameter PUT wrote the reserved parameters child")
		}
	}
	got := assignmentAtScope(t, db, def.ID, ntp.ID)
	if got == nil || intValue(t, got.Value) != 1234 {
		t.Fatalf("ntp assignment = %+v", got)
	}
}

func TestDeleteReservedParametersScopeDeletesOriginals(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	child, err := findParametersChild(db, folder.ID)
	if err != nil || child == nil {
		t.Fatal(err)
	}
	if err := DeleteScope(db, child.ID); err != nil {
		t.Fatal(err)
	}
	if assignmentAtScope(t, db, def.ID, folder.ID) != nil {
		t.Fatal("original survived DeleteScope of reserved parameters child")
	}
	if assignmentAtScope(t, db, def.ID, child.ID) != nil {
		t.Fatal("child assignment survived DeleteScope")
	}
}

func TestDeleteNamedParameterScopeLeavesParentOriginal(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	ntp, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, def.ID, ntp.ID, jsonRaw(t, 1234)); err != nil {
		t.Fatal(err)
	}
	if err := DeleteScope(db, ntp.ID); err != nil {
		t.Fatal(err)
	}
	orig := assignmentAtScope(t, db, def.ID, folder.ID)
	if orig == nil || intValue(t, orig.Value) != 1500 {
		t.Fatalf("parent original = %+v, want 1500", orig)
	}
	if assignmentAtScope(t, db, def.ID, ntp.ID) != nil {
		t.Fatal("named parameter assignments not deleted")
	}
}

func TestResolvePrefersParameterChild(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	child := models.ConfigScope{
		ParentID: &folder.ID, Name: models.ConfigParametersChildName,
		Kind: models.ConfigScopeKindParameter, Enabled: true,
	}
	mustCreate(t, db, &child)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: child.ID, Value: jsonRaw(t, 9000)})
	v, src, err := resolveDefAt(db, &def, &folder, "")
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || src.ID != child.ID {
		t.Fatalf("source = %+v, want child", src)
	}
	if n, _ := asInt(v); n != 9000 {
		t.Errorf("value = %v, want child 9000", v)
	}
}

func TestResolveIgnoresOrganizationalAssignment(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	v, src, err := resolveDefAt(db, &def, &folder, "")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil || src != nil {
		t.Fatalf("leftover folder assignment value=%v source=%+v, want ignored", v, src)
	}
}

func TestUpdateScopePayloadAndEnabled(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	node, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter, SortOrder: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !node.Enabled {
		t.Fatal("create should default Enabled")
	}
	payload := models.ConfigScopePayload{Platforms: []string{"eos"}, Description: "NTP"}
	updated, err := UpdateScope(db, node.ID, &models.ConfigScopeDTO{Payload: &payload})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Payload.Description != "NTP" || len(updated.Payload.Platforms) != 1 || updated.Payload.Platforms[0] != "eos" {
		t.Fatalf("payload = %+v", updated.Payload)
	}
	if !updated.Enabled {
		t.Fatal("name-less payload patch disabled the node")
	}
	if updated.SortOrder != 4 {
		t.Fatalf("sort_order = %d, want 4", updated.SortOrder)
	}
	name := "qos-core"
	renamed, err := UpdateScope(db, node.ID, &models.ConfigScopeDTO{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "qos-core" {
		t.Fatalf("name = %s", renamed.Name)
	}
	if renamed.Payload.Description != "NTP" || !renamed.Enabled {
		t.Fatalf("rename wiped payload/enabled: %+v", renamed)
	}
	en := false
	disabled, err := UpdateScope(db, node.ID, &models.ConfigScopeDTO{Enabled: &en})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("expected enabled=false")
	}
	if disabled.Payload.Description != "NTP" {
		t.Fatalf("disable wiped payload: %+v", disabled.Payload)
	}
}

func TestResolveParameterObjectFilter(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, _, _ := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})

	eosChild := models.ConfigScope{
		ParentID: &folder.ID, Name: models.ConfigParametersChildName,
		Kind: models.ConfigScopeKindParameter, Enabled: true, SortOrder: 1,
		Payload: models.ConfigScopePayload{Platforms: []string{"eos"}},
	}
	mustCreate(t, db, &eosChild)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: eosChild.ID, Value: jsonRaw(t, 9000)})

	v, src, err := resolveDefAt(db, &def, &folder, "ios-xr")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil || src != nil {
		t.Fatalf("ios-xr source = %+v value=%v, want no organizational fallback", src, v)
	}

	v, src, err = resolveDefAt(db, &def, &folder, "sros-md")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil || src != nil {
		t.Fatalf("sros-md vs eos-only source = %+v, want no organizational fallback", src)
	}

	srosChild := models.ConfigScope{
		ParentID: &folder.ID, Name: "sros-knobs",
		Kind: models.ConfigScopeKindParameter, Enabled: true, SortOrder: 2,
		Payload: models.ConfigScopePayload{Platforms: []string{"sros"}},
	}
	mustCreate(t, db, &srosChild)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: srosChild.ID, Value: jsonRaw(t, 9100)})

	v, src, err = resolveDefAt(db, &def, &folder, "sros-md")
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || src.ID != srosChild.ID {
		t.Fatalf("sros-md source = %+v, want sros child", src)
	}
	if n, _ := asInt(v); n != 9100 {
		t.Errorf("sros-md value = %v, want 9100", v)
	}

	eosChild.Enabled = false
	if err := db.Save(&eosChild).Error; err != nil {
		t.Fatal(err)
	}
	v, src, err = resolveDefAt(db, &def, &folder, "eos")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil || src != nil {
		t.Fatalf("disabled child source = %+v, want no organizational fallback", src)
	}

	other := models.ConfigScope{
		ParentID: &folder.ID, Name: "ntp",
		Kind: models.ConfigScopeKindParameter, Enabled: true, SortOrder: 0,
	}
	mustCreate(t, db, &other)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: other.ID, Value: jsonRaw(t, 1234)})
	v, src, err = resolveDefAt(db, &def, &folder, "eos")
	if err != nil {
		t.Fatal(err)
	}
	if src == nil || src.ID != other.ID {
		t.Fatalf("fallback child source = %+v, want ntp", src)
	}
	if n, _ := asInt(v); n != 1234 {
		t.Errorf("fallback child value = %v, want 1234", v)
	}
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected status %d, got nil", status)
	}
	se := AsStatusError(err)
	if se == nil {
		t.Fatalf("expected StatusError %d, got %v", status, err)
	}
	if se.Status != status {
		t.Fatalf("status = %d (%s), want %d", se.Status, se.Message, status)
	}
}

func TestMoveFolderUnderFolderOKUnderDeviceRejected(t *testing.T) {
	db := newTestDB(t)
	global, folder, deviceScope, _, _ := seedTree(t, db)
	child, err := CreateScope(db, &models.ConfigScope{
		ParentID: &global.ID, Name: "sites", Kind: models.ConfigScopeKindFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := MoveScope(db, child.ID, folder.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ParentID == nil || *moved.ParentID != folder.ID {
		t.Fatalf("parent = %v, want folder %d", moved.ParentID, folder.ID)
	}
	_, err = MoveScope(db, child.ID, deviceScope.ID, nil)
	wantStatus(t, err, 400)
}

func TestMoveInterfaceRejected(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, ifaceScope, _ := seedTree(t, db)
	_, err := MoveScope(db, ifaceScope.ID, folder.ID, nil)
	wantStatus(t, err, 409)
	err = DeleteScope(db, ifaceScope.ID)
	wantStatus(t, err, 409)
}

func TestMoveCycleRejected(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	a := models.ConfigScope{ParentID: &root.ID, Name: "a", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &a)
	b := models.ConfigScope{ParentID: &a.ID, Name: "b", Kind: models.ConfigScopeKindFolder}
	mustCreate(t, db, &b)
	_, err = MoveScope(db, a.ID, b.ID, nil)
	wantStatus(t, err, 400)
	if se := AsStatusError(err); se != nil && se.Message != "scope parent cycle" {
		t.Errorf("message = %q, want scope parent cycle", se.Message)
	}
}

func TestGrandfatherIllegalParentNameUpdate(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, _, _ := seedTree(t, db)
	illegal := models.ConfigScope{
		ParentID: &deviceScope.ID, Name: "under-device", Kind: models.ConfigScopeKindFolder,
	}
	mustCreate(t, db, &illegal)
	name := "still-there"
	updated, err := UpdateScope(db, illegal.ID, &models.ConfigScopeDTO{Name: &name})
	if err != nil {
		t.Fatalf("name-only update of grandfathered node: %v", err)
	}
	if updated.Name != name {
		t.Fatalf("name = %s", updated.Name)
	}
	if updated.ParentID == nil || *updated.ParentID != deviceScope.ID {
		t.Fatal("name-only update reparented")
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &deviceScope.ID, Name: "new-folder", Kind: models.ConfigScopeKindFolder,
	})
	wantStatus(t, err, 400)
	_, err = MoveScope(db, folder.ID, deviceScope.ID, nil)
	wantStatus(t, err, 400)
}

func TestKindChangeAssertsParentMatrix(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, _, _ := seedTree(t, db)
	param, err := CreateScope(db, &models.ConfigScope{
		ParentID: &deviceScope.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	folderKind := models.ConfigScopeKindFolder
	_, err = UpdateScope(db, param.ID, &models.ConfigScopeDTO{Kind: &folderKind})
	wantStatus(t, err, 400)
	got, err := GetScope(db, param.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != models.ConfigScopeKindParameter {
		t.Fatalf("kind = %s, want parameter (rejected kind change must not persist)", got.Kind)
	}

	illegal := models.ConfigScope{
		ParentID: &deviceScope.ID, Name: "under-device", Kind: models.ConfigScopeKindFolder,
	}
	mustCreate(t, db, &illegal)
	paramKind := models.ConfigScopeKindParameter
	updated, err := UpdateScope(db, illegal.ID, &models.ConfigScopeDTO{Kind: &paramKind})
	if err != nil {
		t.Fatalf("folder→parameter under device: %v", err)
	}
	if updated.Kind != models.ConfigScopeKindParameter {
		t.Fatalf("kind = %s, want parameter", updated.Kind)
	}

	child, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "from-folder", Kind: models.ConfigScopeKindFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	moved, err := UpdateScope(db, child.ID, &models.ConfigScopeDTO{
		ParentID: &deviceScope.ID, Kind: &paramKind,
	})
	if err != nil {
		t.Fatalf("combined parent+kind to parameter under device: %v", err)
	}
	if moved.Kind != models.ConfigScopeKindParameter {
		t.Fatalf("kind = %s, want parameter", moved.Kind)
	}
	if moved.ParentID == nil || *moved.ParentID != deviceScope.ID {
		t.Fatalf("parent = %v, want device %d", moved.ParentID, deviceScope.ID)
	}
}

func TestCannotReparentReservedFolders(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "lab-reserved", Kind: models.ConfigScopeKindFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := scopeChild(t, db, root.ID, models.ConfigCatalogName)
	services := scopeChild(t, db, root.ID, models.ConfigServicesFolderName)
	_, err = MoveScope(db, catalog.ID, folder.ID, nil)
	wantStatus(t, err, 400)
	_, err = MoveScope(db, services.ID, folder.ID, nil)
	wantStatus(t, err, 400)
	still, err := GetScope(db, catalog.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.ParentID == nil || *still.ParentID != root.ID {
		t.Fatalf("catalog parent = %v, want global", still.ParentID)
	}
}

func TestDetachDeviceKeepsDCIMRemovesSubtree(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, ifaceScope, iface := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	mustCreate(t, db, &def)
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: ifaceScope.ID, Value: jsonRaw(t, 1500)})
	cli := models.ConfigScope{
		ParentID: &deviceScope.ID, Name: "banner", Kind: models.ConfigScopeKindCLI,
		Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	}
	mustCreate(t, db, &cli)
	feat := models.ConfigCLIFeature{ScopeID: cli.ID, Name: "apply", AddCommands: "banner motd"}
	mustCreate(t, db, &feat)
	svcRow := models.Service{ServiceID: "CN00099", ServiceType: "ELINE"}
	mustCreate(t, db, &svcRow)
	sid := svcRow.ID
	svcScope := models.ConfigScope{
		ParentID: &deviceScope.ID, Name: svcRow.ServiceID, Kind: models.ConfigScopeKindService, ServiceID: &sid,
	}
	mustCreate(t, db, &svcScope)

	if err := DetachDevice(db, deviceScope.ID); err != nil {
		t.Fatal(err)
	}

	var dev models.Device
	if err := db.First(&dev, iface.DeviceID).Error; err != nil {
		t.Fatalf("device row deleted: %v", err)
	}
	if _, err := GetScope(db, deviceScope.ID); err == nil {
		t.Fatal("device scope still present")
	}
	if _, err := GetScope(db, ifaceScope.ID); err == nil {
		t.Fatal("interface scope still present")
	}
	if _, err := GetScope(db, cli.ID); err == nil {
		t.Fatal("cli scope still present")
	}
	var nFeat int64
	if err := db.Model(&models.ConfigCLIFeature{}).Where("id = ?", feat.ID).Count(&nFeat).Error; err != nil {
		t.Fatal(err)
	}
	if nFeat != 0 {
		t.Fatal("cli feature survived detach")
	}
	var nAssign int64
	if err := db.Model(&models.ConfigAssignment{}).Where("scope_id = ?", ifaceScope.ID).Count(&nAssign).Error; err != nil {
		t.Fatal(err)
	}
	if nAssign != 0 {
		t.Fatal("assignments survived detach")
	}
	reloaded, err := GetScope(db, svcScope.ID)
	if err != nil {
		t.Fatalf("service scope deleted: %v", err)
	}
	if reloaded.ParentID == nil || *reloaded.ParentID != folder.ID {
		t.Fatalf("service parent = %v, want folder %d", reloaded.ParentID, folder.ID)
	}
}

func TestMoveDeviceRefreshesInterfaces(t *testing.T) {
	db := newTestDB(t)
	global, _, deviceScope, ifaceScope, iface := seedTree(t, db)
	other := models.Interface{DeviceID: iface.DeviceID, Name: "Ethernet2", Type: "1000base-t", NetboxID: 2}
	mustCreate(t, db, &other)
	moved, err := MoveScope(db, deviceScope.ID, global.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ParentID == nil || *moved.ParentID != global.ID {
		t.Fatalf("parent = %v, want global %d", moved.ParentID, global.ID)
	}
	found, err := scopeByInterfaceID(db, other.ID)
	if err != nil || found == nil {
		t.Fatalf("new interface child missing: %v", err)
	}
	old, err := GetScope(db, ifaceScope.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.ParentID == nil || *old.ParentID != deviceScope.ID {
		t.Fatal("existing interface child reparented")
	}
}

func TestCompileContextPattern(t *testing.T) {
	re, err := CompileContextPattern("interface <name>")
	if err != nil {
		t.Fatal(err)
	}
	if re == nil {
		t.Fatal("expected compiled regex")
	}
	if got, want := re.String(), `^interface\s+(?P<name>\S+)$`; got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}
	re, err = CompileContextPattern("router bgp <as>")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := re.String(), `^router\s+bgp\s+(?P<as>\S+)$`; got != want {
		t.Errorf("bgp pattern = %q, want %q", got, want)
	}
	for _, p := range []string{"", "global", "GLOBAL", "  global  "} {
		re, err = CompileContextPattern(p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		if re != nil {
			t.Errorf("%q: got %v, want nil", p, re)
		}
	}
	if _, err := CompileContextPattern("interface <name"); err == nil {
		t.Fatal("expected invalid capture error")
	}
}

func TestWrapEmptyContext(t *testing.T) {
	db := newTestDB(t)
	feat := models.ConfigCLIFeature{
		RemoveCommands: "no mtu",
		AddCommands:    `mtu {{index .Vars "mtu"}}`,
	}
	data := BaselineRenderData{Vars: map[string]any{"mtu": 9100}}
	got, err := RenderCLIFeature(db, nil, &feat, data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"no mtu", "mtu 9100"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	empty := &models.CLIContext{Pattern: "global", Enter: "", Exit: "exit"}
	got, err = RenderCLIFeature(db, empty, &feat, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("global ctx got %v, want %v", got, want)
	}
}

func TestWrapEnterRemoveAtRootFalse(t *testing.T) {
	db := newTestDB(t)
	ctx := &models.CLIContext{
		Pattern: "interface <name>",
		Enter:   "interface {{.LocalIface}}",
		Exit:    "exit",
	}
	feat := models.ConfigCLIFeature{
		RemoveCommands: "no mtu",
		AddCommands:    `mtu {{index .Vars "mtu"}}`,
	}
	data := BaselineRenderData{LocalIface: "Ethernet1", Vars: map[string]any{"mtu": 9100}}
	got, err := RenderCLIFeature(db, ctx, &feat, data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"interface Ethernet1", "no mtu", "mtu 9100", "exit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWrapEnterRemoveAtRootTrue(t *testing.T) {
	db := newTestDB(t)
	ctx := &models.CLIContext{
		Pattern: "interface <name>",
		Enter:   "interface {{.LocalIface}}",
		Exit:    "exit",
	}
	feat := models.ConfigCLIFeature{
		RemoveCommands: "no mtu",
		AddCommands:    `mtu {{index .Vars "mtu"}}`,
		RemoveAtRoot:   true,
	}
	data := BaselineRenderData{LocalIface: "Ethernet1", Vars: map[string]any{"mtu": 9100}}
	got, err := RenderCLIFeature(db, ctx, &feat, data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"no mtu", "interface Ethernet1", "mtu 9100", "exit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLookupCLIObjectSROSMDFallback(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	elineMD, err := LookupCLIObject(db, "ELINE", "sros-md")
	if err != nil || elineMD == nil {
		t.Fatalf("seeded sros-md: %v %#v", err, elineMD)
	}
	if elineMD.Platform != "sros-md" {
		t.Fatalf("platform = %s, want sros-md", elineMD.Platform)
	}

	var elan models.ServiceType
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	obj, err := LookupCLIObject(db, "ELAN", "sros-md")
	if err != nil {
		t.Fatal(err)
	}
	if obj != nil {
		t.Fatalf("unexpected CLI object %+v", obj)
	}
	sros := models.ConfigScope{
		ParentID: &root.ID, Name: "sros", Kind: models.ConfigScopeKindCLI,
		Platform: "sros", PayloadKind: models.PayloadKindCLI, Enabled: true,
		ServiceTypeID: &elan.ID,
	}
	mustCreate(t, db, &sros)
	got, err := LookupCLIObject(db, "ELAN", "sros-md")
	if err != nil || got == nil {
		t.Fatalf("fallback: %v %#v", err, got)
	}
	if got.ID != sros.ID {
		t.Fatalf("got id %d, want sros %d", got.ID, sros.ID)
	}
	md := models.ConfigScope{
		ParentID: &root.ID, Name: "sros-md", Kind: models.ConfigScopeKindCLI,
		Platform: "sros-md", PayloadKind: models.PayloadKindCLI, Enabled: true,
		ServiceTypeID: &elan.ID,
	}
	mustCreate(t, db, &md)
	got, err = LookupCLIObject(db, "ELAN", "sros-md")
	if err != nil || got == nil {
		t.Fatalf("dedicated: %v %#v", err, got)
	}
	if got.ID != md.ID {
		t.Fatalf("got id %d, want sros-md %d", got.ID, md.ID)
	}
}

func TestRenderDeviceIncludesBaselineCLI(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-cli", Platform: "eos"}
	mustCreate(t, db, &dev)
	cli, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "ntp", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cli.PayloadKind != models.PayloadKindCLI {
		t.Fatalf("payload_kind = %q, want cli", cli.PayloadKind)
	}
	mustCreate(t, db, &models.ConfigCLIFeature{
		ScopeID: cli.ID, Name: "servers", SortOrder: 0,
		AddCommands: "ntp server 1.1.1.1",
	})
	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range out.Sources {
		if s.Source == "cli:ntp" {
			found = true
			if s.Kind != "cli" {
				t.Errorf("kind = %s", s.Kind)
			}
			if s.Error != "" {
				t.Fatalf("render error: %s", s.Error)
			}
			if !reflect.DeepEqual(s.Commands, []string{"ntp server 1.1.1.1"}) {
				t.Errorf("commands = %v", s.Commands)
			}
		}
	}
	if !found {
		t.Fatalf("baseline CLI missing from RenderDevice: %+v", out.Sources)
	}
}

func elineRenderIntent() GenericRenderData {
	return GenericRenderData{
		Name:             "CN00570",
		Description:      "ID=CN00570 Acme AB",
		LocalIface:       "Ethernet1",
		LocalVLAN:        100,
		ServiceNumericID: 1000570,
		SDPID:            28,
		Remote: &ELINERemote{
			NeighborIP:   "172.27.250.28",
			PseudowireID: 1000570,
			MTU:          9100,
			ControlWord:  true,
			DeviceName:   "pe2",
			RemoteIface:  "Ethernet2",
			RemoteVLAN:   200,
		},
	}
}

func TestRenderSeededELINECLIMatchesEmbed(t *testing.T) {
	db := newTestDB(t)
	intent := elineRenderIntent()
	embeds := map[string]string{
		"eos":    templates.EOSEline,
		"ios-xr": templates.IOSXREline,
		"sros":   templates.SROSEline,
	}
	for plat, body := range embeds {
		want, err := Render(db, body, "", intent)
		if err != nil {
			t.Fatalf("%s embed: %v", plat, err)
		}
		obj, err := LookupCLIObject(db, "ELINE", plat)
		if err != nil || obj == nil {
			t.Fatalf("%s CLI: %v %#v", plat, err, obj)
		}
		got, err := RenderCLIObject(db, obj, intent)
		if err != nil {
			t.Fatalf("%s CLI render: %v", plat, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s CLI cmds =\n%v\nwant embed\n%v", plat, got, want)
		}
	}
}

func TestSeedLeavesEditedCLIFeature(t *testing.T) {
	db := newTestDB(t)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(feats) != 1 {
		t.Fatalf("features: %v %+v", err, feats)
	}
	feats[0].AddCommands = "edited-add {{.Name}}"
	if err := db.Save(&feats[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(again) != 1 {
		t.Fatalf("after seed: %v %+v", err, again)
	}
	if again[0].AddCommands != "edited-add {{.Name}}" {
		t.Errorf("feature overwritten: %q", again[0].AddCommands)
	}
}

func TestSeedLeavesEditedCLIContext(t *testing.T) {
	db := newTestDB(t)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(feats) != 1 {
		t.Fatalf("features: %v %+v", err, feats)
	}
	wantAdd := feats[0].AddCommands
	wantRemove := feats[0].RemoveCommands
	obj.Payload.Context = &models.CLIContext{Enter: "interface {{.LocalIface}}"}
	if err := db.Save(obj).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || again == nil {
		t.Fatalf("after seed: %v %#v", err, again)
	}
	if again.Payload.Context == nil || again.Payload.Context.Enter != "interface {{.LocalIface}}" {
		t.Errorf("context reset: %+v", again.Payload.Context)
	}
	feats2, err := ListCLIFeatures(db, again.ID)
	if err != nil || len(feats2) != 1 {
		t.Fatalf("features after seed: %v %+v", err, feats2)
	}
	if feats2[0].AddCommands != wantAdd || feats2[0].RemoveCommands != wantRemove {
		t.Fatal("features reset after context edit")
	}
}

func TestRenderGenericUsesCLIObjectWhenPresent(t *testing.T) {
	db := newTestDB(t)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	feats, err := ListCLIFeatures(db, obj.ID)
	if err != nil || len(feats) != 1 {
		t.Fatalf("features: %v %+v", err, feats)
	}
	feats[0].AddCommands = "cli-marker {{.Name}}"
	feats[0].RemoveCommands = "no cli-marker {{.Name}}"
	if err := db.Save(&feats[0]).Error; err != nil {
		t.Fatal(err)
	}

	cust := models.Customer{Name: "Acme AB"}
	mustCreate(t, db, &cust)
	pe1 := models.Device{Name: "pe-cli-use", Platform: "eos", NetboxID: 801}
	pe2 := models.Device{Name: "pe-cli-use-b", Platform: "eos", NetboxID: 802}
	mustCreate(t, db, &pe1)
	mustCreate(t, db, &pe2)
	ifa := models.Interface{DeviceID: pe1.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 811}
	ifb := models.Interface{DeviceID: pe2.ID, Name: "Ethernet2", Type: "1000base-t", NetboxID: 821}
	mustCreate(t, db, &ifa)
	mustCreate(t, db, &ifb)
	lo1 := models.Interface{DeviceID: pe1.ID, Name: "Loopback0", Type: "virtual", NetboxID: 812}
	lo2 := models.Interface{DeviceID: pe2.ID, Name: "Loopback0", Type: "virtual", NetboxID: 822}
	mustCreate(t, db, &lo1)
	mustCreate(t, db, &lo2)
	mustCreate(t, db, &models.Address{InterfaceID: lo1.ID, Address: "10.0.0.1/32"})
	mustCreate(t, db, &models.Address{InterfaceID: lo2.ID, Address: "10.0.0.2/32"})
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00570", ServiceType: "ELINE", PseudowireID: 1000570}
	mustCreate(t, db, &svc)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "a", DeviceID: pe1.ID, InterfaceID: ifa.ID,
		Fields: EncodeEndpointFields(100, 0, 0),
	})
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "b", DeviceID: pe2.ID, InterfaceID: ifb.ID,
		Fields: EncodeEndpointFields(200, 0, 0),
	})

	out, err := RenderService(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var joined string
	for _, src := range out {
		if src.Error != "" {
			t.Fatalf("render error: %s", src.Error)
		}
		joined += strings.Join(src.Commands, "\n") + "\n"
	}
	if !strings.Contains(joined, "cli-marker CN00570") {
		t.Errorf("CLI object not used: %s", joined)
	}
	if strings.Contains(joined, "neighbor 10.0.0.2") {
		t.Errorf("pack body leaked while CLI object exists: %s", joined)
	}
}

func TestRenderGenericErrorsWhenELINECLIDeleted(t *testing.T) {
	db := newTestDB(t)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	if err := DeleteScope(db, obj.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := LookupCLIObject(db, "ELINE", "eos"); err != nil || again != nil {
		t.Fatalf("CLI still present: %v %#v", err, again)
	}

	dev := models.Device{Name: "pe-no-cli", Platform: "eos", NetboxID: 901}
	mustCreate(t, db, &dev)
	svc := models.Service{ServiceID: "CN00571", ServiceType: "ELINE", PseudowireID: 1000571}
	mustCreate(t, db, &svc)
	out := renderGenericForDevice(db, &svc, &dev, nil)
	want := MissingCLIObjectMessage("ELINE", "eos")
	if len(out) != 1 || out[0].Error != want {
		t.Fatalf("sources = %+v, want %q", out, want)
	}
}

func TestRequireCLIObjectPayloadKind(t *testing.T) {
	if err := RequireCLIObject(nil); err == nil {
		t.Fatal("expected error for nil")
	}
	cli := &models.ConfigScope{PayloadKind: models.PayloadKindCLI}
	if err := RequireCLIObject(cli); err != nil {
		t.Fatal(err)
	}
	empty := &models.ConfigScope{}
	if err := RequireCLIObject(empty); err != nil {
		t.Fatalf("empty payload_kind should default to cli: %v", err)
	}
	nc := &models.ConfigScope{PayloadKind: models.PayloadKindNETCONF}
	if err := RequireCLIObject(nc); err == nil {
		t.Fatal("expected netconf reject")
	}
}

func TestExtractDefineBodyNestedRange(t *testing.T) {
	src := "{{define \"cleanup\"}}\nbefore\n{{range .StaleSubinterfaces}}\nno interface {{.Iface}}.{{.VLAN}}\n{{end}}\nafter\n{{end}}\nbody"
	got := extractDefineBody(src, "cleanup")
	want := "\nbefore\n{{range .StaleSubinterfaces}}\nno interface {{.Iface}}.{{.VLAN}}\n{{end}}\nafter\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCLIObjectChecksumIncludesContext(t *testing.T) {
	feats := []models.ConfigCLIFeature{{
		Name: "apply", AddCommands: "add", RemoveCommands: "remove",
	}}
	a := CLIObjectChecksum("eos", "cli", nil, feats)
	b := CLIObjectChecksum("eos", "cli", &models.CLIContext{Enter: "interface X"}, feats)
	if a == b {
		t.Fatal("setting enter should change checksum")
	}
	c := CLIObjectChecksum("eos", "cli", nil, feats)
	if a != c {
		t.Fatal("checksum not stable")
	}
	// Empty UpdateCommands still contributes "\nupdate\n" (documented concat).
	raw := "eos\ncli\nnull\napply\nremove_at_root=false\nadd\nadd\nupdate\n\nremove\nremove\n"
	if a != packChecksum(raw) {
		t.Fatalf("checksum = %s, want %s", a, packChecksum(raw))
	}
	rooted := []models.ConfigCLIFeature{{
		Name: "apply", AddCommands: "add", RemoveCommands: "remove", RemoveAtRoot: true,
	}}
	if CLIObjectChecksum("eos", "cli", nil, rooted) == a {
		t.Fatal("RemoveAtRoot should change checksum")
	}
	updated := []models.ConfigCLIFeature{{
		Name: "apply", AddCommands: "add", UpdateCommands: "set", RemoveCommands: "remove",
	}}
	if CLIObjectChecksum("eos", "cli", nil, updated) == a {
		t.Fatal("non-empty UpdateCommands should change checksum")
	}
}

func TestRenderGenericSurfacesLookupError(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "pe-lookup", Platform: "eos", NetboxID: 701}
	mustCreate(t, db, &dev)
	svc := models.Service{ServiceID: "CN00999", ServiceType: "NO-SUCH-TYPE"}
	mustCreate(t, db, &svc)
	out := renderGenericForDevice(db, &svc, &dev, nil)
	if len(out) != 1 || !strings.Contains(out[0].Error, `unknown service type "NO-SUCH-TYPE"`) {
		t.Fatalf("sources = %+v, want lookup error", out)
	}
}

func TestRenderGenericMissingTranslator(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "pe-elan", Platform: "eos", NetboxID: 702}
	mustCreate(t, db, &dev)
	svc := models.Service{ServiceID: "CN00998", ServiceType: "ELAN"}
	mustCreate(t, db, &svc)
	out := renderGenericForDevice(db, &svc, &dev, nil)
	want := MissingCLIObjectMessage("ELAN", "eos")
	if len(out) != 1 || out[0].Error != want {
		t.Fatalf("sources = %+v, want %q", out, want)
	}
}

func TestCreateScopeRejectsInvalidContextPattern(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "bad", Kind: models.ConfigScopeKindCLI, Platform: "eos",
		Payload: models.ConfigScopePayload{Context: &models.CLIContext{Pattern: "interface <name"}},
	})
	if err == nil {
		t.Fatal("expected invalid pattern")
	}
}

func TestMatrixWalksParameterAncestor(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, ifaceScope, iface := seedTree(t, db)
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt, DefaultValue: jsonRaw(t, 1500)}
	mustCreate(t, db, &def)
	param, err := CreateScope(db, &models.ConfigScope{
		ParentID: &folder.ID, Name: "qos", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Matrix(db, param.ID, "mtu")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.InterfaceID == iface.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("matrix from parameter missed interface: %+v (iface scope %d)", rows, ifaceScope.ID)
	}
}

func cliChild(t *testing.T, db *gorm.DB, parentID uint, name string) models.ConfigScope {
	t.Helper()
	var s models.ConfigScope
	if err := db.Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, parentID, name).First(&s).Error; err != nil {
		t.Fatalf("cli %s under %d: %v", name, parentID, err)
	}
	return s
}

type leftoverPackRow struct {
	ID            uint `gorm:"primaryKey"`
	ServiceTypeID uint
	Platform      string
}

func (leftoverPackRow) TableName() string { return "platform_packs" }

func createLeftoverPackTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&leftoverPackRow{}); err != nil {
		t.Fatal(err)
	}
}

func TestDropPacksRefusesWithoutCLITwin(t *testing.T) {
	db := newTestDB(t)
	createLeftoverPackTable(t, db)
	var eline models.ServiceType
	if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &leftoverPackRow{ServiceTypeID: eline.ID, Platform: "custom-nos"})
	err := AssertPacksHaveCLITwins(db)
	if err == nil {
		t.Fatal("expected refuse when a pack has no CLI twin")
	}
	if !strings.Contains(err.Error(), "cannot drop platform_packs") || !strings.Contains(err.Error(), "custom-nos") {
		t.Fatalf("err = %v", err)
	}
	if !db.Migrator().HasTable("platform_packs") {
		t.Fatal("assert must not drop platform_packs")
	}
}

func TestDropPacksWhenTwinsExist(t *testing.T) {
	db := newTestDB(t)
	createLeftoverPackTable(t, db)
	obj, err := LookupCLIObject(db, "ELINE", "eos")
	if err != nil || obj == nil {
		t.Fatalf("CLI: %v %#v", err, obj)
	}
	mustCreate(t, db, &leftoverPackRow{ServiceTypeID: *obj.ServiceTypeID, Platform: "eos"})
	if err := AssertPacksHaveCLITwins(db); err != nil {
		t.Fatalf("assert: %v", err)
	}
	if err := DropPackAndTemplateTables(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("platform_packs") {
		t.Fatal("platform_packs still present after drop")
	}
}

type leftoverTemplateRow struct {
	ID       uint `gorm:"primaryKey"`
	Name     string
	Platform string
	ScopeID  *uint
}

func (leftoverTemplateRow) TableName() string { return "config_templates" }

func createLeftoverTemplateTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&leftoverTemplateRow{}); err != nil {
		t.Fatal(err)
	}
}

func TestDropTemplatesRefusesWithoutCLITwin(t *testing.T) {
	db := newTestDB(t)
	createLeftoverTemplateTable(t, db)
	mustCreate(t, db, &leftoverTemplateRow{Name: "banner", Platform: "eos"})
	err := AssertTemplatesHaveCLITwins(db)
	if err == nil {
		t.Fatal("expected refuse when a template has no CLI twin")
	}
	if !strings.Contains(err.Error(), "cannot drop config_templates") || !strings.Contains(err.Error(), "banner") {
		t.Fatalf("err = %v", err)
	}
	if !db.Migrator().HasTable("config_templates") {
		t.Fatal("assert must not drop config_templates")
	}
}

func TestDropTemplatesWhenTwinsExist(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	})
	if err != nil {
		t.Fatal(err)
	}
	createLeftoverTemplateTable(t, db)
	mustCreate(t, db, &leftoverTemplateRow{Name: "banner", Platform: "eos"})
	if err := AssertTemplatesHaveCLITwins(db); err != nil {
		t.Fatalf("assert: %v", err)
	}
	if err := DropPackAndTemplateTables(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("config_templates") {
		t.Fatal("config_templates still present after drop")
	}
}

func TestDropTemplatesTranslationCLIIsNotTwin(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	var elan models.ServiceType
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI,
		Platform: "eos", ServiceTypeID: &elan.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	createLeftoverTemplateTable(t, db)
	mustCreate(t, db, &leftoverTemplateRow{Name: "banner", Platform: "eos"})
	err = AssertTemplatesHaveCLITwins(db)
	if err == nil {
		t.Fatal("translation CLI must not count as a baseline twin")
	}
	if !strings.Contains(err.Error(), "cannot drop config_templates") {
		t.Fatalf("err = %v", err)
	}
}

func walkTree(nodes []ScopeTreeNode, fn func(ScopeTreeNode)) {
	for _, n := range nodes {
		fn(n)
		walkTree(n.Children, fn)
	}
}

func TestReplaceEndpointsDoesNotValidate(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-partial", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	svc := models.Service{ServiceID: "CN00901", ServiceType: "ELINE"}
	mustCreate(t, db, &svc)
	if _, err := AttachService(db, folder.ID, svc.ID); err != nil {
		t.Fatal(err)
	}
	partial := []models.ServiceEndpoint{{
		Role: "a", DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(100, 0, 0),
	}}
	st, err := LookupServiceType(db, "ELINE")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEndpoints(db, st, partial); err == nil {
		t.Fatal("expected ValidateEndpoints to reject a-only ELINE")
	}
	if err := ReplaceEndpoints(db, svc.ID, partial); err != nil {
		t.Fatalf("ReplaceEndpoints rejected partial ELINE: %v", err)
	}
	got, err := ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != "a" {
		t.Fatalf("stored = %+v, want one role a", got)
	}
}

func TestELANSamePortDifferentVLANTwoRefs(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, ifaceScope, iface := seedTree(t, db)
	svc := models.Service{ServiceID: "CN00902", ServiceType: "ELAN"}
	mustCreate(t, db, &svc)
	if _, err := AttachService(db, folder.ID, svc.ID); err != nil {
		t.Fatal(err)
	}
	eps := []models.ServiceEndpoint{
		{Role: "endpoint", DeviceID: iface.DeviceID, InterfaceID: iface.ID, Fields: EncodeEndpointFields(100, 0, 0)},
		{Role: "endpoint", DeviceID: iface.DeviceID, InterfaceID: iface.ID, Fields: EncodeEndpointFields(200, 0, 0)},
	}
	if EndpointIdentity(eps[0]) == EndpointIdentity(eps[1]) {
		t.Fatal("expected distinct endpointIdentity for VLAN 100 vs 200")
	}
	if err := ReplaceEndpoints(db, svc.ID, eps); err != nil {
		t.Fatal(err)
	}
	canon, err := scopeByServiceID(db, svc.ID)
	if err != nil || canon == nil {
		t.Fatalf("canonical: %v", err)
	}
	var kids []models.ConfigScope
	if err := db.Where("parent_id = ? AND kind = ?", canon.ID, models.ConfigScopeKindServiceEndpoint).
		Find(&kids).Error; err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 {
		t.Fatalf("endpoint children = %d, want 2", len(kids))
	}
	seen := map[string]bool{}
	for i := range kids {
		seen[identityFromEndpointScope(&kids[i])] = true
	}
	if len(seen) != 2 {
		t.Fatalf("child identities = %v", seen)
	}
	tree, err := ScopeTree(db)
	if err != nil {
		t.Fatal(err)
	}
	var refs []ScopeTreeNode
	walkTree(tree, func(n ScopeTreeNode) {
		if n.Type == models.ConfigScopeKindServiceRef && n.Data.ParentID != nil && *n.Data.ParentID == ifaceScope.ID {
			refs = append(refs, n)
		}
	})
	if len(refs) != 2 {
		t.Fatalf("refs under interface = %d, want 2 (%+v)", len(refs), refs)
	}
	if refs[0].Key == refs[1].Key {
		t.Fatalf("ref keys collided: %s", refs[0].Key)
	}
}

func TestProjectEndpointScopesDedupsIdentity(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-dup", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	svc := models.Service{ServiceID: "CN00930", ServiceType: "ELAN"}
	mustCreate(t, db, &svc)
	if _, err := AttachService(db, folder.ID, svc.ID); err != nil {
		t.Fatal(err)
	}
	same := models.ServiceEndpoint{
		Role: "endpoint", DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(10, 0, 0),
	}
	if err := ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{same, same}); err != nil {
		t.Fatal(err)
	}
	canon, err := scopeByServiceID(db, svc.ID)
	if err != nil || canon == nil {
		t.Fatal(err)
	}
	countKids := func() int {
		t.Helper()
		var n int64
		if err := db.Model(&models.ConfigScope{}).
			Where("parent_id = ? AND kind = ?", canon.ID, models.ConfigScopeKindServiceEndpoint).
			Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return int(n)
	}
	if got := countKids(); got != 1 {
		t.Fatalf("children after duplicate table rows = %d, want 1", got)
	}
	if err := projectEndpointScopes(db, svc.ID); err != nil {
		t.Fatal(err)
	}
	if got := countKids(); got != 1 {
		t.Fatalf("children after second project = %d, want 1", got)
	}
}

func TestVirtualRefsSkipUnattached(t *testing.T) {
	db := newTestDB(t)
	_, _, _, _, iface := seedTree(t, db)
	svc := models.Service{ServiceID: "CN00931", ServiceType: "ELAN"}
	mustCreate(t, db, &svc)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "endpoint", DeviceID: iface.DeviceID, InterfaceID: iface.ID,
		Fields: EncodeEndpointFields(10, 0, 0),
	})
	tree, err := ScopeTree(db)
	if err != nil {
		t.Fatal(err)
	}
	var refs int
	walkTree(tree, func(n ScopeTreeNode) {
		if n.Type == models.ConfigScopeKindServiceRef {
			refs++
		}
	})
	if refs != 0 {
		t.Fatalf("unattached service produced %d refs, want 0", refs)
	}
}

func TestEndpointIdentityNonVLANFields(t *testing.T) {
	a := models.ServiceEndpoint{ServiceID: 1, Role: "endpoint", DeviceID: 2, InterfaceID: 3, Fields: jsonRaw(t, map[string]any{"vrf": "red"})}
	b := models.ServiceEndpoint{ServiceID: 1, Role: "endpoint", DeviceID: 2, InterfaceID: 3, Fields: jsonRaw(t, map[string]any{"vrf": "blue"})}
	if EndpointIdentity(a) == EndpointIdentity(b) {
		t.Fatal("expected distinct identity for different non-vlan fields")
	}
	empty := models.ServiceEndpoint{ServiceID: 1, Role: "endpoint", DeviceID: 2, InterfaceID: 3}
	if !strings.HasSuffix(EndpointIdentity(empty), ":0") {
		t.Fatalf("empty fields disc = %s, want ...:0", EndpointIdentity(empty))
	}
}

func TestCreateServiceFromTreeZeroEndpoints(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	cust := models.Customer{Name: "TreeCo"}
	mustCreate(t, db, &cust)
	dto := &models.ServiceDTO{
		Category:    "CN",
		ServiceType: "ELINE",
		CustomerID:  cust.ID,
		Fields:      jsonRaw(t, map[string]any{"bandwidth_mbps": 100}),
	}
	node, err := CreateServiceFromTree(db, root.ID, dto)
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != models.ConfigScopeKindService || node.ServiceID == nil {
		t.Fatalf("node = %+v", node)
	}
	var svc models.Service
	if err := db.First(&svc, *node.ServiceID).Error; err != nil {
		t.Fatal(err)
	}
	if svc.ServiceID != "CN00001" {
		t.Errorf("service_id = %s, want CN00001", svc.ServiceID)
	}
	if svc.ServiceType != "ELINE" {
		t.Errorf("type = %s", svc.ServiceType)
	}
	if svc.BandwidthMbps != 100 {
		t.Errorf("bandwidth = %d, want 100", svc.BandwidthMbps)
	}
	eps, err := ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 0 {
		t.Fatalf("endpoints = %d, want 0", len(eps))
	}
	var kids int64
	if err := db.Model(&models.ConfigScope{}).
		Where("parent_id = ? AND kind = ?", node.ID, models.ConfigScopeKindServiceEndpoint).
		Count(&kids).Error; err != nil {
		t.Fatal(err)
	}
	if kids != 0 {
		t.Fatalf("endpoint children = %d, want 0", kids)
	}
}

func TestCreateServiceFromTreeInterfaceParent(t *testing.T) {
	db := newTestDB(t)
	_, folder, _, ifaceScope, _ := seedTree(t, db)
	parentID, err := CanonicalServiceParentID(db, &ifaceScope)
	if err != nil {
		t.Fatal(err)
	}
	if parentID != folder.ID {
		t.Fatalf("parent = %d, want device folder %d", parentID, folder.ID)
	}
}

func TestAttachServiceProjectsEndpointChildren(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-attach", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	svc := models.Service{ServiceID: "CN00903", ServiceType: "ELAN", Source: "lime"}
	mustCreate(t, db, &svc)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: svc.ID, Role: "endpoint", DeviceID: dev.ID, InterfaceID: ifc.ID,
		Fields: EncodeEndpointFields(50, 0, 0),
	})
	node, err := AttachService(db, folder.ID, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "CN00903" {
		t.Errorf("name = %s", node.Name)
	}
	var kids []models.ConfigScope
	if err := db.Where("parent_id = ? AND kind = ?", node.ID, models.ConfigScopeKindServiceEndpoint).
		Find(&kids).Error; err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 {
		t.Fatalf("children = %d, want 1", len(kids))
	}
	if kids[0].Payload.Role != "endpoint" {
		t.Errorf("role = %s", kids[0].Payload.Role)
	}
}

func TestAttachServiceRejectsOptical(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	svc := models.Service{ServiceID: "VL00001"}
	mustCreate(t, db, &svc)
	_, err = AttachService(db, folder.ID, svc.ID)
	wantStatus(t, err, 400)
}

func TestSeedPlacesTypedServicesUnderServicesFolder(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-seed-svc", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	eline := models.Service{ServiceID: "CN00910", ServiceType: "ELINE"}
	mustCreate(t, db, &eline)
	mustCreate(t, db, &models.ServiceEndpoint{
		ServiceID: eline.ID, Role: "a", DeviceID: dev.ID, InterfaceID: ifc.ID,
		Fields: EncodeEndpointFields(10, 0, 0),
	})
	lime := models.Service{ServiceID: "CN00911", ServiceType: "ELAN", Source: "lime"}
	mustCreate(t, db, &lime)
	vl := models.Service{ServiceID: "VL00002"}
	mustCreate(t, db, &vl)
	lf := models.Service{ServiceID: "LF00002"}
	mustCreate(t, db, &lf)
	untyped := models.Service{ServiceID: "CN00912"}
	mustCreate(t, db, &untyped)
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	elineNode, err := scopeByServiceID(db, eline.ID)
	if err != nil || elineNode == nil {
		t.Fatalf("ELINE not placed: %v", err)
	}
	if elineNode.ParentID == nil || *elineNode.ParentID != folder.ID {
		t.Errorf("ELINE parent = %v, want _services %d", elineNode.ParentID, folder.ID)
	}
	if elineNode.Name != "CN00910" {
		t.Errorf("ELINE name = %s", elineNode.Name)
	}
	var kids int64
	if err := db.Model(&models.ConfigScope{}).
		Where("parent_id = ? AND kind = ?", elineNode.ID, models.ConfigScopeKindServiceEndpoint).
		Count(&kids).Error; err != nil {
		t.Fatal(err)
	}
	if kids != 1 {
		t.Fatalf("ELINE endpoint children = %d, want 1", kids)
	}
	limeNode, err := scopeByServiceID(db, lime.ID)
	if err != nil || limeNode == nil {
		t.Fatalf("Lime ELAN not placed: %v", err)
	}
	if got, _ := scopeByServiceID(db, vl.ID); got != nil {
		t.Fatal("VL should not be placed")
	}
	if got, _ := scopeByServiceID(db, lf.ID); got != nil {
		t.Fatal("LF should not be placed")
	}
	if got, _ := scopeByServiceID(db, untyped.ID); got != nil {
		t.Fatal("untyped CN should not be placed")
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := scopeByServiceID(db, eline.ID)
	if err != nil || again == nil || again.ID != elineNode.ID {
		t.Fatal("second Seed recreated the service node")
	}
}

func TestDeleteServiceScopeDetachesInventory(t *testing.T) {
	db := newTestDB(t)
	folder, err := servicesFolder(db)
	if err != nil {
		t.Fatal(err)
	}
	svc := models.Service{ServiceID: "CN00920", ServiceType: "ELINE"}
	mustCreate(t, db, &svc)
	node, err := AttachService(db, folder.ID, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteScope(db, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetScope(db, node.ID); err == nil {
		t.Fatal("canonical still present")
	}
	var still models.Service
	if err := db.First(&still, svc.ID).Error; err != nil {
		t.Fatalf("inventory deleted: %v", err)
	}
}

func TestCreateServiceFromTreeRejectsOptical(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateServiceFromTree(db, root.ID, &models.ServiceDTO{Category: "VL", ServiceType: "ELINE"})
	wantStatus(t, err, 400)
}
