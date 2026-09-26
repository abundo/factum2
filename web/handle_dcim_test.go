package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestApiDCIMConnectionPairCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	a := models.Device{Name: "leaf-1", CfSource: "factum"}
	b := models.Device{Name: "spine-1", CfSource: "factum"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Ethernet1", Description: "uplink"}
	ib := models.Interface{DeviceID: b.ID, Name: "Ethernet1", Description: "to leaf"}
	ic := models.Interface{DeviceID: b.ID, Name: "Ethernet2"}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ic).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/connections/pair?a="+strconv.FormatUint(uint64(a.ID), 10)+"&b="+strconv.FormatUint(uint64(b.ID), 10), nil, nil, nil)
	if err := ctrl.ApiDCIMConnectionPair(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("pair status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/connections", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID, Label: "uplink",
	}, nil, nil)
	if err := ctrl.ApiDCIMConnectionCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Connection
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.NetboxID != 0 || created.Label != "uplink" {
		t.Fatalf("created = %+v", created)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/connections/x", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ic.ID, Label: "moved",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDCIMConnectionUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}

	nb := models.Connection{
		NetboxID: 5, DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID,
	}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/connections/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(nb.ID), 10)})
	if err := ctrl.ApiDCIMConnectionDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("nb delete status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/connections/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDCIMConnectionDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiDCIMConnectionCreatePushesToNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	a := models.Device{Name: "nb-a", NetboxID: 1, CfSource: "netbox"}
	b := models.Device{Name: "nb-b", NetboxID: 2, CfSource: "netbox"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Eth1", NetboxID: 11}
	ib := models.Interface{DeviceID: b.ID, Name: "Eth1", NetboxID: 22}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/connections", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID, Label: "uplink",
	}, nil, nil)
	if err := ctrl.ApiDCIMConnectionCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no netbox config status = %d body=%s", rec.Code, rec.Body.String())
	}

	var posted map[string]any
	var patched map[string]any
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/dcim/cables/":
			_ = json.Unmarshal(body, &posted)
			_, _ = w.Write([]byte(`{
				"id": 99,
				"label": "uplink",
				"a_terminations": [{"object_type": "dcim.interface", "object_id": 11}],
				"b_terminations": [{"object_type": "dcim.interface", "object_id": 22}]
			}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/dcim/cables/"):
			_ = json.Unmarshal(body, &patched)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/dcim/cables/"):
			deleted = true
			w.WriteHeader(http.StatusNoContent)
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

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/connections", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID, Label: "uplink",
	}, nil, nil)
	if err := ctrl.ApiDCIMConnectionCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Connection
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.NetboxID != 99 {
		t.Fatalf("netbox_id = %d, want 99; posted=%v", created.NetboxID, posted)
	}
	if posted["label"] != "uplink" {
		t.Fatalf("posted label = %v", posted["label"])
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/connections/x", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID, Label: "renamed",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDCIMConnectionUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if patched["label"] != "renamed" {
		t.Fatalf("patched = %v", patched)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/connections/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDCIMConnectionDelete(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !deleted {
		t.Fatal("want NetBox cable deleted")
	}
}

func TestApiDCIMConnectionCreateKeepsWebhookRow(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	a := models.Device{Name: "lu17-lab-r0.itn.nu", NetboxID: 1, CfSource: "netbox"}
	b := models.Device{Name: "lu17-lab-r2.itn.nu", NetboxID: 2, CfSource: "netbox"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Ethernet4", NetboxID: 11}
	ib := models.Interface{DeviceID: b.ID, Name: "Ethernet4", NetboxID: 22}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}

	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/dcim/cables/":
			// Webhook lands before the local insert, same as the lab log.
			wh := models.Connection{
				NetboxID: 8, DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID, Label: "uplink",
			}
			if err := db.Create(&wh).Error; err != nil {
				t.Errorf("webhook insert: %v", err)
			}
			_, _ = w.Write([]byte(`{
				"id": 8,
				"label": "uplink",
				"a_terminations": [{"object_type": "dcim.interface", "object_id": 11}],
				"b_terminations": [{"object_type": "dcim.interface", "object_id": 22}]
			}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/dcim/cables/"):
			deleted = true
			w.WriteHeader(http.StatusNoContent)
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

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/connections", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID, Label: "uplink",
	}, nil, nil)
	if err := ctrl.ApiDCIMConnectionCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if deleted {
		t.Fatal("webhook cable was rolled back in NetBox")
	}
	var n int64
	if err := db.Model(&models.Connection{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("connections = %d, want the webhook row kept", n)
	}
}

