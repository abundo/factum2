package cfgmgmt

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
)

func f64(v float64) *float64 { return &v }

func TestValidateFieldSchemaNameAndSNPA(t *testing.T) {
	bad := models.FieldSchema{Name: "VLAN", Type: models.FieldTypeVLAN}
	if err := ValidateFieldSchema(&bad, true); err == nil {
		t.Fatal("expected invalid name")
	}
	empty := models.FieldSchema{Type: models.FieldTypeString}
	if err := ValidateFieldSchema(&empty, true); err == nil {
		t.Fatal("expected named field to require name")
	}
	item := models.FieldSchema{Type: models.FieldTypeIPv4Prefix, Resource: "peering-v4"}
	if err := ValidateFieldSchema(&item, false); err != nil {
		t.Fatalf("nameless prefix item: %v", err)
	}
	snpa := models.FieldSchema{Name: "addr", Type: models.FieldTypeSNPA}
	if err := ValidateFieldSchema(&snpa, true); err != nil {
		t.Fatal(err)
	}
	if snpa.Type != models.FieldTypeMAC {
		t.Fatalf("type = %q, want mac", snpa.Type)
	}
}

func TestValidateFieldSchemaListAndResource(t *testing.T) {
	nested := models.FieldSchema{
		Name: "oops", Type: models.FieldTypeList,
		Items: &models.FieldSchema{Type: models.FieldTypeList, Items: &models.FieldSchema{Type: models.FieldTypeString}},
	}
	if err := ValidateFieldSchema(&nested, true); err == nil {
		t.Fatal("expected nested list reject")
	}
	onList := models.FieldSchema{
		Name: "cidrs", Type: models.FieldTypeList, Resource: "pool",
		Items: &models.FieldSchema{Type: models.FieldTypeIPv4Prefix},
	}
	if err := ValidateFieldSchema(&onList, true); err == nil {
		t.Fatal("expected resource on list itself to fail")
	}
	ok := models.FieldSchema{
		Name: "cidrs", Type: models.FieldTypeList,
		Items: &models.FieldSchema{Type: models.FieldTypeIPv4Prefix, Resource: "peering-v4"},
	}
	if err := ValidateFieldSchema(&ok, true); err != nil {
		t.Fatalf("resource on prefix items: %v", err)
	}
	noItems := models.FieldSchema{Name: "cidrs", Type: models.FieldTypeList}
	if err := ValidateFieldSchema(&noItems, true); err == nil {
		t.Fatal("expected list without items to fail")
	}
	reqItem := models.FieldSchema{
		Name: "cidrs", Type: models.FieldTypeList,
		Items: &models.FieldSchema{Type: models.FieldTypeString, Required: true},
	}
	if err := ValidateFieldSchema(&reqItem, true); err == nil {
		t.Fatal("expected items.required to fail")
	}
}

func TestValidateFieldSchemaEnumAndBounds(t *testing.T) {
	emptyEnum := models.FieldSchema{Name: "mode", Type: models.FieldTypeEnum}
	if err := ValidateFieldSchema(&emptyEnum, true); err == nil {
		t.Fatal("expected empty enum reject")
	}
	dup := models.FieldSchema{
		Name: "mode", Type: models.FieldTypeEnum,
		Enum: []models.EnumChoice{{Value: "a"}, {Label: "A", Value: "a"}},
	}
	if err := ValidateFieldSchema(&dup, true); err == nil {
		t.Fatal("expected duplicate enum value reject")
	}
	unit := models.FieldSchema{Name: "name", Type: models.FieldTypeString, Unit: "ms"}
	if err := ValidateFieldSchema(&unit, true); err == nil {
		t.Fatal("expected unit on string to fail")
	}
	minmax := models.FieldSchema{Name: "n", Type: models.FieldTypeInt, Min: f64(10), Max: f64(1)}
	if err := ValidateFieldSchema(&minmax, true); err == nil {
		t.Fatal("expected min > max reject")
	}
	vlanHi := models.FieldSchema{Name: "vlan", Type: models.FieldTypeVLAN, Min: f64(5000)}
	if err := ValidateFieldSchema(&vlanHi, true); err == nil {
		t.Fatal("expected vlan min 5000 (default max 4094) to fail")
	}
	vlanMax := models.FieldSchema{Name: "vlan", Type: models.FieldTypeVLAN, Max: f64(5000)}
	if err := ValidateFieldSchema(&vlanMax, true); err != nil {
		t.Fatal(err)
	}
	if vlanMax.Max == nil || *vlanMax.Max != 4094 {
		t.Fatalf("vlan max clamped = %v, want 4094", vlanMax.Max)
	}
	vlanLo := models.FieldSchema{Name: "vlan", Type: models.FieldTypeVLAN, Min: f64(0)}
	if err := ValidateFieldSchema(&vlanLo, true); err != nil {
		t.Fatal(err)
	}
	if vlanLo.Min == nil || *vlanLo.Min != 1 {
		t.Fatalf("vlan min clamped = %v, want 1", vlanLo.Min)
	}
}

