package cfgmgmt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB uses t.Name() as the sqlite name, so a second newTestDB in the
// same test aliases the first database.
func freshTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
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
		&models.ServiceConnectionType{},
		&models.ConfigMacro{},
		&models.ServiceEndpoint{},
	); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestConfigBundleELINERoundTrip(t *testing.T) {
	db := newTestDB(t)
	st := mustELINEType(t, db)
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("factum")...)
	mustCreate(t, db, &models.ServiceConnectionType{
		ServiceTypeID: st.ID, Name: "vlan", ContentType: "image/png", Image: png,
	})
	mustCreate(t, db, &models.ServiceType{Name: "ELAN", Description: "multipoint"})

	parent, err := CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI,
		Payload: models.ConfigScopePayload{Context: &models.CLIContext{Enter: "configure", Exit: "exit"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCLIFeature(db, cli.ID, &models.ConfigCLIFeature{
		Name: "apply", SortOrder: 1,
		AddCommands:    "{{ include \"qos\" }}\nmtu {{.Vars.mtu}}",
		UpdateCommands: "{{include(\"inner\")}}",
		RemoveCommands: "no mtu",
	}); err != nil {
		t.Fatal(err)
	}
	elanParent, err := CatalogCLITypeFolder(db, "ELAN")
	if err != nil {
		t.Fatal(err)
	}
	var elan models.ServiceType
	if err := db.Where("name = ?", "ELAN").First(&elan).Error; err != nil {
		t.Fatal(err)
	}
	elanID := elan.ID
	if _, err := CreateScope(db, &models.ConfigScope{
		ParentID: &elanParent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &elanID, Platform: "eos", PayloadKind: models.PayloadKindCLI,
	}); err != nil {
		t.Fatal(err)
	}
	root, err := RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "baseline", Kind: models.ConfigScopeKindCLI, Platform: "eos",
	}); err != nil {
		t.Fatal(err)
	}

	mustCreate(t, db, &models.ConfigMacro{Name: "qos", Body: "{{ include \"inner\" }}"})
	mustCreate(t, db, &models.ConfigMacro{Name: "inner", Body: "remark {{.Vars.remark}}"})
	mustCreate(t, db, &models.ConfigMacro{Name: "other", Body: "leave me"})

	mtu := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt, Description: "MTU"}
	asn := models.ConfigVariableDef{Name: "asn", Type: models.VarTypeInt}
	remark := models.ConfigVariableDef{Name: "remark", Type: models.VarTypeString}
	ntp := models.ConfigVariableDef{Name: "ntp_server", Type: models.VarTypeString}
	token := models.ConfigVariableDef{Name: "token", Type: models.VarTypeSecret, Secret: true}
	mustCreate(t, db, &mtu)
	mustCreate(t, db, &asn)
	mustCreate(t, db, &remark)
	mustCreate(t, db, &ntp)
	mustCreate(t, db, &token)

	lab, err := CreateScope(db, &models.ConfigScope{ParentID: &root.ID, Name: "lab", Kind: models.ConfigScopeKindFolder})
	if err != nil {
		t.Fatal(err)
	}
	param, err := CreateScope(db, &models.ConfigScope{
		ParentID: &lab.ID, Name: "eline-defaults", Kind: models.ConfigScopeKindParameter, SortOrder: 3,
		Payload: models.ConfigScopePayload{Description: "ELINE defaults"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, mtu.ID, param.ID, []byte("9100")); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, asn.ID, param.ID, []byte("65000")); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, token.ID, param.ID, []byte(`"s3cret"`)); err != nil {
		t.Fatal(err)
	}
	ntpParam, err := CreateScope(db, &models.ConfigScope{
		ParentID: &root.ID, Name: "ntp", Kind: models.ConfigScopeKindParameter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAssignment(db, ntp.ID, ntpParam.ID, []byte(`"10.0.0.1"`)); err != nil {
		t.Fatal(err)
	}

	bundle, err := ExportBundle(db, BundleExportRequest{
		ServiceDefinitions: []string{"ELINE"},
		Related:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.ServiceDefinitions) != 1 || bundle.ServiceDefinitions[0].Name != "ELINE" {
		t.Fatalf("definitions = %+v", bundle.ServiceDefinitions)
	}
	if len(bundle.ServiceDefinitions[0].ConnectionTypes) != 1 || bundle.ServiceDefinitions[0].ConnectionTypes[0].ImageBase64 == "" {
		t.Fatalf("connection types = %+v", bundle.ServiceDefinitions[0].ConnectionTypes)
	}
	if len(bundle.CLIObjects) != 1 || bundle.CLIObjects[0].Path != "global/_catalog/cli/ELINE/eos" {
		t.Fatalf("cli = %+v", bundle.CLIObjects)
	}
	if bundle.CLIObjects[0].ServiceType != "ELINE" || bundle.CLIObjects[0].Features[0].AddCommands == "" {
		t.Fatalf("cli body = %+v", bundle.CLIObjects[0])
	}
	gotMacros := map[string]bool{}
	for _, m := range bundle.Macros {
		gotMacros[m.Name] = true
	}
	if !gotMacros["qos"] || !gotMacros["inner"] || gotMacros["other"] {
		t.Fatalf("macros = %+v", bundle.Macros)
	}
	if len(bundle.ParameterObjects) != 1 || bundle.ParameterObjects[0].Path != "global/lab/eline-defaults" {
		t.Fatalf("parameters = %+v", bundle.ParameterObjects)
	}
	gotVars := map[string]bool{}
	for _, v := range bundle.Variables {
		gotVars[v.Name] = true
	}
	for _, name := range []string{"mtu", "asn", "remark", "token"} {
		if !gotVars[name] {
			t.Fatalf("variables = %+v, missing %s", bundle.Variables, name)
		}
	}
	if gotVars["ntp_server"] {
		t.Fatalf("ntp_server should stay out of the bundle: %+v", bundle.Variables)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("secret value was exported without include_secrets")
	}

	withSecrets, err := ExportBundle(db, BundleExportRequest{
		ServiceDefinitions: []string{"ELINE"}, Related: true, IncludeSecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secRaw, err := json.Marshal(withSecrets)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(secRaw), "s3cret") {
		t.Fatal("include_secrets did not export the secret")
	}

	defsOnly, err := ExportBundle(db, BundleExportRequest{ServiceDefinitions: []string{"ELINE"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(defsOnly.CLIObjects) != 0 || len(defsOnly.Macros) != 0 || len(defsOnly.ParameterObjects) != 0 {
		t.Fatalf("related off still pulled objects: %+v", defsOnly)
	}

	var decoded ConfigBundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	dst := freshTestDB(t, t.Name()+"-dst")
	res, err := ImportBundle(dst, decoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) == 0 {
		t.Fatalf("created = %#v", res)
	}
	warn := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "token") {
			warn = true
		}
	}
	if !warn {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
	got, err := LookupCLIObject(dst, "ELINE", "eos")
	if err != nil || got == nil {
		t.Fatalf("lookup cli: %v %#v", err, got)
	}
	feats, err := ListCLIFeatures(dst, got.ID)
	if err != nil || len(feats) != 1 || !strings.Contains(feats[0].AddCommands, `include "qos"`) {
		t.Fatalf("features = %+v %v", feats, err)
	}
	var qos models.ConfigMacro
	if err := dst.Where("name = ?", "qos").First(&qos).Error; err != nil || qos.Body != `{{ include "inner" }}` {
		t.Fatalf("qos = %+v %v", qos, err)
	}
	var paramDst models.ConfigScope
	if err := dst.Where("name = ? AND kind = ?", "eline-defaults", models.ConfigScopeKindParameter).First(&paramDst).Error; err != nil {
		t.Fatal(err)
	}
	if paramDst.SortOrder != 3 {
		t.Fatalf("sort = %d", paramDst.SortOrder)
	}
	var mtuDst models.ConfigVariableDef
	if err := dst.Where("name = ?", "mtu").First(&mtuDst).Error; err != nil {
		t.Fatal(err)
	}
	var assign models.ConfigAssignment
	if err := dst.Where("variable_def_id = ? AND scope_id = ?", mtuDst.ID, paramDst.ID).First(&assign).Error; err != nil {
		t.Fatal(err)
	}
	var mtuVal any
	if err := json.Unmarshal(assign.Value, &mtuVal); err != nil {
		t.Fatal(err)
	}
	if mtuVal != float64(9100) {
		t.Fatalf("mtu = %#v (%s)", mtuVal, assign.Value)
	}
	var ct models.ServiceConnectionType
	if err := dst.Where("name = ?", "vlan").First(&ct).Error; err != nil {
		t.Fatal(err)
	}
	if string(ct.Image) != string(png) || ct.ContentType != "image/png" {
		t.Fatalf("image = %q %q", ct.ContentType, ct.Image)
	}
	var labDst models.ConfigScope
	if err := dst.Where("name = ? AND kind = ?", "lab", models.ConfigScopeKindFolder).First(&labDst).Error; err != nil {
		t.Fatal(err)
	}

	// Reimport updates the CLI and leaves macros that were not in the file.
	decoded.CLIObjects[0].Features[0].AddCommands = "mtu {{.Vars.mtu}}"
	decoded.CLIObjects[0].Features = append(decoded.CLIObjects[0].Features, BundleCLIFeature{Name: "extra", AddCommands: "x"})
	// Drop extra on the next pass; first add it, then import a copy without it.
	res, err = ImportBundle(db, decoded)
	if err != nil {
		t.Fatal(err)
	}
	feats, err = ListCLIFeatures(db, cli.ID)
	if err != nil || len(feats) != 2 {
		t.Fatalf("features after update = %+v %v", feats, err)
	}
	trimmed := decoded
	trimmed.CLIObjects[0].Features = trimmed.CLIObjects[0].Features[:1]
	if _, err := ImportBundle(db, trimmed); err != nil {
		t.Fatal(err)
	}
	feats, err = ListCLIFeatures(db, cli.ID)
	if err != nil || len(feats) != 1 || feats[0].AddCommands != "mtu {{.Vars.mtu}}" {
		t.Fatalf("features after trim = %+v %v", feats, err)
	}
	var other models.ConfigMacro
	if err := db.Where("name = ?", "other").First(&other).Error; err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("kind = ? AND service_type_id = ?", models.ConfigScopeKindCLI, elan.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ELAN cli count = %d", n)
	}
}

func TestConfigBundleRollsBack(t *testing.T) {
	db := newTestDB(t)
	b := ConfigBundle{
		Format: BundleFormat, Version: BundleVersion,
		ServiceDefinitions: []BundleServiceDefinition{{
			Name: "ELINE", Schema: []models.FieldSchema{},
			Interfaces:      models.ServiceInterfacesSpec{Min: 2, Max: 2},
			ConnectionTypes: []BundleConnectionType{},
		}},
		CLIObjects: []BundleCLIObject{{
			Path: "global/_catalog/cli/ELINE/eos", Name: "eos", Platform: "eos",
			ServiceType: "ELINE",
			Features:    []BundleCLIFeature{{Name: ""}},
		}},
	}
	if _, err := ImportBundle(db, b); err == nil {
		t.Fatal("expected error")
	}
	var n int64
	if err := db.Model(&models.ServiceType{}).Where("name = ?", "ELINE").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("service definition was committed despite a failed import")
	}
}

func TestConfigBundleRejectsUnattachedDevice(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.Device{Name: "pe1", Platform: "eos", NetboxID: 77}).Error; err != nil {
		t.Fatal(err)
	}
	b := ConfigBundle{
		Format: BundleFormat, Version: BundleVersion,
		Variables: []BundleVariable{{Name: "mtu", Type: models.VarTypeInt}},
		ParameterObjects: []BundleParameterObject{{
			Path: "global/pe1/mtu", Name: "mtu",
			Assignments: []BundleAssignment{{Variable: "mtu", Value: []byte("1500")}},
		}},
	}
	_, err := ImportBundle(db, b)
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("err = %v", err)
	}
	var n int64
	if err := db.Model(&models.ConfigVariableDef{}).Where("name = ?", "mtu").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("variable was committed despite a failed import")
	}
}

func TestConfigBundleRejectsShape(t *testing.T) {
	db := newTestDB(t)
	if _, err := ExportBundle(db, BundleExportRequest{}); err == nil {
		t.Fatal("empty selection")
	}
	if _, err := ImportBundle(db, ConfigBundle{Format: "nope", Version: 1}); err == nil {
		t.Fatal("bad format")
	}
	if _, err := ImportBundle(db, ConfigBundle{Format: BundleFormat, Version: 2, Macros: []BundleMacro{{Name: "a", Body: "b"}}}); err == nil {
		t.Fatal("bad version")
	}
}
