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
		&models.PlatformPack{},
		&models.ConfigTemplate{},
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

func TestSeedCreatesRootAndELINEPacks(t *testing.T) {
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
	pack, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil || pack == nil {
		t.Fatalf("eos pack: %v %#v", err, pack)
	}
	if pack.ApplyTemplate != templates.EOSEline {
		t.Error("seeded eos pack body does not match embed")
	}
	md, err := LookupPlatformPack(db, "ELINE", "sros-md")
	if err != nil || md == nil {
		t.Fatalf("sros-md pack: %v", err)
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

func TestSeedDoesNotOverwritePack(t *testing.T) {
	db := newTestDB(t)
	pack, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil {
		t.Fatal(err)
	}
	pack.ApplyTemplate = "edited"
	if err := db.Save(pack).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil {
		t.Fatal(err)
	}
	if again.ApplyTemplate != "edited" {
		t.Errorf("pack was overwritten")
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

func TestRenderELINEPackMatchesEmbed(t *testing.T) {
	db := newTestDB(t)
	pack, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil || pack == nil {
		t.Fatalf("pack: %v", err)
	}
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
	got, err := Render(db, pack.ApplyTemplate, "", intent)
	if err != nil {
		t.Fatalf("pack render: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pack cmds =\n%v\nwant\n%v", got, want)
	}
	bodyOnly, err := RenderPackApplyBody(db, pack, intent)
	if err != nil {
		t.Fatalf("apply body: %v", err)
	}
	for _, line := range bodyOnly {
		if strings.Contains(line, "no pseudowire") || strings.Contains(line, "no patch") {
			t.Fatalf("apply body still includes cleanup: %v", bodyOnly)
		}
	}
	foundIface := false
	for _, line := range bodyOnly {
		if line == "interface Ethernet1" {
			foundIface = true
		}
	}
	if !foundIface {
		t.Fatalf("apply body missing interface config: %v", bodyOnly)
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

func TestRenderDeviceIncludesGlobalTemplate(t *testing.T) {
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
	tmpl := models.ConfigTemplate{
		Name:     "banner",
		Platform: "eos",
		Body:     "banner motd {{.Name}} {{index .Vars \"color\"}}",
		ScopeID:  &root.ID,
		Enabled:  true,
	}
	mustCreate(t, db, &tmpl)

	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatalf("RenderDevice: %v", err)
	}
	found := false
	for _, s := range out.Sources {
		if s.Source == "template:banner" {
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
		t.Fatalf("baseline template missing from RenderDevice: %+v", out.Sources)
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

func TestSeedUpdatesUntouchedPack(t *testing.T) {
	db := newTestDB(t)
	pack, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil {
		t.Fatal(err)
	}
	pack.ApplyTemplate = "old-seed-body"
	pack.SeedChecksum = packChecksum("old-seed-body")
	if err := db.Save(pack).Error; err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	again, err := LookupPlatformPack(db, "ELINE", "eos")
	if err != nil {
		t.Fatal(err)
	}
	if again.ApplyTemplate != templates.EOSEline {
		t.Fatal("untouched pack was not refreshed from embed")
	}
	if again.SeedChecksum != packChecksum(templates.EOSEline) {
		t.Fatal("checksum not updated")
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

func TestRenderDevicePrefersCLIOverTemplate(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-twin", Platform: "eos"}
	mustCreate(t, db, &dev)
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "eos", Body: "banner from-template",
		ScopeID: &root.ID, Enabled: true,
	})
	cli, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigCLIFeature{
		ScopeID: cli.ID, Name: "body", AddCommands: "banner from-cli",
	})
	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawCLI, sawTmpl bool
	for _, s := range out.Sources {
		if s.Source == "cli:banner" {
			sawCLI = true
			if !reflect.DeepEqual(s.Commands, []string{"banner from-cli"}) {
				t.Errorf("cli commands = %v", s.Commands)
			}
		}
		if s.Source == "template:banner" {
			sawTmpl = true
		}
	}
	if !sawCLI {
		t.Fatal("CLI twin missing")
	}
	if sawTmpl {
		t.Fatal("template should be skipped when a CLI twin exists")
	}
}

func TestRenderDeviceTwinIgnoresTranslationAndOtherPlatform(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-twin-filter", Platform: "eos"}
	mustCreate(t, db, &dev)
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "eos", Body: "banner from-template",
		ScopeID: &root.ID, Enabled: true,
	})
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
	_, err = CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "sros",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawTmpl, sawCLI bool
	for _, s := range out.Sources {
		if s.Source == "template:banner" {
			sawTmpl = true
			if !reflect.DeepEqual(s.Commands, []string{"banner from-template"}) {
				t.Errorf("template commands = %v", s.Commands)
			}
		}
		if s.Source == "cli:banner" {
			sawCLI = true
		}
	}
	if !sawTmpl {
		t.Fatal("eos template hidden by translation or sros CLI twin")
	}
	if sawCLI {
		t.Fatal("translation/sros CLI should not render as baseline on eos")
	}

	disabled, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	})
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := UpdateScope(db, disabled.ID, &models.ConfigScopeDTO{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	out, err = RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	sawTmpl, sawCLI = false, false
	for _, s := range out.Sources {
		if s.Source == "template:banner" {
			sawTmpl = true
		}
		if s.Source == "cli:banner" {
			sawCLI = true
		}
	}
	if sawTmpl {
		t.Fatal("disabled baseline CLI should still hide the matching template")
	}
	if sawCLI {
		t.Fatal("disabled CLI should not emit commands")
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
	for _, plat := range []string{"eos", "ios-xr", "sros"} {
		pack, err := LookupPlatformPack(db, "ELINE", plat)
		if err != nil || pack == nil {
			t.Fatalf("%s pack: %v", plat, err)
		}
		want, err := Render(db, pack.ApplyTemplate, "", intent)
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
			t.Errorf("%s CLI cmds =\n%v\nwant pack/embed\n%v", plat, got, want)
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

func TestRenderGenericFallsBackToPackWhenNoCLI(t *testing.T) {
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

	cust := models.Customer{Name: "Acme AB"}
	mustCreate(t, db, &cust)
	pe1 := models.Device{Name: "pe-pack-fb", Platform: "eos", NetboxID: 901}
	pe2 := models.Device{Name: "pe-pack-fb-b", Platform: "eos", NetboxID: 902}
	mustCreate(t, db, &pe1)
	mustCreate(t, db, &pe2)
	ifa := models.Interface{DeviceID: pe1.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 911}
	ifb := models.Interface{DeviceID: pe2.ID, Name: "Ethernet2", Type: "1000base-t", NetboxID: 921}
	mustCreate(t, db, &ifa)
	mustCreate(t, db, &ifb)
	lo1 := models.Interface{DeviceID: pe1.ID, Name: "Loopback0", Type: "virtual", NetboxID: 912}
	lo2 := models.Interface{DeviceID: pe2.ID, Name: "Loopback0", Type: "virtual", NetboxID: 922}
	mustCreate(t, db, &lo1)
	mustCreate(t, db, &lo2)
	mustCreate(t, db, &models.Address{InterfaceID: lo1.ID, Address: "10.0.0.1/32"})
	mustCreate(t, db, &models.Address{InterfaceID: lo2.ID, Address: "10.0.0.2/32"})
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00571", ServiceType: "ELINE", PseudowireID: 1000571}
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
			t.Fatalf("pack fallback error: %s", src.Error)
		}
		joined += strings.Join(src.Commands, "\n") + "\n"
	}
	if !strings.Contains(joined, "neighbor 10.0.0.2") {
		t.Errorf("missing pack neighbor in %s", joined)
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

func cliChildByPlatform(t *testing.T, db *gorm.DB, parentID uint, name, platform string) models.ConfigScope {
	t.Helper()
	var rows []models.ConfigScope
	if err := db.Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, parentID, name).Find(&rows).Error; err != nil {
		t.Fatalf("list cli %s under %d: %v", name, parentID, err)
	}
	for i := range rows {
		s := &rows[i]
		if isTranslationCLI(s) {
			continue
		}
		if NormalizePlatform(s.Platform) == NormalizePlatform(platform) {
			return *s
		}
	}
	t.Fatalf("baseline cli %s/%s under %d not found in %+v", name, platform, parentID, rows)
	return models.ConfigScope{}
}

func TestSeedMigratesNullScopeTemplateToGlobalCLI(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-mig", Platform: "eos"}
	mustCreate(t, db, &dev)
	tmpl := models.ConfigTemplate{
		Name: "banner", Platform: "eos", PayloadKind: models.PayloadKindCLI,
		Body: "banner motd {{.Name}}", Enabled: true,
	}
	mustCreate(t, db, &tmpl)
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}

	cli := cliChild(t, db, root.ID, "banner")
	if cli.Platform != "eos" || cli.PayloadKind != models.PayloadKindCLI || !cli.Enabled {
		t.Errorf("cli = %+v", cli)
	}
	if cli.Payload.Context != nil {
		t.Errorf("context = %+v, want nil", cli.Payload.Context)
	}
	catalog := scopeChild(t, db, root.ID, models.ConfigCatalogName)
	var underCatalog int64
	if err := db.Model(&models.ConfigScope{}).
		Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, catalog.ID, "banner").
		Count(&underCatalog).Error; err != nil {
		t.Fatal(err)
	}
	if underCatalog != 0 {
		t.Fatal("null-scope template must not become a child of _catalog")
	}
	feats, err := ListCLIFeatures(db, cli.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(feats) != 1 || feats[0].Name != "body" || feats[0].AddCommands != tmpl.Body {
		t.Errorf("features = %+v", feats)
	}
	var still models.ConfigTemplate
	if err := db.First(&still, tmpl.ID).Error; err != nil {
		t.Fatalf("template row deleted: %v", err)
	}

	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawCLI, sawTmpl bool
	for _, s := range out.Sources {
		if s.Source == "cli:banner" {
			sawCLI = true
			if s.Error != "" {
				t.Fatalf("render error: %s", s.Error)
			}
			if !reflect.DeepEqual(s.Commands, []string{"banner motd pe-mig"}) {
				t.Errorf("commands = %v", s.Commands)
			}
		}
		if s.Source == "template:banner" {
			sawTmpl = true
		}
	}
	if !sawCLI {
		t.Fatalf("migrated CLI missing from RenderDevice: %+v", out.Sources)
	}
	if sawTmpl {
		t.Fatal("template should be skipped when a CLI twin exists")
	}
}

func TestSeedMigratesScopedTemplateToCLI(t *testing.T) {
	db := newTestDB(t)
	_, folder, deviceScope, _, _ := seedTree(t, db)
	tmpl := models.ConfigTemplate{
		Name: "qos", Platform: "eos", Body: "qos enable",
		ScopeID: &folder.ID, Enabled: true,
	}
	mustCreate(t, db, &tmpl)
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}

	cli := cliChild(t, db, folder.ID, "qos")
	if cli.Payload.Context != nil {
		t.Errorf("context = %+v, want nil", cli.Payload.Context)
	}
	feats, err := ListCLIFeatures(db, cli.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(feats) != 1 || feats[0].Name != "body" || feats[0].AddCommands != "qos enable" {
		t.Errorf("features = %+v", feats)
	}
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	var underGlobal int64
	if err := db.Model(&models.ConfigScope{}).
		Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, root.ID, "qos").
		Count(&underGlobal).Error; err != nil {
		t.Fatal(err)
	}
	if underGlobal != 0 {
		t.Fatal("scoped template must not become a child of global")
	}

	out, err := RenderDevice(db, *deviceScope.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range out.Sources {
		if s.Source == "cli:qos" {
			found = true
			if !reflect.DeepEqual(s.Commands, []string{"qos enable"}) {
				t.Errorf("commands = %v", s.Commands)
			}
		}
		if s.Source == "template:qos" {
			t.Fatal("template should be skipped when a CLI twin exists")
		}
	}
	if !found {
		t.Fatalf("scoped CLI missing from RenderDevice: %+v", out.Sources)
	}
}

func TestSeedTemplateMigrationIdempotent(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "eos", Body: "banner motd hello", Enabled: true,
	})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	cli := cliChild(t, db, root.ID, "banner")
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).
		Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, root.ID, "banner").
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cli count = %d, want 1", n)
	}
	var feats int64
	if err := db.Model(&models.ConfigCLIFeature{}).Where("scope_id = ?", cli.ID).Count(&feats).Error; err != nil {
		t.Fatal(err)
	}
	if feats != 1 {
		t.Fatalf("feature count = %d, want 1", feats)
	}
	var tmpls int64
	if err := db.Model(&models.ConfigTemplate{}).Where("name = ?", "banner").Count(&tmpls).Error; err != nil {
		t.Fatal(err)
	}
	if tmpls != 1 {
		t.Fatalf("template count = %d, want 1 (not deleted)", tmpls)
	}
}

func TestSeedMigratesSameNameTemplatesPerPlatform(t *testing.T) {
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
	srosCLI, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "banner", Kind: models.ConfigScopeKindCLI, Platform: "sros",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, &models.ConfigCLIFeature{
		ScopeID: srosCLI.ID, Name: "body", AddCommands: "banner from-existing-sros",
	})
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "eos", Body: "banner from-eos-template", Enabled: true,
	})
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "sros", Body: "banner from-sros-template", Enabled: true,
	})
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "ntp", Platform: "eos", Body: "ntp server 1.1.1.1", Enabled: true,
	})
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "ntp", Platform: "sros", Body: "ntp server 9.9.9.9", Enabled: true,
	})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}

	eosNTP := cliChildByPlatform(t, db, root.ID, "ntp", "eos")
	srosNTP := cliChildByPlatform(t, db, root.ID, "ntp", "sros")
	eosNTPFeats, err := ListCLIFeatures(db, eosNTP.ID)
	if err != nil {
		t.Fatal(err)
	}
	srosNTPFeats, err := ListCLIFeatures(db, srosNTP.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eosNTPFeats) != 1 || eosNTPFeats[0].AddCommands != "ntp server 1.1.1.1" {
		t.Errorf("eos ntp features = %+v", eosNTPFeats)
	}
	if len(srosNTPFeats) != 1 || srosNTPFeats[0].AddCommands != "ntp server 9.9.9.9" {
		t.Errorf("sros ntp features = %+v", srosNTPFeats)
	}

	eosCLI := cliChildByPlatform(t, db, root.ID, "banner", "eos")
	srosGot := cliChildByPlatform(t, db, root.ID, "banner", "sros")
	if srosGot.ID != srosCLI.ID {
		t.Fatalf("sros cli id = %d, want pre-existing %d", srosGot.ID, srosCLI.ID)
	}
	eosFeats, err := ListCLIFeatures(db, eosCLI.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eosFeats) != 1 || eosFeats[0].AddCommands != "banner from-eos-template" {
		t.Errorf("eos features = %+v", eosFeats)
	}
	srosFeats, err := ListCLIFeatures(db, srosGot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(srosFeats) != 1 || srosFeats[0].AddCommands != "banner from-existing-sros" {
		t.Errorf("sros features overwritten: %+v", srosFeats)
	}

	var kids []models.ConfigScope
	if err := db.Where("kind = ? AND parent_id = ? AND name = ?", models.ConfigScopeKindCLI, root.ID, "banner").
		Find(&kids).Error; err != nil {
		t.Fatal(err)
	}
	var nEos, nSros, nTrans int
	for i := range kids {
		if isTranslationCLI(&kids[i]) {
			nTrans++
			continue
		}
		switch NormalizePlatform(kids[i].Platform) {
		case "eos":
			nEos++
		case "sros":
			nSros++
		}
	}
	if nEos != 1 || nSros != 1 || nTrans != 1 {
		t.Fatalf("banner children eos=%d sros=%d trans=%d (from %+v), want 1 each", nEos, nSros, nTrans, kids)
	}
}

func TestSeedMigratesDisabledTemplate(t *testing.T) {
	db := newTestDB(t)
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe-off", Platform: "eos"}
	mustCreate(t, db, &dev)
	mustCreate(t, db, &models.ConfigTemplate{
		Name: "banner", Platform: "eos", Body: "banner motd dark", Enabled: false,
	})
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	cli := cliChild(t, db, root.ID, "banner")
	if cli.Enabled {
		t.Fatal("disabled template must copy Enabled=false")
	}
	out, err := RenderDevice(db, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range out.Sources {
		if s.Source == "cli:banner" || s.Source == "template:banner" {
			t.Fatalf("disabled twin still emitted %s: %+v", s.Source, out.Sources)
		}
	}
}
