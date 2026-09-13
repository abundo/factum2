package cfgmgmt

import (
	"strings"
	"testing"

	"github.com/abundo/factum2/models"
)

func TestFillEndpointDefaultsSameNameOnly(t *testing.T) {
	st := &models.ServiceType{
		Schema: []models.FieldSchema{
			{Name: "bandwidth", Type: models.FieldTypeInt},
			{Name: "default_bandwidth", Type: models.FieldTypeInt},
			{Name: "service_id", Type: models.FieldTypeServiceID},
			{Name: "enabled", Type: models.FieldTypeBool},
		},
		Interfaces: models.ServiceInterfacesSpec{
			Fields: []models.FieldSchema{
				{Name: "bandwidth", Type: models.FieldTypeInt},
				{Name: "service_id", Type: models.FieldTypeServiceID},
				{Name: "enabled", Type: models.FieldTypeBool},
			},
		},
	}
	svc := map[string]any{
		"bandwidth":         100,
		"default_bandwidth": 999,
		"service_id":        7,
		"enabled":           false,
	}
	got := FillEndpointDefaults(st, svc, map[string]any{})
	if got["bandwidth"] != 100 {
		t.Errorf("bandwidth = %#v, want 100 from same-name service field", got["bandwidth"])
	}
	if _, ok := got["default_bandwidth"]; ok {
		t.Errorf("must not fill from default_bandwidth: %#v", got)
	}
	if n, ok := asInt(got["service_id"]); !ok || n != 7 {
		t.Errorf("service_id = %#v", got["service_id"])
	}
	if got["enabled"] != false {
		t.Errorf("false is not empty and should fill: %#v", got["enabled"])
	}

	got = FillEndpointDefaults(st, svc, map[string]any{"bandwidth": 25, "service_id": 0})
	if got["bandwidth"] != 25 {
		t.Errorf("explicit bandwidth overwritten: %#v", got["bandwidth"])
	}
	if n, ok := asInt(got["service_id"]); !ok || n != 7 {
		t.Errorf("service_id 0 should fill: %#v", got["service_id"])
	}

	emptySvc := map[string]any{"service_id": 0, "bandwidth": ""}
	got = FillEndpointDefaults(st, emptySvc, map[string]any{})
	if _, ok := got["bandwidth"]; ok {
		t.Errorf("empty service bandwidth should not fill: %#v", got)
	}
	if v, ok := got["service_id"]; ok && !FieldEmpty(st.Interfaces.Fields[1], v) {
		t.Errorf("service_id 0 is empty and must not fill: %#v", got["service_id"])
	}
}

func TestFillEndpointDefaultsDoesNotPersist(t *testing.T) {
	db := newTestDB(t)
	st := mustELINEType(t, db)
	st.Interfaces.Fields = append(st.Interfaces.Fields, models.FieldSchema{Name: "bandwidth_mbps", Type: models.FieldTypeInt})
	if err := db.Save(st).Error; err != nil {
		t.Fatal(err)
	}
	cust := models.Customer{Name: "Acme"}
	mustCreate(t, db, &cust)
	pe := models.Device{Name: "pe-fill", Platform: "eos", NetboxID: 804}
	mustCreate(t, db, &pe)
	ifc := models.Interface{DeviceID: pe.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 841}
	mustCreate(t, db, &ifc)
	svc := models.Service{
		CustomerID:  cust.ID,
		ServiceID:   "CN00999",
		ServiceType: "ELINE",
		Fields:      jsonRaw(t, map[string]any{"bandwidth_mbps": 50}),
	}
	mustCreate(t, db, &svc)
	ep := models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: pe.ID, InterfaceID: ifc.ID,
		Fields: jsonRaw(t, map[string]any{"vlan": 10}),
	}
	mustCreate(t, db, &ep)

	data, err := GenericData(db, &svc, &ep, &pe, &ifc)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := asInt(data.Current.Fields["bandwidth_mbps"]); !ok || n != 50 {
		t.Errorf("filled bandwidth = %#v", data.Current.Fields["bandwidth_mbps"])
	}
	var stored models.ServiceEndpoint
	if err := db.First(&stored, ep.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok := fieldsMap(stored.Fields)["bandwidth_mbps"]; ok {
		t.Fatalf("stored endpoint JSON must stay empty of filled keys: %s", stored.Fields)
	}
}

