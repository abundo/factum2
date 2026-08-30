package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/models"
)

func TestConfigScopeCreateAndTree(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatalf("root: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/scopes", map[string]any{
		"parent_id": root.ID, "name": "lab", "kind": "folder",
	}, nil, nil)
	if err := ctrl.ApiConfigScopeCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.ConfigScope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/scopes/tree", nil, nil, nil)
	if err := ctrl.ApiConfigScopeTree(c); err != nil {
		t.Fatalf("tree: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("tree status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var tree []cfgmgmt.ScopeTreeNode
	if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Title != models.ConfigRootName {
		t.Fatalf("tree = %+v", tree)
	}
	found := false
	for _, ch := range tree[0].Children {
		if ch.Data.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created folder missing from tree: %+v", tree[0].Children)
	}
}

func TestConfigAssignmentUpsertAndResolveRedactsSecret(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "pe1", Platform: "eos", NetboxID: 201}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	did, iid := dev.ID, iface.ID
	devScope := models.ConfigScope{ParentID: &root.ID, Name: "pe1", Kind: models.ConfigScopeKindDevice, DeviceID: &did}
	if err := db.Create(&devScope).Error; err != nil {
		t.Fatal(err)
	}
	ifcScope := models.ConfigScope{ParentID: &devScope.ID, Name: "Ethernet1", Kind: models.ConfigScopeKindInterface, DeviceID: &did, InterfaceID: &iid}
	if err := db.Create(&ifcScope).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "enable_secret", "type": "secret", "secret": true,
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("var status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var def models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/assignments", map[string]any{
		"variable_def_id": def.ID, "scope_id": ifcScope.ID, "value": "s3cret",
	}, nil, nil)
	if err := ctrl.ApiConfigAssignmentUpsert(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/resolve?interface_id="+strconv.FormatUint(uint64(iface.ID), 10), nil, nil, nil)
	if err := ctrl.ApiConfigResolve(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var rows []resolvedVarJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Name != "enable_secret" {
			continue
		}
		found = true
		if r.Value != "***" {
			t.Errorf("secret value = %#v, want redacted", r.Value)
		}
	}
	if !found {
		t.Fatal("secret var missing from resolve")
	}
}

func TestCannotDeleteBuiltinServiceType(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	var eline models.ServiceType
	if err := db.Where("name = ?", "ELINE").First(&eline).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodDelete, "/api/config/service-types/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(eline.ID), 10)})
	if err := ctrl.ApiConfigServiceTypeDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
}

func TestConfigAssignmentListRedactsSecret(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "pw", "type": "secret", "secret": true, "default_value": "defsecret",
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	var def models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/config/assignments", map[string]any{
		"variable_def_id": def.ID, "scope_id": root.ID, "value": "s3cret",
	}, nil, nil)
	if err := ctrl.ApiConfigAssignmentUpsert(c); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/assignments?scope_id="+strconv.FormatUint(uint64(root.ID), 10), nil, nil, nil)
	if err := ctrl.ApiConfigAssignmentList(c); err != nil {
		t.Fatal(err)
	}
	var rows []models.ConfigAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("assignments = %d", len(rows))
	}
	var val any
	if err := json.Unmarshal(rows[0].Value, &val); err != nil {
		t.Fatal(err)
	}
	if val != "***" {
		t.Errorf("assignment value = %#v, want redacted", val)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables", nil, nil, nil)
	if err := ctrl.ApiConfigVariableList(c); err != nil {
		t.Fatal(err)
	}
	var defs []models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &defs); err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.Name != "pw" {
			continue
		}
		if len(d.DefaultValue) > 0 && string(d.DefaultValue) != "null" {
			t.Errorf("secret default_value leaked: %s", d.DefaultValue)
		}
	}
}

func TestConfigVariableSecretDefaultNotOverwrittenByGetPut(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "enable_pw", "type": "secret", "secret": true, "default_value": "defsecret",
		"description": "orig",
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiConfigVariableGet(c); err != nil {
		t.Fatal(err)
	}
	var got models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.DefaultValue) > 0 && string(got.DefaultValue) != "null" {
		t.Fatalf("GET leaked default_value %s", got.DefaultValue)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/variables/x", map[string]any{
		"name": "enable_pw", "type": "secret", "secret": true, "description": "edited",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiConfigVariableUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("put omitted default status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/variables/x", map[string]any{
		"name": "enable_pw", "type": "secret", "secret": true, "description": "edited2",
		"default_value": "***",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiConfigVariableUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("put *** status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var stored models.ConfigVariableDef
	if err := db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	var dv any
	if err := json.Unmarshal(stored.DefaultValue, &dv); err != nil {
		t.Fatal(err)
	}
	if dv != "defsecret" {
		t.Errorf("stored default = %#v, want defsecret", dv)
	}
	if stored.Description != "edited2" {
		t.Errorf("description = %q", stored.Description)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/variables/x", map[string]any{
		"name": "enable_pw", "type": "secret", "secret": true, "description": "edited3",
		"default_value": "newsecret",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiConfigVariableUpdate(c); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stored.DefaultValue, &dv); err != nil {
		t.Fatal(err)
	}
	if dv != "newsecret" {
		t.Errorf("stored default after explicit set = %#v, want newsecret", dv)
	}
}

func TestConfigVariableCreateFromFormPayload(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "  ntp_server  ", "type": "string", "description": "",
		"required": false, "secret": false,
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "ntp_server" {
		t.Errorf("name = %q, want trimmed ntp_server", created.Name)
	}
	if created.Type != "string" {
		t.Errorf("type = %q", created.Type)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables", nil, nil, nil)
	if err := ctrl.ApiConfigVariableList(c); err != nil {
		t.Fatal(err)
	}
	var defs []models.ConfigVariableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &defs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range defs {
		if d.Name == "ntp_server" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created variable missing from list: %+v", defs)
	}
}

func TestConfigVariableListAndMapConstraints(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "ntp", "type": "list", "constraints": map[string]any{"items": "ip"},
		"default_value": []string{"10.0.0.1"},
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "bad_ntp", "type": "list", "constraints": map[string]any{"items": "nope"},
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad item type status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "bad_default", "type": "list", "constraints": map[string]any{"items": "ip"},
		"default_value": []string{"not-an-ip"},
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad default status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "communities", "type": "map",
		"constraints": map[string]any{"keys": "string", "values": "int"},
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("map status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "bad_keys", "type": "map",
		"constraints": map[string]any{"keys": "list", "values": "int"},
	}, nil, nil)
	if err := ctrl.ApiConfigVariableCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list keys status = %d, body=%s", rec.Code, rec.Body.String())
	}
}
