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

func TestApiServiceCreateAcceptsELINEAndUserType(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodPost, "/api/service", map[string]any{
		"category": "CN", "service_type": "ELINE",
	}, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("ELINE create status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/config/service-types", map[string]any{
		"name": "INTERNET", "description": "DIA",
	}, nil, nil)
	if err := ctrl.ApiConfigServiceTypeCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("type status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/service", map[string]any{
		"category": "CN", "service_type": "INTERNET",
	}, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("user type create status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/service", map[string]any{
		"category": "CN", "service_type": "NOT-A-TYPE",
	}, nil, nil)
	if err := ctrl.ApiServiceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiServiceElineUpdateStandaloneWithoutNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00001", ServiceType: "ELINE"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	devA := models.Device{Name: "pe-a", Platform: "eos", NetboxID: 101}
	devB := models.Device{Name: "pe-b", Platform: "eos", NetboxID: 102}
	if err := db.Create(&devA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&devB).Error; err != nil {
		t.Fatal(err)
	}
	ifa := models.Interface{DeviceID: devA.ID, Name: "Ethernet1", Type: "1000base-t"}
	ifb := models.Interface{DeviceID: devB.ID, Name: "Ethernet1", Type: "1000base-t"}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"endpoint_a_device_id":    devA.ID,
		"endpoint_a_interface_id": ifa.ID,
		"endpoint_a_vlan":         100,
		"endpoint_b_device_id":    devB.ID,
		"endpoint_b_interface_id": ifb.ID,
		"endpoint_b_vlan":         200,
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/eline", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceElineUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updated models.Service
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.PseudowireID == 0 {
		t.Fatal("expected locally derived pseudowire_id")
	}
	if updated.L2VPNNetboxID != 0 {
		t.Errorf("standalone save wrote netbox id %d", updated.L2VPNNetboxID)
	}
	eps, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(eps))
	}
	byRole := map[string]models.ServiceEndpoint{}
	for _, ep := range eps {
		byRole[ep.Role] = ep
	}
	if byRole["a"].InterfaceID != ifa.ID || cfgmgmt.VLANFromFields(byRole["a"].Fields) != 100 {
		t.Errorf("endpoint a = %+v", byRole["a"])
	}
	if byRole["b"].InterfaceID != ifb.ID || cfgmgmt.VLANFromFields(byRole["b"].Fields) != 200 {
		t.Errorf("endpoint b = %+v", byRole["b"])
	}
}