func TestApiDCIMConnectionCreateExistingPairSkipsNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	a := models.Device{Name: "lu17-lab-r0.itn.nu", NetboxID: 1, CfSource: "netbox"}
	b := models.Device{Name: "lu17-lab-r2.itn.nu", NetboxID: 2, CfSource: "netbox"}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	ia := models.Interface{DeviceID: a.ID, Name: "Ethernet4", NetboxID: 11}
	ib := models.Interface{DeviceID: b.ID, Name: "Ethernet4", NetboxID: 22}
	if err := db.Create(&ia).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	wh := models.Connection{
		NetboxID: 8, DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID,
	}
	if err := db.Create(&wh).Error; err != nil {
		t.Fatal(err)
	}
	local := models.Connection{DeviceAID: a.ID, InterfaceAID: ia.ID, DeviceBID: b.ID, InterfaceBID: ib.ID}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}

	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		http.Error(w, "netbox should not be called", http.StatusInternalServerError)
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

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/connections", models.ConnectionWriteDTO{
		InterfaceAID: ia.ID, InterfaceBID: ib.ID,
	}, nil, nil)
	if err := ctrl.ApiDCIMConnectionCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if posts != 0 {
		t.Fatalf("netbox calls = %d", posts)
	}
	var n int64
	if err := db.Model(&models.Connection{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("connections = %d, want 1", n)
	}
}

func TestApiGetConnections(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/connections", nil, nil, nil)
	if err := ctrl.ApiGetConnections(c); err != nil {
		t.Fatalf("empty: %v", err)
	}
	var empty []ConnectionListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if empty == nil {
		t.Fatal("want [] not null")
	}

	devA := models.Device{Name: "pe-a", NetboxID: 201}
	devB := models.Device{Name: "pe-b", NetboxID: 202}
	db.Create(&devA)
	db.Create(&devB)
	ia := models.Interface{DeviceID: devA.ID, Name: "Eth1"}
	ib := models.Interface{DeviceID: devB.ID, Name: "Eth2"}
	db.Create(&ia)
	db.Create(&ib)
	db.Create(&models.Connection{
		DeviceAID: devA.ID, InterfaceAID: ia.ID,
		DeviceBID: devB.ID, InterfaceBID: ib.ID,
		Label: "trunk",
	})

	c, rec = jsonRequest(t, http.MethodGet, "/api/connections", nil, nil, nil)
	if err := ctrl.ApiGetConnections(c); err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []ConnectionListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "pe-a Eth1 ↔ pe-b Eth2 (trunk)" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestApiGetDevicesIncludeInterfaces(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "dns-sync-r1", NetboxID: 424242, PrimaryIPv4: "10.0.0.1/32"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1/1"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatalf("create interface: %v", err)
	}
	addr := models.Address{InterfaceID: iface.ID, Address: "10.1.1.1/24"}
	if err := db.Create(&addr).Error; err != nil {
		t.Fatalf("create address: %v", err)
	}

	ctrl := &Controller{DB: db}

	c, rec := jsonRequest(t, http.MethodGet, "/api/device", nil, nil, nil)
	if err := ctrl.ApiGetDevices(c); err != nil {
		t.Fatalf("ApiGetDevices shallow: %v", err)
	}
	var shallow []models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &shallow); err != nil {
		t.Fatalf("decode shallow: %v", err)
	}
	shallowDev := findDevice(t, shallow, "dns-sync-r1")
	if len(shallowDev.Interfaces) != 0 {
		t.Fatalf("shallow interfaces = %d, want 0 (list view must stay cheap)", len(shallowDev.Interfaces))
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/device?include=interfaces", nil, nil, nil)
	if err := ctrl.ApiGetDevices(c); err != nil {
		t.Fatalf("ApiGetDevices include=interfaces: %v", err)
	}
	var full []models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode full: %v", err)
	}
	fullDev := findDevice(t, full, "dns-sync-r1")
	if len(fullDev.Interfaces) != 1 {
		t.Fatalf("full interfaces = %d, want 1", len(fullDev.Interfaces))
	}
	if fullDev.Interfaces[0].Name != "Ethernet1/1" {
		t.Errorf("interface name = %q, want Ethernet1/1", fullDev.Interfaces[0].Name)
	}
	if len(fullDev.Interfaces[0].Addresses) != 1 || fullDev.Interfaces[0].Addresses[0].Address != "10.1.1.1/24" {
		t.Errorf("addresses = %+v, want [10.1.1.1/24]", fullDev.Interfaces[0].Addresses)
	}
}