func TestGenericDataOthersNeighborIP(t *testing.T) {
	db := newTestDB(t)
	mustELINEType(t, db)
	cust := models.Customer{Name: "Acme"}
	mustCreate(t, db, &cust)
	pe1 := models.Device{Name: "pe1", Platform: "eos", NetboxID: 801}
	pe2 := models.Device{Name: "pe2", Platform: "eos", NetboxID: 802}
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
	ct := models.ServiceConnectionType{ServiceTypeID: 0, Name: "nni-vlan"}
	st, err := LookupServiceType(db, "ELINE")
	if err != nil {
		t.Fatal(err)
	}
	ct.ServiceTypeID = st.ID
	mustCreate(t, db, &ct)
	svc := models.Service{
		CustomerID: cust.ID, ServiceID: "CN00100", ServiceType: "ELINE",
		Comment: "operator note", ConnectionTypeID: &ct.ID,
		Fields: jsonRaw(t, map[string]any{"bandwidth_mbps": 100, "mtu": 9100}),
	}
	mustCreate(t, db, &svc)
	epa := models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: pe1.ID, InterfaceID: ifa.ID, Fields: EncodeEndpointFields(100, 0, 0),
	}
	epb := models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: pe2.ID, InterfaceID: ifb.ID, Fields: EncodeEndpointFields(200, 0, 0),
	}
	mustCreate(t, db, &epa)
	mustCreate(t, db, &epb)

	data, err := GenericData(db, &svc, &epa, &pe1, &ifa)
	if err != nil {
		t.Fatal(err)
	}
	if data.Description != "operator note" {
		t.Errorf("Description = %q, want Service.Comment", data.Description)
	}
	if data.ConnectionType != "nni-vlan" {
		t.Errorf("ConnectionType = %q", data.ConnectionType)
	}
	if data.LocalIface != "Ethernet1" {
		t.Errorf("LocalIface = %q", data.LocalIface)
	}
	if len(data.Interfaces) != 2 || len(data.Others) != 1 {
		t.Fatalf("Interfaces=%d Others=%d", len(data.Interfaces), len(data.Others))
	}
	if data.Others[0].NeighborIP != "10.0.0.2" {
		t.Errorf("remote NeighborIP = %q", data.Others[0].NeighborIP)
	}
	if data.Current.NeighborIP != "" {
		t.Errorf("Current.NeighborIP should be empty, got %q", data.Current.NeighborIP)
	}
	vlan, _ := asInt(data.Current.Fields["vlan"])
	if vlan != 100 {
		t.Errorf("current vlan = %#v", data.Current.Fields["vlan"])
	}

	same := models.Device{Name: "pe-same", Platform: "eos", NetboxID: 803}
	mustCreate(t, db, &same)
	ifc1 := models.Interface{DeviceID: same.ID, Name: "Ethernet3", Type: "1000base-t", NetboxID: 831}
	ifc2 := models.Interface{DeviceID: same.ID, Name: "Ethernet4", Type: "1000base-t", NetboxID: 832}
	mustCreate(t, db, &ifc1)
	mustCreate(t, db, &ifc2)
	svc2 := models.Service{ServiceID: "CN00101", ServiceType: "ELINE", Fields: jsonRaw(t, map[string]any{"bandwidth_mbps": 1})}
	mustCreate(t, db, &svc2)
	e1 := models.ServiceEndpoint{ServiceID: svc2.ID, Role: models.EndpointRoleInterface, DeviceID: same.ID, InterfaceID: ifc1.ID, Fields: EncodeEndpointFields(10, 0, 0)}
	e2 := models.ServiceEndpoint{ServiceID: svc2.ID, Role: models.EndpointRoleInterface, DeviceID: same.ID, InterfaceID: ifc2.ID, Fields: EncodeEndpointFields(20, 0, 0)}
	mustCreate(t, db, &e1)
	mustCreate(t, db, &e2)
	data, err = GenericData(db, &svc2, &e1, &same, &ifc1)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Others) != 1 {
		t.Fatalf("same-device Others=%d", len(data.Others))
	}
	if data.Others[0].NeighborIP != "" {
		t.Errorf("same-device NeighborIP = %q, want empty", data.Others[0].NeighborIP)
	}
	if data.Others[0].LocalIface != "Ethernet4" {
		t.Errorf("peer LocalIface = %q", data.Others[0].LocalIface)
	}
}