func TestApiServiceEndpointsPutELINE(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00002", ServiceType: "ELINE"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	devA := models.Device{Name: "pe-a2", Platform: "eos", NetboxID: 201}
	devB := models.Device{Name: "pe-b2", Platform: "eos", NetboxID: 202}
	if err := db.Create(&devA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&devB).Error; err != nil {
		t.Fatal(err)
	}
	ifa := models.Interface{DeviceID: devA.ID, Name: "Ethernet1", Type: "1000base-t"}
	ifb := models.Interface{DeviceID: devB.ID, Name: "Ethernet1", Type: "1000base-t"}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"endpoints": []map[string]any{
			{"role": "a", "device_id": devA.ID, "interface_id": ifa.ID, "fields": map[string]any{"vlan": 10}},
			{"role": "b", "device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 20}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	eps, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(eps))
	}
	var stored models.Service
	if err := db.First(&stored, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PseudowireID == 0 {
		t.Fatal("expected pseudowire_id")
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

// TestConfigVariableGet_RejectsSQLInjectionInID covers the RequireRead
// ApiConfigVariableGet route: a viewer-role caller used to be able to
// exfiltrate the DB via a 200-versus-404 boolean oracle on :id.
func TestConfigVariableGet_RejectsSQLInjectionInID(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	createTestUser(t, db, "admin", "secret", true)

	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "vlan", "type": "int",
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

	truePayload, falsePayload := booleanOraclePayloads()
	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables/x", nil, []string{"id"}, []string{truePayload})
	if err := ctrl.ApiConfigVariableGet(c); err != nil {
		t.Fatal(err)
	}
	trueCode := rec.Code
	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables/x", nil, []string{"id"}, []string{falsePayload})
	if err := ctrl.ApiConfigVariableGet(c); err != nil {
		t.Fatal(err)
	}
	if trueCode == http.StatusOK && rec.Code == http.StatusNotFound {
		t.Fatalf("boolean SQL-injection oracle is open: true=%d false=%d", trueCode, rec.Code)
	}
	if trueCode == http.StatusOK {
		t.Fatalf("true injection payload returned 200, want reject")
	}
	if trueCode != rec.Code {
		t.Fatalf("true payload status %d != false payload status %d (boolean oracle)", trueCode, rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/variables/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiConfigVariableGet(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("numeric id status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestConfigAssignmentListWinningRowsAfterCopy(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	folder := models.ConfigScope{ParentID: &root.ID, Name: "lab", Kind: models.ConfigScopeKindFolder}
	if err := db.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	def := models.ConfigVariableDef{Name: "mtu", Type: models.VarTypeInt}
	if err := db.Create(&def).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ConfigAssignment{VariableDefID: def.ID, ScopeID: folder.ID, Value: json.RawMessage(`1500`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.Seed(db); err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/config/assignments?scope_id="+strconv.FormatUint(uint64(folder.ID), 10), nil, nil, nil)
	if err := ctrl.ApiConfigAssignmentList(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var rows []models.ConfigAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("GET folder rows = %d, want 1", len(rows))
	}
	if rows[0].ScopeID == folder.ID {
		t.Fatal("winner scope_id is the folder original, want parameters child")
	}
	var child models.ConfigScope
	if err := db.Where("parent_id = ? AND kind = ? AND name = ?", folder.ID, models.ConfigScopeKindParameter, models.ConfigParametersChildName).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	if rows[0].ScopeID != child.ID {
		t.Fatalf("winner scope_id = %d, want child %d", rows[0].ScopeID, child.ID)
	}
}

func TestConfigAssignmentSecretPutUnchanged(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/config/variables", map[string]any{
		"name": "enable_secret", "type": "secret", "secret": true,
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
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/config/assignments?scope_id="+strconv.FormatUint(uint64(root.ID), 10), nil, nil, nil)
	if err := ctrl.ApiConfigAssignmentList(c); err != nil {
		t.Fatal(err)
	}
	var listed []models.ConfigAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed = %d", len(listed))
	}
	var listedVal any
	if err := json.Unmarshal(listed[0].Value, &listedVal); err != nil {
		t.Fatal(err)
	}
	if listedVal != "***" {
		t.Errorf("GET value = %#v, want redacted", listedVal)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/config/assignments", map[string]any{
		"variable_def_id": def.ID, "scope_id": root.ID, "value": "***",
	}, nil, nil)
	if err := ctrl.ApiConfigAssignmentUpsert(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("put *** status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var putRow models.ConfigAssignment
	if err := json.Unmarshal(rec.Body.Bytes(), &putRow); err != nil {
		t.Fatal(err)
	}
	var putVal any
	if err := json.Unmarshal(putRow.Value, &putVal); err != nil {
		t.Fatal(err)
	}
	if putVal != "***" {
		t.Errorf("PUT *** body = %#v, want redacted", putVal)
	}

	var stored []models.ConfigAssignment
	if err := db.Where("variable_def_id = ?", def.ID).Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 {
		t.Fatal("no stored assignments")
	}
	for _, a := range stored {
		var v any
		if err := json.Unmarshal(a.Value, &v); err != nil {
			t.Fatal(err)
		}
		if v != "s3cret" {
			t.Errorf("stored value = %#v, want s3cret (*** must not persist)", v)
		}
	}
}
