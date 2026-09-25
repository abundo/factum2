package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func ptrFloat(v float64) *float64 { return &v }

func TestApiGetTopology_OmitsUnlocatedDevices(t *testing.T) {
	db := newTestDB(t)
	placed := models.Device{
		Name: "rtr1", NetboxID: 1, Site: "STO",
		Manufacturer: "Nokia", ModelName: "7750 SR-1",
		Latitude: ptrFloat(59.3), Longitude: ptrFloat(18.0),
	}
	unplaced := models.Device{Name: "rtr2", NetboxID: 2}
	if err := db.Create(&placed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&unplaced).Error; err != nil {
		t.Fatal(err)
	}
	site := models.Site{NetboxID: 4, Name: "STO", Latitude: 59.3, Longitude: 18.0}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	region := models.Site{Name: "Sweden", NetboxKind: models.SiteNetboxKindRegion, NetboxID: 1, Source: models.SiteSourceNetbox}
	if err := db.Create(&region).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/topology", nil, nil, nil)
	if err := (&Controller{DB: db}).ApiGetTopology(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 1 || body.Devices[0].Name != "rtr1" {
		t.Errorf("devices = %+v, want [rtr1]", body.Devices)
	}
	if body.Devices[0].Manufacturer != "Nokia" || body.Devices[0].ModelName != "7750 SR-1" {
		t.Errorf("hardware = %q %q", body.Devices[0].Manufacturer, body.Devices[0].ModelName)
	}
	if len(body.Sites) != 1 || body.Sites[0].Name != "STO" {
		t.Errorf("sites = %+v", body.Sites)
	}
}

func TestApiGetTopology_EdgesIncludeInterfaceDescription(t *testing.T) {
	db := newTestDB(t)
	a := models.Device{
		Name: "rtr-a", NetboxID: 1, Site: "GBG",
		Latitude: ptrFloat(57.7), Longitude: ptrFloat(11.9),
	}
	b := models.Device{
		Name: "rtr-b", NetboxID: 2, Site: "STO",
		Latitude: ptrFloat(59.3), Longitude: ptrFloat(18.0),
	}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ifa := models.Interface{DeviceID: a.ID, NetboxID: 10, Name: "et-0/0/1", Description: "to Stockholm"}
	ifb := models.Interface{DeviceID: b.ID, NetboxID: 11, Name: "et-0/0/2", Description: "to Gothenburg"}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}
	conn := models.Connection{
		NetboxID: 20, DeviceAID: a.ID, InterfaceAID: ifa.ID,
		DeviceBID: b.ID, InterfaceBID: ifb.ID, Label: "FO-1",
	}
	if err := db.Create(&conn).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/topology", nil, nil, nil)
	if err := (&Controller{DB: db}).ApiGetTopology(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Edges) != 1 {
		t.Fatalf("edges = %+v, want 1", body.Edges)
	}
	e := body.Edges[0]
	if e.InterfaceA != "et-0/0/1" || e.InterfaceADescription != "to Stockholm" {
		t.Errorf("A = %q %q", e.InterfaceA, e.InterfaceADescription)
	}
	if e.InterfaceB != "et-0/0/2" || e.InterfaceBDescription != "to Gothenburg" {
		t.Errorf("B = %q %q", e.InterfaceB, e.InterfaceBDescription)
	}
	if e.Label != "FO-1" {
		t.Errorf("label = %q", e.Label)
	}
}