func TestApiDeviceCreateVM(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	mfr := models.Manufacturer{Name: "Generic", Slug: "generic"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	vmType := true
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "virt", Slug: "virt", VM: vmType}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}

	vm := true
	c, rec := jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "vm-explicit",
		DeviceTypeID: dt.ID,
		VM:           &vm,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.VM {
		t.Fatal("explicit vm flag was not stored")
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "vm-from-type",
		DeviceTypeID: dt.ID,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatal(err)
	}
	var fromType models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &fromType); err != nil {
		t.Fatal(err)
	}
	if !fromType.VM {
		t.Fatal("create without vm should copy the device type flag")
	}

	off := false
	c, rec = jsonRequest(t, http.MethodPut, "/api/device/x", models.DeviceCreateDTO{
		Name:         "vm-explicit",
		DeviceTypeID: dt.ID,
		VM:           &off,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeviceUpdate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.VM {
		t.Fatal("update did not clear vm")
	}
}

func TestApiDeviceCreateLocal(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	mfr := models.Manufacturer{Name: "Arista", Slug: "arista"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatalf("manufacturer: %v", err)
	}
	plat := models.Platform{Name: "Arista EOS", Slug: "eos"}
	if err := db.Create(&plat).Error; err != nil {
		t.Fatalf("platform: %v", err)
	}
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "DCS-7050", Slug: "dcs-7050", PlatformID: plat.ID}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatalf("device type: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "leaf-1",
		DeviceTypeID: dt.ID,
		PlatformID:   plat.ID,
		Site:         "lab",
		Status:       "active",
		PrimaryIPv4:  "10.0.0.1/32",
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 0 || created.NetboxID != 0 || created.CfSource != "factum" {
		t.Fatalf("created = %+v", created)
	}
	if created.Manufacturer != "Arista" || created.ModelName != "DCS-7050" || created.Platform != "eos" {
		t.Fatalf("denormalized fields = %+v", created)
	}
	if created.DeviceTypeID != dt.ID {
		t.Fatalf("device_type_id = %d, want %d", created.DeviceTypeID, dt.ID)
	}
	if created.PlatformID != plat.ID {
		t.Fatalf("platform_id = %d, want %d", created.PlatformID, plat.ID)
	}
	if created.PrimaryIPv4 != "" || created.PrimaryIPv4ID != 0 {
		t.Fatalf("primary ipv4 must not be set from device DTO = %+v", created)
	}
	if err := db.Create(&models.Interface{DeviceID: created.ID, Name: "Ethernet1", Type: "1000base-t"}).Error; err != nil {
		t.Fatalf("interface: %v", err)
	}

	site := models.Site{Name: "STO", Source: models.SiteSourceFactum, Latitude: 59.3, Longitude: 18.0}
	if err := db.Create(&site).Error; err != nil {
		t.Fatalf("site: %v", err)
	}
	c, rec = jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "leaf-at-site",
		DeviceTypeID: dt.ID,
		SiteID:       site.ID,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatalf("create with site_id: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("site_id status = %d body=%s", rec.Code, rec.Body.String())
	}
	var withSite models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &withSite); err != nil {
		t.Fatalf("decode with site: %v", err)
	}
	if withSite.SiteID != site.ID || withSite.Site != "STO" {
		t.Fatalf("site fields = %+v, want id=%d name=STO", withSite, site.ID)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "leaf-from-type-platform",
		DeviceTypeID: dt.ID,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatalf("create from type platform: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("from type platform status = %d body=%s", rec.Code, rec.Body.String())
	}
	var fromType models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &fromType); err != nil {
		t.Fatalf("decode from type: %v", err)
	}
	if fromType.Platform != "eos" || fromType.PlatformID != plat.ID {
		t.Fatalf("platform copied from device type = %+v", fromType)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "leaf-2",
		DeviceTypeID: dt.ID,
		PlatformID:   plat.ID,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("second status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{Name: "no-type"}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatalf("missing type: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing type status = %d", rec.Code)
	}

	enabled := false
	icinga := true
	oxidized := true
	c, rec = jsonRequest(t, http.MethodPut, "/api/device/x", models.DeviceCreateDTO{
		Name:             "leaf-1-renamed",
		DeviceTypeID:     dt.ID,
		PlatformID:       plat.ID,
		Site:             "lab2",
		Status:           "offline",
		PrimaryIPv4:      "10.0.0.2/32",
		PrimaryIPv6:      "2001:db8::1/128",
		Comments:         "lab box",
		Enabled:          &enabled,
		CfLocation:       "row-a",
		CfMonitorIcinga:  &icinga,
		CfBackupOxidized: &oxidized,
		OpticalKind:      "roadm",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeviceUpdate(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Name != "leaf-1-renamed" || updated.Site != "lab2" || updated.Status != "offline" {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.Enabled || updated.Comments != "lab box" {
		t.Fatalf("updated extra = %+v", updated)
	}
	if updated.PrimaryIPv4 != "" || updated.PrimaryIPv6 != "" || updated.PrimaryIPv6ID != 0 {
		t.Fatalf("primary IPs must not be set from device DTO = %+v", updated)
	}
	if updated.CfLocation != "row-a" || !updated.CfMonitorIcinga || !updated.CfBackupOxidized {
		t.Fatalf("updated monitoring = %+v", updated)
	}
	if updated.OpticalKind != "roadm" {
		t.Fatalf("optical_kind = %q", updated.OpticalKind)
	}
	if len(updated.Interfaces) != 1 || updated.Interfaces[0].Name != "Ethernet1" {
		t.Fatalf("update dropped interfaces: %+v", updated.Interfaces)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/device/x", models.DeviceCreateDTO{
		Name:         "leaf-1-renamed",
		DeviceTypeID: dt.ID,
		OpticalKind:  "not-a-kind",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeviceUpdate(c); err != nil {
		t.Fatalf("bad optical: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad optical status = %d", rec.Code)
	}

	nbDev := models.Device{Name: "from-nb", NetboxID: 7, CfSource: "netbox"}
	if err := db.Create(&nbDev).Error; err != nil {
		t.Fatalf("nb device: %v", err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/device/x", models.DeviceCreateDTO{
		Name:         "nope",
		DeviceTypeID: dt.ID,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(nbDev.ID), 10)})
	if err := ctrl.ApiDeviceUpdate(c); err != nil {
		t.Fatalf("update nb: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("update nb status = %d", rec.Code)
	}
}

func TestApiDeviceDeleteLocalVsNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	local := models.Device{Name: "local-1", CfSource: "factum"}
	nb := models.Device{Name: "nb-1", NetboxID: 99, CfSource: "netbox"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatalf("local: %v", err)
	}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatalf("netbox: %v", err)
	}

	c, rec := jsonRequest(t, http.MethodDelete, "/api/device/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(nb.ID), 10)})
	if err := ctrl.ApiDeviceDelete(c); err != nil {
		t.Fatalf("delete netbox: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("netbox delete status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/device/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(local.ID), 10)})
	if err := ctrl.ApiDeviceDelete(c); err != nil {
		t.Fatalf("delete local: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("local delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func findDevice(t *testing.T, devices []models.Device, name string) models.Device {
	t.Helper()
	for _, d := range devices {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("device %q not in %d-device list", name, len(devices))
	return models.Device{}
}

func TestApiDeviceCreateCopiesInterfaceTemplates(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	mfr := models.Manufacturer{Name: "Arista", Slug: "arista"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "DCS-7050", Slug: "dcs-7050"}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.InterfaceTemplate{DeviceTypeID: dt.ID, Name: "Ethernet1", Type: "1000base-t"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.InterfaceTemplate{DeviceTypeID: dt.ID, Name: "Management1", Type: "1000base-t"}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/device", models.DeviceCreateDTO{
		Name:         "leaf-1",
		DeviceTypeID: dt.ID,
	}, nil, nil)
	if err := ctrl.ApiDeviceCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	var ifaces []models.Interface
	if err := db.Where("device_id = ?", created.ID).Order("name").Find(&ifaces).Error; err != nil {
		t.Fatal(err)
	}
	if len(ifaces) != 2 || ifaces[0].Name != "Ethernet1" || ifaces[1].Name != "Management1" {
		t.Fatalf("ifaces = %+v", ifaces)
	}
	if ifaces[0].NetboxID != 0 || ifaces[0].Type != "1000base-t" || !ifaces[0].Enabled {
		t.Fatalf("Ethernet1 = %+v", ifaces[0])
	}
}

func TestApiDCIMInterfacesCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	local := models.Device{Name: "local-1", CfSource: "factum"}
	nb := models.Device{Name: "nb-1", NetboxID: 9, CfSource: "netbox"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&nb).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Interface{DeviceID: nb.ID, Name: "Eth1", NetboxID: 50, Type: "1000base-t"}).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/interfaces", nil, nil, nil)
	if err := ctrl.ApiGetDCIMInterfaces(c); err != nil {
		t.Fatal(err)
	}
	var listed DCIMInterfaceListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].DeviceName != "nb-1" || listed.Items[0].Source != "netbox" {
		t.Fatalf("list = %+v", listed)
	}

	enabled := true
	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interfaces", models.InterfaceCreateDTO{
		DeviceID: local.ID, Name: "Ethernet1", Type: "10gbase-x-sfpp", VRF: "MGMT", Enabled: &enabled,
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Interface
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.NetboxID != 0 || created.Type != "10gbase-x-sfpp" || created.VRF != "MGMT" {
		t.Fatalf("created = %+v", created)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interfaces", models.InterfaceCreateDTO{
		DeviceID: local.ID, Name: "Ethernet2", Type: "1000base-t",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("second create status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interfaces", models.InterfaceCreateDTO{
		DeviceID: local.ID, Name: "Ethernet1", Type: "1000base-t",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dup status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/interfaces", models.InterfaceCreateDTO{
		DeviceID: nb.ID, Name: "Ethernet9", Type: "1000base-t",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nb create status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/interfaces/x", models.InterfaceCreateDTO{
		Name: "Ethernet1-renamed", Type: "1000base-t", Description: "uplink", VRF: "CUST",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiUpdateDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated models.Interface
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.VRF != "CUST" || updated.Name != "Ethernet1-renamed" {
		t.Fatalf("updated = %+v", updated)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/dcim/interfaces", nil, nil, nil)
	if err := ctrl.ApiGetDCIMInterfaces(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 3 || len(listed.Items) != 3 {
		t.Fatalf("listed total=%d items=%d, want 3: %+v", listed.Total, len(listed.Items), listed)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/dcim/interfaces?limit=1&offset=0", nil, nil, nil)
	if err := ctrl.ApiGetDCIMInterfaces(c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 3 || len(listed.Items) != 1 || listed.Limit != 1 {
		t.Fatalf("paged = %+v", listed)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/interfaces/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDCIMInterface(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiDCIMAddressesCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	ns := models.IpamNamespace{Name: "global"}
	if err := db.Create(&ns).Error; err != nil {
		t.Fatal(err)
	}
	vrf := models.IpamVRF{NamespaceID: ns.ID, Name: "MGMT", IsDefault: true}
	if err := db.Create(&vrf).Error; err != nil {
		t.Fatal(err)
	}
	pfx := models.IpamPrefix{NamespaceID: ns.ID, VRFID: vrf.ID, Prefix: "10.0.0.0/24", Family: 4}
	if err := db.Create(&pfx).Error; err != nil {
		t.Fatal(err)
	}

	dev := models.Device{Name: "pe-1", CfSource: "factum"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1", Type: "1000base-t"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	nbAddr := models.Address{InterfaceID: iface.ID, Address: "192.0.2.1/32", NetboxID: 88}
	if err := db.Create(&nbAddr).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/addresses", nil, nil, nil)
	if err := ctrl.ApiGetDCIMAddresses(c); err != nil {
		t.Fatal(err)
	}
	var listed DCIMAddressListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Items[0].DeviceName != "pe-1" || listed.Items[0].Source != "netbox" {
		t.Fatalf("list = %+v", listed)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.1/24", DNSName: "lo.pe-1.example", VRF: "MGMT",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created models.Address
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.NetboxID != 0 || created.Address != "10.0.0.1/24" || created.VRF != "MGMT" {
		t.Fatalf("created = %+v", created)
	}
	if created.PrefixID == nil || *created.PrefixID != pfx.ID {
		t.Fatalf("created prefix_id = %v want %d", created.PrefixID, pfx.ID)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "203.0.113.1/32",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no-prefix status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.1/24",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dup status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "not-an-ip",
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cidr status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/addresses/x", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.2/24", DNSName: "lo2.pe-1.example",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiUpdateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/addresses/x", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "203.0.113.1/32",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(nbAddr.ID), 10)})
	if err := ctrl.ApiUpdateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nb update status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/addresses/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(nbAddr.ID), 10)})
	if err := ctrl.ApiDeleteDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nb delete status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/addresses/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(created.ID), 10)})
	if err := ctrl.ApiDeleteDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func boolPtr(v bool) *bool { return &v }

func TestApiDCIMAddressManagementPrimary(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	ns := models.IpamNamespace{Name: "global"}
	if err := db.Create(&ns).Error; err != nil {
		t.Fatal(err)
	}
	vrf := models.IpamVRF{NamespaceID: ns.ID, Name: "MGMT", IsDefault: true}
	if err := db.Create(&vrf).Error; err != nil {
		t.Fatal(err)
	}
	pfx4 := models.IpamPrefix{NamespaceID: ns.ID, VRFID: vrf.ID, Prefix: "10.0.0.0/24", Family: 4}
	if err := db.Create(&pfx4).Error; err != nil {
		t.Fatal(err)
	}
	pfx6 := models.IpamPrefix{NamespaceID: ns.ID, VRFID: vrf.ID, Prefix: "2001:db8::/64", Family: 6}
	if err := db.Create(&pfx6).Error; err != nil {
		t.Fatal(err)
	}

	dev := models.Device{Name: "pe-mgmt", CfSource: "factum"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Management1", Type: "1000base-t"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.1/24", Management: boolPtr(true),
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create v4 status = %d body=%s", rec.Code, rec.Body.String())
	}
	var v4 models.Address
	if err := json.Unmarshal(rec.Body.Bytes(), &v4); err != nil {
		t.Fatal(err)
	}
	var got models.Device
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv4ID != v4.ID || got.PrimaryIPv4 != "10.0.0.1/24" {
		t.Fatalf("primary v4 after create = id=%d addr=%q, want id=%d 10.0.0.1/24", got.PrimaryIPv4ID, got.PrimaryIPv4, v4.ID)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "2001:db8::1/64", Management: boolPtr(true),
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create v6 status = %d body=%s", rec.Code, rec.Body.String())
	}
	var v6 models.Address
	if err := json.Unmarshal(rec.Body.Bytes(), &v6); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv4ID != v4.ID || got.PrimaryIPv6ID != v6.ID || got.PrimaryIPv6 != "2001:db8::1/64" {
		t.Fatalf("primaries after v6 = v4=%d/%q v6=%d/%q", got.PrimaryIPv4ID, got.PrimaryIPv4, got.PrimaryIPv6ID, got.PrimaryIPv6)
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/addresses", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.2/24", Management: boolPtr(true),
	}, nil, nil)
	if err := ctrl.ApiCreateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("replace v4 status = %d body=%s", rec.Code, rec.Body.String())
	}
	var v4b models.Address
	if err := json.Unmarshal(rec.Body.Bytes(), &v4b); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv4ID != v4b.ID || got.PrimaryIPv4 != "10.0.0.2/24" || got.PrimaryIPv6ID != v6.ID {
		t.Fatalf("replace v4 = %+v", got)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/addresses/x", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.3/24", Management: boolPtr(true),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(v4b.ID), 10)})
	if err := ctrl.ApiUpdateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("update v4 status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv4ID != v4b.ID || got.PrimaryIPv4 != "10.0.0.3/24" {
		t.Fatalf("denormalized v4 after edit = id=%d addr=%q", got.PrimaryIPv4ID, got.PrimaryIPv4)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/addresses/x", models.AddressCreateDTO{
		InterfaceID: iface.ID, Address: "10.0.0.3/24", Management: boolPtr(false),
	}, []string{"id"}, []string{strconv.FormatUint(uint64(v4b.ID), 10)})
	if err := ctrl.ApiUpdateDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("clear v4 status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv4ID != 0 || got.PrimaryIPv4 != "" || got.PrimaryIPv6ID != v6.ID {
		t.Fatalf("after uncheck v4 = %+v", got)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/addresses/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(v6.ID), 10)})
	if err := ctrl.ApiDeleteDCIMAddress(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete v6 status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&got, dev.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.PrimaryIPv6ID != 0 || got.PrimaryIPv6 != "" {
		t.Fatalf("after delete v6 = %+v", got)
	}
}

func TestApiInterfaceTemplatesCRUD(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}

	mfr := models.Manufacturer{Name: "Arista", Slug: "arista"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "DCS-7050", Slug: "dcs-7050", Source: "factum"}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "leaf-1", CfSource: "factum", DeviceTypeID: dt.ID, Manufacturer: "Arista", ModelName: "DCS-7050"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/device-types/x/interface-templates", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dt.ID), 10)})
	if err := ctrl.ApiGetInterfaceTemplates(c); err != nil {
		t.Fatal(err)
	}
	var empty []models.InterfaceTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty == nil {
		t.Fatal("want [] not null")
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/device-types/x/interface-templates", models.InterfaceTemplateDTO{
		Name: "Ethernet1", Type: "1000base-t", Description: "access",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dt.ID), 10)})
	if err := ctrl.ApiCreateInterfaceTemplate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var tmpl models.InterfaceTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &tmpl); err != nil {
		t.Fatal(err)
	}
	if tmpl.Source != "factum" || tmpl.DeviceTypeID != dt.ID {
		t.Fatalf("tmpl = %+v", tmpl)
	}
	var ifaces []models.Interface
	if err := db.Where("device_id = ?", dev.ID).Find(&ifaces).Error; err != nil {
		t.Fatal(err)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "Ethernet1" {
		t.Fatalf("instantiated = %+v", ifaces)
	}

	nbTmpl := models.InterfaceTemplate{DeviceTypeID: dt.ID, Name: "Management1", Type: "1000base-t", Source: "netbox", NetboxID: 7}
	if err := db.Create(&nbTmpl).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/interface-templates/x", models.InterfaceTemplateDTO{
		Name: "Management1", Type: "virtual",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(nbTmpl.ID), 10)})
	if err := ctrl.ApiUpdateInterfaceTemplate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nb update status = %d", rec.Code)
	}

	c, rec = jsonRequest(t, http.MethodDelete, "/api/dcim/interface-templates/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(tmpl.ID), 10)})
	if err := ctrl.ApiDeleteInterfaceTemplate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if err := db.First(&ifaces[0], ifaces[0].ID).Error; err != nil {
		t.Fatal("deleting a template must not delete instantiated interfaces")
	}
}