func TestGenericDataOthersSamePortDifferentVLAN(t *testing.T) {
	db := newTestDB(t)
	st := models.ServiceType{
		Name: "TWOvLAN",
		Interfaces: models.ServiceInterfacesSpec{
			Fields: []models.FieldSchema{{Name: "vlan", Type: models.FieldTypeVLAN, Required: true}},
		},
	}
	mustCreate(t, db, &st)
	dev := models.Device{Name: "pe-2vlan", Platform: "eos", NetboxID: 910}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 911}
	mustCreate(t, db, &ifc)
	svc := models.Service{ServiceID: "CN00300", ServiceType: "TWOvLAN"}
	mustCreate(t, db, &svc)
	eps := []models.ServiceEndpoint{
		{ServiceID: svc.ID, Role: models.EndpointRoleInterface, DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(10, 0, 0)},
		{ServiceID: svc.ID, Role: models.EndpointRoleInterface, DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(20, 0, 0)},
	}
	data, err := genericData(db, &svc, &eps[0], &dev, &ifc, eps, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Interfaces) != 2 || len(data.Others) != 1 {
		t.Fatalf("Interfaces=%d Others=%d, want 2/1 for draft same-port UNIs", len(data.Interfaces), len(data.Others))
	}
	vlan, _ := asInt(data.Others[0].Fields["vlan"])
	if vlan != 20 {
		t.Errorf("other vlan = %#v, want 20", data.Others[0].Fields["vlan"])
	}
	cur, _ := asInt(data.Current.Fields["vlan"])
	if cur != 10 {
		t.Errorf("current vlan = %#v", data.Current.Fields["vlan"])
	}
}

func TestResolveCommercialSameRowVsUnset(t *testing.T) {
	db := newTestDB(t)
	cust := models.Customer{Name: "Acme"}
	mustCreate(t, db, &cust)
	dev := models.Device{Name: "pe-cn", Platform: "eos", NetboxID: 920}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 921}
	mustCreate(t, db, &ifc)

	noPicker := models.ServiceType{
		Name: "NOPICK",
		Interfaces: models.ServiceInterfacesSpec{
			Min: 1, Max: 1,
			Fields: []models.FieldSchema{{Name: "vlan", Type: models.FieldTypeVLAN, Required: true}},
		},
	}
	mustCreate(t, db, &noPicker)
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00400", Name: "same-row", ServiceType: "NOPICK"}
	mustCreate(t, db, &svc)
	ep := models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(5, 0, 0),
	}
	mustCreate(t, db, &ep)
	data, err := GenericData(db, &svc, &ep, &dev, &ifc)
	if err != nil {
		t.Fatal(err)
	}
	if data.Current.Commercial == nil || data.Current.Commercial.ID != svc.ID {
		t.Fatalf("same-row Commercial = %+v, want technical row", data.Current.Commercial)
	}

	picker := models.ServiceType{
		Name: "PICK",
		Schema: []models.FieldSchema{
			{Name: "service_id", Type: models.FieldTypeServiceID},
		},
		Interfaces: models.ServiceInterfacesSpec{
			Min: 1, Max: 1,
			Fields: []models.FieldSchema{{Name: "vlan", Type: models.FieldTypeVLAN, Required: true}},
		},
	}
	mustCreate(t, db, &picker)
	tech := models.Service{CustomerID: cust.ID, ServiceID: "CN00401", Name: "technical", ServiceType: "PICK"}
	mustCreate(t, db, &tech)
	ep2 := models.ServiceEndpoint{
		ServiceID: tech.ID, Role: models.EndpointRoleInterface,
		DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(6, 0, 0),
	}
	mustCreate(t, db, &ep2)
	data, err = GenericData(db, &tech, &ep2, &dev, &ifc)
	if err != nil {
		t.Fatal(err)
	}
	if data.Current.Commercial != nil {
		t.Fatalf("unset service_id Commercial = %+v, want nil", data.Current.Commercial)
	}

	comm := models.Service{CustomerID: cust.ID, ServiceID: "CN00402", Name: "picked"}
	mustCreate(t, db, &comm)
	tech.Fields = jsonRaw(t, map[string]any{"service_id": comm.ID})
	if err := db.Save(&tech).Error; err != nil {
		t.Fatal(err)
	}
	data, err = GenericData(db, &tech, &ep2, &dev, &ifc)
	if err != nil {
		t.Fatal(err)
	}
	if data.Current.Commercial == nil || data.Current.Commercial.ID != comm.ID || data.Current.Commercial.ServiceID != "CN00402" {
		t.Fatalf("picked Commercial = %+v", data.Current.Commercial)
	}
}

