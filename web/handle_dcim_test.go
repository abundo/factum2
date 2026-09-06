package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abundo/factum2/models"
)

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