func TestValidateServiceTypeUniqueNames(t *testing.T) {
	st := &models.ServiceType{
		Name: "X",
		Schema: []models.FieldSchema{
			{Name: "vlan", Type: models.FieldTypeVLAN},
			{Name: "vlan", Type: models.FieldTypeInt},
		},
	}
	if err := ValidateServiceType(st); err == nil {
		t.Fatal("expected duplicate schema name")
	}
	st = &models.ServiceType{
		Name: "X",
		Schema: []models.FieldSchema{
			{Name: "bandwidth_mbps", Type: models.FieldTypeInt, Unit: "Mbps"},
		},
		Interfaces: models.ServiceInterfacesSpec{
			Min: 0, Max: 0,
			Fields: []models.FieldSchema{
				{Name: "vlan", Type: models.FieldTypeVLAN},
				{Name: "service_id", Type: models.FieldTypeServiceID},
			},
		},
	}
	if err := ValidateServiceType(st); err != nil {
		t.Fatal(err)
	}
}

func TestTypeCheckFieldMACAndPrefix(t *testing.T) {
	mac := models.FieldSchema{Name: "mac", Type: models.FieldTypeMAC}
	got, err := TypeCheckField(mac, "AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatal(err)
	}
	if got != "aabb.ccdd.eeff" {
		t.Fatalf("mac = %v, want aabb.ccdd.eeff", got)
	}
	got, err = TypeCheckField(mac, "aabb-ccdd-eeff")
	if err != nil {
		t.Fatal(err)
	}
	if got != "aabb.ccdd.eeff" {
		t.Fatalf("hyphen mac = %v", got)
	}
	got, err = TypeCheckField(mac, "AABBCCDDEEFF")
	if err != nil {
		t.Fatal(err)
	}
	if got != "aabb.ccdd.eeff" {
		t.Fatalf("bare mac = %v", got)
	}
	if _, err := TypeCheckField(mac, "not-a-mac"); err == nil {
		t.Fatal("expected bad mac reject")
	}

	pfx := models.FieldSchema{Name: "p", Type: models.FieldTypeIPv4Prefix}
	got, err = TypeCheckField(pfx, "10.0.0.1/24")
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.0.0.0/24" {
		t.Fatalf("prefix = %v, want masked 10.0.0.0/24", got)
	}
	if _, err := TypeCheckField(pfx, "2001:db8::/32"); err == nil {
		t.Fatal("expected ipv6 in ipv4_prefix to fail")
	}

	def := &models.ConfigVariableDef{Name: "p", Type: models.VarTypePrefix}
	vgot, err := TypeCheck(def, "10.0.0.1/24")
	if err != nil {
		t.Fatal(err)
	}
	if vgot != "10.0.0.1/24" {
		t.Fatalf("TypeCheck prefix = %v, want unmasked (config variables)", vgot)
	}
}

func TestTypeCheckFieldServiceIDAndEmpty(t *testing.T) {
	sid := models.FieldSchema{Name: "service_id", Type: models.FieldTypeServiceID, Required: true}
	if !FieldEmpty(sid, nil) || !FieldEmpty(sid, "") || !FieldEmpty(sid, 0) || !FieldEmpty(sid, 0.0) {
		t.Fatal("service_id 0/omit/null/empty must be empty")
	}
	if FieldEmpty(sid, 3) {
		t.Fatal("service_id 3 is not empty")
	}
	bl := models.FieldSchema{Name: "cw", Type: models.FieldTypeBool, Required: true}
	if FieldEmpty(bl, false) {
		t.Fatal("false is not empty")
	}
	lst := models.FieldSchema{Name: "peers", Type: models.FieldTypeList, Required: true}
	if FieldEmpty(lst, []any{}) {
		t.Fatal("[] is not empty")
	}
	n := models.FieldSchema{Name: "n", Type: models.FieldTypeInt, Required: true}
	if FieldEmpty(n, 0) {
		t.Fatal("int 0 is not empty")
	}
}