func TestRenderFuncMapSDPIDAndMAC(t *testing.T) {
	db := newTestDB(t)
	out, err := Render(db, `{{ sdpid (index .Others 0).NeighborIP }} {{ macColon "aabb.ccdd.eeff" }} {{ macHyphen "AA:BB:CC:DD:EE:FF" }} {{ macCisco "aa-bb-cc-dd-ee-ff" }}`, "", GenericRenderData{
		Others: []RenderEndpoint{{NeighborIP: "172.27.250.28"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(out, " ")
	if !strings.Contains(got, "28") || !strings.Contains(got, "aa:bb:cc:dd:ee:ff") || !strings.Contains(got, "aa-bb-cc-dd-ee-ff") || !strings.Contains(got, "aabb.ccdd.eeff") {
		t.Errorf("funcmap output = %q", got)
	}
	if _, err := Render(db, `{{ sdpid (index .Others 0).NeighborIP }}`, "", GenericRenderData{
		Others: []RenderEndpoint{{NeighborIP: ""}},
	}); err == nil {
		t.Fatal("sdpid on empty NeighborIP should error")
	}
	if _, err := Render(db, `{{ .Fields.nope }}`, "", GenericRenderData{Fields: map[string]any{}}); err == nil {
		t.Fatal("missingkey=error should still fail")
	}
}

func TestRenderIndexOthersNotOthers0(t *testing.T) {
	db := newTestDB(t)
	data := GenericRenderData{
		Others: []RenderEndpoint{{NeighborIP: "10.1.1.1", Fields: map[string]any{"vlan": 9}}},
	}
	out, err := Render(db, `{{ (index .Others 0).NeighborIP }} {{ index (index .Others 0).Fields "vlan" }}`, "", data)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "10.1.1.1") || !strings.Contains(joined, "9") {
		t.Errorf("got %q", joined)
	}
}

func TestGenericDataFieldMetaUnit(t *testing.T) {
	db := newTestDB(t)
	st := models.ServiceType{
		Name: "META",
		Schema: []models.FieldSchema{
			{Name: "bandwidth_mbps", Type: models.FieldTypeInt, Unit: "Mbps"},
		},
		Interfaces: models.ServiceInterfacesSpec{
			Min: 1, Max: 1,
			Fields: []models.FieldSchema{{Name: "vlan", Type: models.FieldTypeVLAN, Required: true}},
		},
	}
	mustCreate(t, db, &st)
	dev := models.Device{Name: "pe-meta", Platform: "eos", NetboxID: 805}
	mustCreate(t, db, &dev)
	ifc := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 851}
	mustCreate(t, db, &ifc)
	svc := models.Service{ServiceID: "CN00200", ServiceType: "META", Fields: jsonRaw(t, map[string]any{"bandwidth_mbps": 10})}
	mustCreate(t, db, &svc)
	ep := models.ServiceEndpoint{
		ServiceID: svc.ID, Role: models.EndpointRoleInterface,
		DeviceID: dev.ID, InterfaceID: ifc.ID, Fields: EncodeEndpointFields(3, 0, 0),
	}
	mustCreate(t, db, &ep)
	data, err := GenericData(db, &svc, &ep, &dev, &ifc)
	if err != nil {
		t.Fatal(err)
	}
	if data.FieldMeta["bandwidth_mbps"].Unit != "Mbps" {
		t.Errorf("FieldMeta unit = %#v", data.FieldMeta["bandwidth_mbps"])
	}
}
