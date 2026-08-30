package cfgmgmt

import (
	"encoding/json"
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

	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: jsonRaw(t, 1500)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: deviceScope.ID, Value: jsonRaw(t, 9000)})
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: ifaceScope.ID, Value: jsonRaw(t, 9100)})

	v, src, err := Resolve(db, iface.ID, "mtu")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src == nil || src.ID != ifaceScope.ID {
		t.Fatalf("source = %+v, want interface scope", src)
	}
	if n, _ := asInt(v); n != 9100 {
		t.Errorf("value = %v, want 9100", v)
	}

	if err := db.Delete(&models.ConfigAssignment{}, "scope_id = ?", ifaceScope.ID).Error; err != nil {
		t.Fatal(err)
	}
	v, src, err = Resolve(db, iface.ID, "mtu")
	if err != nil {
		t.Fatalf("resolve device: %v", err)
	}
	if src == nil || src.ID != deviceScope.ID {
		t.Fatalf("source = %+v, want device", src)
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
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: deviceScope.ID, Value: jsonRaw(t, 65000)})

	v, src, err := Resolve(db, ifc.ID, "asn")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src == nil || src.ID != deviceScope.ID {
		t.Fatalf("source = %+v, want device scope", src)
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
	_, err = UpdateScope(db, a.ID, &models.ConfigScope{ParentID: &b.ID})
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
	intent := ELINERenderData{
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
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: def.ID, ScopeID: root.ID, Value: jsonRaw(t, "blue")})
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
	if _, err := UpdateScope(db, n2.ID, &models.ConfigScope{DeviceID: &id, Kind: models.ConfigScopeKindDevice}); err == nil {
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
	mustCreate(t, db, &models.ConfigAssignment{VariableDefID: opt.ID, ScopeID: ifaceScope.ID, Value: []byte("null")})
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
	ok := []models.ServiceEndpoint{{Role: "endpoint", DeviceID: dev.ID, InterfaceID: ifc.ID}}
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
	if _, err := UpdateScope(db, root.ID, &models.ConfigScope{Name: "not-global"}); err == nil {
		t.Fatal("expected rename reject")
	}
	if _, err := UpdateScope(db, root.ID, &models.ConfigScope{Kind: models.ConfigScopeKindSite}); err == nil {
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