func TestValidateServiceFieldsRequired(t *testing.T) {
	st := &models.ServiceType{
		Schema: []models.FieldSchema{
			{Name: "bandwidth_mbps", Type: models.FieldTypeInt, Required: true, Min: f64(1)},
			{Name: "service_id", Type: models.FieldTypeServiceID, Required: true},
			{Name: "control_word", Type: models.FieldTypeBool},
		},
	}
	if _, err := ValidateServiceFields(st, jsonRaw(t, map[string]any{"bandwidth_mbps": 100, "service_id": 0})); err == nil {
		t.Fatal("expected required service_id 0 to fail")
	}
	if _, err := ValidateServiceFields(st, jsonRaw(t, map[string]any{"service_id": 9})); err == nil {
		t.Fatal("expected missing bandwidth to fail")
	}
	raw, err := ValidateServiceFields(st, jsonRaw(t, map[string]any{
		"bandwidth_mbps": 100, "service_id": 9, "control_word": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	m := fieldsMap(raw)
	if FieldEmpty(st.Schema[2], m["control_word"]) {
		t.Fatalf("false should persist: %#v", m["control_word"])
	}
	n, ok := asInt(m["service_id"])
	if !ok || n != 9 {
		t.Fatalf("service_id = %#v", m["service_id"])
	}
}

func TestValidateFieldSchemaDefault(t *testing.T) {
	ok := models.FieldSchema{Name: "mtu", Type: models.FieldTypeInt, Default: jsonRaw(t, 1500), Min: f64(1)}
	if err := ValidateFieldSchema(&ok, true); err != nil {
		t.Fatal(err)
	}
	badType := models.FieldSchema{Name: "mtu", Type: models.FieldTypeInt, Default: jsonRaw(t, "nope")}
	if err := ValidateFieldSchema(&badType, true); err == nil {
		t.Fatal("expected type mismatch on default")
	}
	empty := models.FieldSchema{Name: "name", Type: models.FieldTypeString, Default: jsonRaw(t, "")}
	if err := ValidateFieldSchema(&empty, true); err == nil {
		t.Fatal("expected empty default reject")
	}
	item := models.FieldSchema{Type: models.FieldTypeString, Default: jsonRaw(t, "x")}
	if err := ValidateFieldSchema(&item, false); err == nil {
		t.Fatal("expected list-item default reject")
	}
	below := models.FieldSchema{Name: "mtu", Type: models.FieldTypeInt, Default: jsonRaw(t, 0), Min: f64(1)}
	if err := ValidateFieldSchema(&below, true); err == nil {
		t.Fatal("expected default below min reject")
	}
	enum := models.FieldSchema{
		Name: "mode", Type: models.FieldTypeEnum,
		Enum:    []models.EnumChoice{{Value: "a"}, {Value: "b"}},
		Default: jsonRaw(t, "a"),
	}
	if err := ValidateFieldSchema(&enum, true); err != nil {
		t.Fatal(err)
	}
}

func TestValidateServiceFieldsAppliesDefault(t *testing.T) {
	st := &models.ServiceType{
		Schema: []models.FieldSchema{
			{Name: "bandwidth_mbps", Type: models.FieldTypeInt, Required: true, Default: jsonRaw(t, 100), Min: f64(1)},
			{Name: "control_word", Type: models.FieldTypeBool, Default: jsonRaw(t, false)},
		},
	}
	if err := ValidateServiceType(st); err != nil {
		t.Fatal(err)
	}
	raw, err := ValidateServiceFields(st, jsonRaw(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	m := fieldsMap(raw)
	n, ok := asInt(m["bandwidth_mbps"])
	if !ok || n != 100 {
		t.Fatalf("bandwidth default = %#v", m["bandwidth_mbps"])
	}
	if m["control_word"] != false {
		t.Fatalf("bool false default = %#v", m["control_word"])
	}
	raw, err = ValidateServiceFields(st, jsonRaw(t, map[string]any{"bandwidth_mbps": 50}))
	if err != nil {
		t.Fatal(err)
	}
	m = fieldsMap(raw)
	n, ok = asInt(m["bandwidth_mbps"])
	if !ok || n != 50 {
		t.Fatalf("explicit bandwidth overwritten: %#v", m["bandwidth_mbps"])
	}
}

func TestTypeCheckFieldListItems(t *testing.T) {
	f := models.FieldSchema{
		Name: "cidrs", Type: models.FieldTypeList, Min: f64(1), Max: f64(2),
		Items: &models.FieldSchema{Type: models.FieldTypePrefix},
	}
	got, err := TypeCheckField(f, []any{"192.168.1.1/24"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := got.([]any)
	if len(list) != 1 || list[0] != "192.168.1.0/24" {
		t.Fatalf("got %#v", got)
	}
	if _, err := TypeCheckField(f, []any{}); err == nil {
		t.Fatal("expected min length")
	}
	if _, err := TypeCheckField(f, []any{nil}); err == nil {
		t.Fatal("expected null list item reject")
	}
	sidList := models.FieldSchema{
		Name: "ids", Type: models.FieldTypeList,
		Items: &models.FieldSchema{Type: models.FieldTypeServiceID},
	}
	if _, err := TypeCheckField(sidList, []any{0}); err == nil {
		t.Fatal("expected service_id 0 list item reject")
	}
}

func TestCreateServiceRecordRequiresSchemaFields(t *testing.T) {
	db := newTestDB(t)
	mustELINEType(t, db)
	if _, err := CreateServiceRecord(db, &models.ServiceDTO{Category: "CN", ServiceType: "ELINE"}); err == nil {
		t.Fatal("expected missing required bandwidth_mbps")
	}
	svc, err := CreateServiceRecord(db, &models.ServiceDTO{
		Category: "CN", ServiceType: "ELINE",
		Fields: jsonRaw(t, map[string]any{"bandwidth_mbps": 50}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.BandwidthMbps != 50 {
		t.Fatalf("bandwidth = %d", svc.BandwidthMbps)
	}
}

func TestValidateEndpointsCanonicalMAC(t *testing.T) {
	db := newTestDB(t)
	st := models.ServiceType{
		Name: "MACY",
		Interfaces: models.ServiceInterfacesSpec{
			Min: 1, Max: 1,
			Fields: []models.FieldSchema{{Name: "mac", Type: models.FieldTypeMAC, Required: true}},
		},
	}
	mustCreate(t, db, &st)
	dev := models.Device{Name: "pe-mac", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	eps := []models.ServiceEndpoint{{
		Role: models.EndpointRoleInterface, DeviceID: dev.ID, InterfaceID: ifc.ID,
		Fields: jsonRaw(t, map[string]any{"mac": "00:11:22:33:44:55"}),
	}}
	if err := ValidateEndpoints(db, &st, eps); err != nil {
		t.Fatal(err)
	}
	if fieldsMap(eps[0].Fields)["mac"] != "0011.2233.4455" {
		t.Fatalf("canonical mac = %#v", fieldsMap(eps[0].Fields)["mac"])
	}
	eps[0].Fields = jsonRaw(t, map[string]any{"mac": ""})
	if err := ValidateEndpoints(db, &st, eps); err == nil {
		t.Fatal("expected required empty mac to fail")
	}
}

func TestValidateEndpointsServiceIDZeroRequired(t *testing.T) {
	db := newTestDB(t)
	st := models.ServiceType{
		Name: "SID",
		Interfaces: models.ServiceInterfacesSpec{
			Min: 1, Max: 1,
			Fields: []models.FieldSchema{{Name: "service_id", Type: models.FieldTypeServiceID, Required: true}},
		},
	}
	mustCreate(t, db, &st)
	dev := models.Device{Name: "pe-sid", Platform: "eos"}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	mustCreate(t, db, &ifc)
	eps := []models.ServiceEndpoint{{
		DeviceID: dev.ID, InterfaceID: ifc.ID,
		Fields: jsonRaw(t, map[string]any{"service_id": 0}),
	}}
	err := ValidateEndpoints(db, &st, eps)
	if err == nil {
		t.Fatal("expected required service_id 0 to fail")
	}
	if !strings.Contains(err.Error(), "service_id") {
		t.Fatalf("err = %v", err)
	}
}
