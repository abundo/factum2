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
	if len(body.Sites) != 1 || body.Sites[0].Name != "STO" {
		t.Errorf("sites = %+v", body.Sites)
	}
}

func TestApiGetTopologyDevices_IncludesUnlocated(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.Device{
		Name: "rtr1", NetboxID: 1, Site: "STO",
		Latitude: ptrFloat(59.3), Longitude: ptrFloat(18.0),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Device{Name: "rtr2", NetboxID: 2}).Error; err != nil {
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