func TestApiGetTopology_ServiceLineColors(t *testing.T) {
	db := newTestDB(t)
	place := func(name string, lat float64) models.Device {
		d := models.Device{Name: name, Latitude: ptrFloat(lat), Longitude: ptrFloat(18)}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
		return d
	}
	peA := place("pe-a", 59.0)
	peB := place("pe-b", 59.2)
	peC := place("pe-c", 59.4)
	roadmA := place("roadm-a", 57.0)
	roadmB := place("roadm-b", 57.2)
	odfA := place("odf-a", 55.0)
	odfB := place("odf-b", 55.2)
	unplaced := models.Device{Name: "unplaced"}
	if err := db.Create(&unplaced).Error; err != nil {
		t.Fatal(err)
	}

	mkIface := func(dev models.Device, name string) models.Interface {
		iface := models.Interface{DeviceID: dev.ID, Name: name}
		if err := db.Create(&iface).Error; err != nil {
			t.Fatal(err)
		}
		return iface
	}
	mkCable := func(a, b models.Device, ifa, ifb models.Interface) models.Connection {
		c := models.Connection{
			DeviceAID: a.ID, InterfaceAID: ifa.ID,
			DeviceBID: b.ID, InterfaceBID: ifb.ID,
		}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	mkSvc := func(serviceID string) models.Service {
		s := models.Service{ServiceID: serviceID}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
		return s
	}

	// A wavelength and a dark fiber share one cable: mixed.
	wl := mkSvc("VL00001")
	fiber := mkSvc("LF00001")
	ifa := mkIface(roadmA, "deg-1")
	ifb := mkIface(roadmB, "deg-1")
	shared := mkCable(roadmA, roadmB, ifa, ifb)
	for i, svc := range []models.Service{wl, fiber} {
		connID := shared.ID
		hop := models.ServiceHop{
			ServiceID: svc.ID, Seq: i + 1, Kind: models.HopConnection, ConnectionID: &connID,
		}
		if err := db.Create(&hop).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Internal fiber hop colors that cable and does not also draw an arc.
	li := mkSvc("LI00007")
	odfIfA := mkIface(odfA, "port-a")
	odfIfB := mkIface(odfB, "port-b")
	fiberCable := mkCable(odfA, odfB, odfIfA, odfIfB)
	fiberConn := fiberCable.ID
	if err := db.Create(&models.ServiceHop{
		ServiceID: li.ID, Seq: 1, Kind: models.HopConnection, ConnectionID: &fiberConn,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServicePath{
		ServiceID: li.ID, Mode: models.TraceModeFiber, Status: models.PathComplete,
		EndpointAInterfaceID: odfIfA.ID, EndpointZInterfaceID: odfIfB.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Wavelength with ends on the map but no hop yet: a direct arc.
	vi := mkSvc("VI00003")
	peIfA := mkIface(peA, "1/1/1")
	peIfB := mkIface(peB, "1/1/1")
	plain := mkCable(peA, peB, peIfA, peIfB)
	if err := db.Create(&models.ServicePath{
		ServiceID: vi.ID, Mode: models.TraceModeWDM, Status: models.PathIncomplete,
		EndpointAInterfaceID: peIfA.ID, EndpointZInterfaceID: peIfB.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Capacity between two sites, plus an ELAN star, plus one end off the map.
	cn := mkSvc("CN00001")
	ci := mkSvc("CI00002")
	cnOff := mkSvc("CN00099")
	for _, ep := range []models.ServiceEndpoint{
		{ServiceID: cn.ID, Role: models.EndpointRoleInterface, DeviceID: peA.ID, InterfaceID: peIfA.ID},
		{ServiceID: cn.ID, Role: models.EndpointRoleInterface, DeviceID: peB.ID, InterfaceID: peIfB.ID},
		{ServiceID: ci.ID, Role: models.EndpointRoleInterface, DeviceID: peC.ID, InterfaceID: peIfA.ID},
		{ServiceID: ci.ID, Role: models.EndpointRoleInterface, DeviceID: peA.ID, InterfaceID: peIfA.ID},
		{ServiceID: ci.ID, Role: models.EndpointRoleInterface, DeviceID: peB.ID, InterfaceID: peIfB.ID},
		{ServiceID: cnOff.ID, Role: models.EndpointRoleInterface, DeviceID: peA.ID, InterfaceID: peIfA.ID},
		{ServiceID: cnOff.ID, Role: models.EndpointRoleInterface, DeviceID: unplaced.ID, InterfaceID: peIfA.ID},
	} {
		if err := db.Create(&ep).Error; err != nil {
			t.Fatal(err)
		}
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/topology", nil, nil, nil)
	if err := (&Controller{DB: db}).ApiGetTopology(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	byCable := map[uint]TopologyEdgeDTO{}
	for _, e := range body.Edges {
		byCable[e.ID] = e
	}
	if got := byCable[shared.ID]; got.Kind != "mixed" || len(got.ServiceIDs) != 2 ||
		got.ServiceIDs[0] != "LF00001" || got.ServiceIDs[1] != "VL00001" {
		t.Errorf("shared cable = %+v, want mixed LF00001,VL00001", got)
	}
	if got := byCable[fiberCable.ID]; got.Kind != "fiber" || len(got.ServiceIDs) != 1 || got.ServiceIDs[0] != "LI00007" {
		t.Errorf("fiber cable = %+v, want fiber LI00007", got)
	}
	if got := byCable[plain.ID]; got.Kind != "" || len(got.ServiceIDs) != 0 {
		t.Errorf("plain cable = %+v, want uncolored", got)
	}

	gotLinks := map[string]TopologyServiceLinkDTO{}
	for _, l := range body.ServiceLinks {
		gotLinks[l.ServiceID+"|"+strconv.FormatUint(uint64(l.DeviceAID), 10)+"|"+strconv.FormatUint(uint64(l.DeviceBID), 10)] = l
	}
	want := []TopologyServiceLinkDTO{
		{ServiceID: "CI00002", Category: "CI", Kind: "capacity", DeviceAID: peA.ID, DeviceBID: peB.ID, Label: "CI00002"},
		{ServiceID: "CI00002", Category: "CI", Kind: "capacity", DeviceAID: peA.ID, DeviceBID: peC.ID, Label: "CI00002"},
		{ServiceID: "CN00001", Category: "CN", Kind: "capacity", DeviceAID: peA.ID, DeviceBID: peB.ID, Label: "CN00001"},
		{ServiceID: "VI00003", Category: "VI", Kind: "wavelength", DeviceAID: peA.ID, DeviceBID: peB.ID, Label: "VI00003"},
	}
	if len(body.ServiceLinks) != len(want) {
		t.Fatalf("service links = %+v, want %d", body.ServiceLinks, len(want))
	}
	for _, w := range want {
		key := w.ServiceID + "|" + strconv.FormatUint(uint64(w.DeviceAID), 10) + "|" + strconv.FormatUint(uint64(w.DeviceBID), 10)
		got, ok := gotLinks[key]
		if !ok || got.Kind != w.Kind || got.Category != w.Category || got.Label != w.Label {
			t.Errorf("missing %+v in %+v", w, body.ServiceLinks)
		}
	}
}

func TestApiGetTopologyDevices_IncludesUnlocated(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.Device{
		Name: "rtr1", NetboxID: 1, Site: "STO",
		Manufacturer: "Nokia", ModelName: "7750 SR-1",
		Latitude: ptrFloat(59.3), Longitude: ptrFloat(18.0),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{Name: "rtr2", NetboxID: 2, Manufacturer: "Arista"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{Name: "vm1", NetboxID: 3, VM: true}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/topology/devices", nil, nil, nil)
	if err := (&Controller{DB: db}).ApiGetTopologyDevices(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyDevicesDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(body.Devices), body.Devices)
	}
	if body.Devices[0].Name != "rtr1" || body.Devices[1].Name != "rtr2" {
		t.Errorf("order = %q %q", body.Devices[0].Name, body.Devices[1].Name)
	}
	if body.Devices[0].Manufacturer != "Nokia" || body.Devices[0].ModelName != "7750 SR-1" {
		t.Errorf("rtr1 hardware = %q %q", body.Devices[0].Manufacturer, body.Devices[0].ModelName)
	}
	if body.Devices[1].Manufacturer != "Arista" || body.Devices[1].ModelName != "" {
		t.Errorf("rtr2 hardware = %q %q", body.Devices[1].Manufacturer, body.Devices[1].ModelName)
	}
	if body.Devices[1].Latitude != nil {
		t.Errorf("unlocated lat = %v, want nil", body.Devices[1].Latitude)
	}
}

func TestApiTopologyDeviceLocation_CreatesSite(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/dcim/sites/":
			_, _ = w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/dcim/sites/":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":9,"name":"Stockholm","slug":"stockholm","latitude":59.3,"longitude":18.0}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/devices/"):
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.NetboxApiURL = srv.URL
	settings.NetboxApiToken = "t"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		SiteName:  "Stockholm",
		Latitude:  ptrFloat(59.3),
		Longitude: ptrFloat(18.0),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Device.Site != "Stockholm" || body.Site == nil || body.Site.Name != "Stockholm" {
		t.Errorf("device/site = %+v / %+v", body.Device, body.Site)
	}
	if body.Device.Latitude == nil || *body.Device.Latitude != 59.3 {
		t.Errorf("lat = %v", body.Device.Latitude)
	}
}

func TestApiTopologyDeviceLocation_DeviceCoords(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42, CfSource: "netbox"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	var patched map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/devices/"):
			_ = json.NewDecoder(r.Body).Decode(&patched)
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)

	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.NetboxApiURL = srv.URL
	settings.NetboxApiToken = "t"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		Latitude:  ptrFloat(59.3),
		Longitude: ptrFloat(18.0),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Site != nil {
		t.Errorf("site = %+v, want omitted", body.Site)
	}
	if body.Device.Latitude == nil || *body.Device.Latitude != 59.3 {
		t.Errorf("lat = %v", body.Device.Latitude)
	}
	if patched["latitude"] != 59.3 || patched["longitude"] != 18.0 {
		t.Errorf("netbox patch = %v", patched)
	}
	if _, ok := patched["site"]; ok {
		t.Errorf("netbox patch included site: %v", patched)
	}
}

func TestApiTopologyDeviceLocation_RejectsDefault(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.NetboxApiURL = "http://127.0.0.1:1"
	settings.NetboxApiToken = "t"
	if err := db.Save(settings).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		SiteName: "Default",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiTopologyDeviceLocation_NotConfigured(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "rtr1", NetboxID: 42}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		SiteName: "STO", Latitude: ptrFloat(1), Longitude: ptrFloat(2),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiTopologyGeocode_BadCoords(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/topology/geocode?lat=x&lng=18", nil, nil, nil)
	if err := ctrl.ApiTopologyGeocode(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/topology/geocode?lat=91&lng=0", nil, nil, nil)
	if err := ctrl.ApiTopologyGeocode(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for out of range, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiTopologyDeviceLocation_NotFound(t *testing.T) {
	db := newTestDB(t)
	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		SiteName: "STO",
	}, []string{"id"}, []string{"99"})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiTopologyDeviceLocation_FactumDevice(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "local-rtr"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		Latitude:  ptrFloat(57.70887),
		Longitude: ptrFloat(11.97456),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Site != nil {
		t.Errorf("site = %+v, want omitted", body.Site)
	}
	if body.Device.Latitude == nil || *body.Device.Latitude != 57.70887 {
		t.Errorf("lat = %v", body.Device.Latitude)
	}

	var stored models.Device
	if err := db.First(&stored, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Latitude == nil || *stored.Latitude != 57.70887 {
		t.Errorf("stored lat = %v", stored.Latitude)
	}
}

func TestApiTopologyDeviceLocation_FactumDeviceCreatesSite(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "local-rtr"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/devices/x/location", topologyLocationRequest{
		SiteName:  "Hall",
		Latitude:  ptrFloat(59.3),
		Longitude: ptrFloat(18.0),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologyDeviceLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologyLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Site == nil || body.Site.Name != "Hall" || body.Site.Source != models.SiteSourceFactum {
		t.Errorf("site = %+v", body.Site)
	}
	if body.Device.Site != "Hall" {
		t.Errorf("device site = %q", body.Device.Site)
	}
}

func TestApiTopologySiteLocation_FactumSite(t *testing.T) {
	db := newTestDB(t)
	site := models.Site{Name: "Hall", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "local-rtr", Site: "Hall", SiteID: site.ID}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/sites/x/location", topologyLocationRequest{
		Latitude:  ptrFloat(59.329324),
		Longitude: ptrFloat(18.068581),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(site.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologySiteLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body TopologySiteLocationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Site.Latitude != 59.329324 || body.Site.Longitude != 18.068581 {
		t.Errorf("site = %+v", body.Site)
	}

	var storedDev models.Device
	if err := db.First(&storedDev, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedDev.Latitude == nil || *storedDev.Latitude != 59.329324 {
		t.Errorf("inherited lat = %v", storedDev.Latitude)
	}
}

func TestApiTopologySiteLocation_RejectsNetboxSite(t *testing.T) {
	db := newTestDB(t)
	site := models.Site{Name: "STO", Source: models.SiteSourceNetbox, NetboxID: 4, NetboxKind: models.SiteNetboxKindSite}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/topology/sites/x/location", topologyLocationRequest{
		Latitude:  ptrFloat(59.3),
		Longitude: ptrFloat(18.0),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(site.ID), 10)})
	if err := (&Controller{DB: db}).ApiTopologySiteLocation(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
