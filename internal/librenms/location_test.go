package librenms

import (
	"bytes"
	"database/sql"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"

	_ "github.com/glebarez/sqlite"
)

func f64(v float64) *float64 { return &v }

func f32(v float64) *Float32String {
	x := Float32String(float32(v))
	return &x
}

func TestPlanLocationCoordinates(t *testing.T) {
	a := &models.Device{LibrenmsID: 1, CfLocation: "STO", Latitude: f64(59.3), Longitude: f64(18.0)}
	b := &models.Device{LibrenmsID: 2, CfLocation: "STO", Latitude: f64(59.3), Longitude: f64(18.0)}
	c := &models.Device{LibrenmsID: 3, CfLocation: "GOT", Latitude: f64(57.7), Longitude: f64(11.9)}
	// Shares STO with a, but its own pin differs. Lowest device id wins.
	d := &models.Device{LibrenmsID: 4, CfLocation: "STO", Latitude: f64(59.4), Longitude: f64(18.1)}
	// No custom location: keep the LibreNMS location name.
	e := &models.Device{LibrenmsID: 5, Latitude: f64(65.0), Longitude: f64(22.0)}
	// Coordinates and no location name: a coordinate location.
	f := &models.Device{LibrenmsID: 6, Latitude: f64(1), Longitude: f64(2)}
	// Destination points at a different LibreNMS device.
	g := &models.Device{LibrenmsID: 99, CfLocation: "STO", Latitude: f64(1), Longitude: f64(2)}
	byID := map[int]*models.Device{1: a, 2: b, 3: c, 4: d, 5: e, 6: f, 7: g}

	devices := []*LibrenmsDevice{
		{DeviceID: 4, Hostname: "10.0.0.4", Location: "old"},
		{DeviceID: 1, Hostname: "10.0.0.1", Location: "old"},
		{DeviceID: 2, Hostname: "10.0.0.2"},
		{DeviceID: 3, Hostname: "10.0.0.3"},
		{DeviceID: 5, Hostname: "10.0.0.5", Location: "LULEA"},
		{DeviceID: 6, Hostname: "10.0.0.6"},
		{DeviceID: 7, Hostname: "10.0.0.7"},
		{DeviceID: 8, Hostname: "10.0.0.8"},
	}
	plan := planLocationCoordinates(devices, map[int]struct{}{3: {}}, func(d *LibrenmsDevice) *models.Device {
		return byID[d.DeviceID]
	}, nil)

	sto := plan["STO"]
	if sto == nil || sto.DeviceID != 1 || !sto.Conflict || !coordsClose(sto.Lat, sto.Lng, 59.3, 18.0) {
		t.Fatalf("STO = %+v", sto)
	}
	if _, ok := plan["GOT"]; ok {
		t.Fatal("quarantined device was planned")
	}
	lulea := plan["LULEA"]
	if lulea == nil || lulea.DeviceID != 5 || lulea.Conflict {
		t.Fatalf("LULEA = %+v", lulea)
	}
	if _, ok := plan[""]; ok {
		t.Fatal("empty location was planned")
	}
	dummy := plan[coordinateLocationName(1, 2, nil)]
	if dummy == nil || dummy.DeviceID != 6 || dummy.Conflict {
		t.Fatalf("coordinate location = %+v", dummy)
	}
	if len(plan) != 3 {
		t.Fatalf("plan = %d locations, want 3", len(plan))
	}
}

func TestLibrenmsLocationName(t *testing.T) {
	d := &models.Device{Latitude: f64(59.329324), Longitude: f64(18.068581)}
	nms := &LibrenmsDevice{}
	const want = "9FFW83H9+PCG"
	if got := librenmsLocationName(d, nms, nil); got != want {
		t.Fatalf("no location: %q", got)
	}
	nms.Location = "STO"
	if got := librenmsLocationName(d, nms, nil); got != "STO" {
		t.Fatalf("existing location: %q", got)
	}
	d.CfLocation = "Lab"
	if got := librenmsLocationName(d, nms, nil); got != "Lab" {
		t.Fatalf("custom location: %q", got)
	}
	d.CfLocation = ""
	nms.Location = "location <1.000000,2.000000>"
	if got := librenmsLocationName(d, nms, nil); got != want {
		t.Fatalf("moved coordinate location: %q", got)
	}
	nms.Location = openLocationCode(1, 2)
	if got := librenmsLocationName(d, nms, nil); got != want {
		t.Fatalf("moved open location code: %q", got)
	}
	d.Latitude, d.Longitude = nil, nil
	if got := librenmsLocationName(d, nms, nil); got != nms.Location {
		t.Fatalf("coordinate location without coordinates: %q", got)
	}
}

func TestCoordinateLocationNameNearSite(t *testing.T) {
	const lat, lng = 59.329324, 18.068581
	here := []locationPlace{{Name: "Hemmingsmark", Lat: lat, Lng: lng}}
	if got := coordinateLocationName(lat, lng, here); got != "+PCG Hemmingsmark" {
		t.Fatalf("at the site: %q", got)
	}
	// About 1.1 km north: short enough to name the town, too far for a
	// leading "+".
	near := []locationPlace{{Name: "Hemmingsmark", Lat: lat + 0.01, Lng: lng}}
	if got := coordinateLocationName(lat, lng, near); got != "H9+PCG Hemmingsmark" {
		t.Fatalf("nearby: %q", got)
	}
	// About 22 km north, still inside the four-digit shorten range.
	town := []locationPlace{{Name: "Hemmingsmark", Lat: lat + 0.2, Lng: lng}}
	if got := coordinateLocationName(lat, lng, town); got != "83H9+PCG Hemmingsmark" {
		t.Fatalf("town: %q", got)
	}
	far := []locationPlace{{Name: "Hemmingsmark", Lat: lat + 2, Lng: lng}}
	if got := coordinateLocationName(lat, lng, far); got != "9FFW83H9+PCG" {
		t.Fatalf("far: %q", got)
	}
	// The nearer site wins. A region and a rack location are not places.
	places := locationPlaces([]models.Site{
		{Name: "Sweden", NetboxKind: models.SiteNetboxKindRegion, Latitude: lat, Longitude: lng},
		{Name: "MDF", NetboxKind: models.SiteNetboxKindLocation, Latitude: lat, Longitude: lng},
		{Name: "Default", NetboxKind: models.SiteNetboxKindSite, Latitude: lat, Longitude: lng},
		{Name: "Piteå", NetboxKind: models.SiteNetboxKindSite, Latitude: lat + 0.2, Longitude: lng},
		{Name: "Hemmingsmark", NetboxKind: models.SiteNetboxKindSite, Latitude: lat, Longitude: lng},
	})
	if got := coordinateLocationName(lat, lng, places); got != "+PCG Hemmingsmark" {
		t.Fatalf("nearest site: %q", got)
	}
	d := &models.Device{Latitude: f64(lat), Longitude: f64(lng)}
	nms := &LibrenmsDevice{Location: "+PCG Hemmingsmark"}
	if got := librenmsLocationName(d, nms, here); got != "+PCG Hemmingsmark" {
		t.Fatalf("kept short code: %q", got)
	}
	if got := librenmsLocationName(d, nms, near); got != "H9+PCG Hemmingsmark" {
		t.Fatalf("moved short code: %q", got)
	}
}

func TestLocationSetCoordinates(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE locations (
		id integer primary key,
		location text,
		lat real,
		lng real,
		fixed_coordinates integer
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO locations (id, location, lat, lng, fixed_coordinates) VALUES
		(1, 'STO', NULL, NULL, 0),
		(2, 'GOT', 57.7, 11.9, 1)`); err != nil {
		t.Fatal(err)
	}
	client := &LibrenmsClient{DB: db}

	wrote, err := client.LocationSetCoordinates("STO", 59.329324, 18.068581)
	if err != nil || !wrote {
		t.Fatalf("write STO: wrote=%v err=%v", wrote, err)
	}
	var lat, lng float64
	var fixed int
	if err := db.QueryRow(`SELECT lat, lng, fixed_coordinates FROM locations WHERE location = 'STO'`).Scan(&lat, &lng, &fixed); err != nil {
		t.Fatal(err)
	}
	if !coordsClose(lat, lng, 59.329324, 18.068581) || fixed != 1 {
		t.Fatalf("STO stored %v,%v fixed=%d", lat, lng, fixed)
	}

	wrote, err = client.LocationSetCoordinates("STO", 59.329324, 18.068581)
	if err != nil || wrote {
		t.Fatalf("unchanged STO: wrote=%v err=%v", wrote, err)
	}

	wrote, err = client.LocationSetCoordinates("GOT", 57.7, 11.9)
	if err != nil || wrote {
		t.Fatalf("already fixed GOT: wrote=%v err=%v", wrote, err)
	}

	if _, err := client.LocationSetCoordinates("missing", 1, 2); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}
}

func TestSyncDeviceCoordinates(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE locations (
		id integer primary key,
		location text,
		lat real,
		lng real,
		fixed_coordinates integer
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE devices (
		device_id integer primary key,
		location_id integer,
		override_sysLocation integer
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO locations (id, location, lat, lng, fixed_coordinates) VALUES
		(1, 'STO', 0, 0, 0),
		(2, 'LULEA', 65, 22, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO devices (device_id, location_id, override_sysLocation) VALUES (6, NULL, 0)`); err != nil {
		t.Fatal(err)
	}

	nms := &LibrenmsDevice{DeviceID: 1, Hostname: "10.0.0.1", Display: "r1", Location: "old", Lat: f32(0), Lng: f32(0)}
	other := &LibrenmsDevice{DeviceID: 4, Hostname: "10.0.0.4", Display: "r4", Location: "old", Lat: f32(59.4), Lng: f32(18.1)}
	same := &LibrenmsDevice{DeviceID: 5, Hostname: "10.0.0.5", Display: "r5", Location: "LULEA", Lat: f32(65), Lng: f32(22)}
	nowhere := &LibrenmsDevice{DeviceID: 6, Hostname: "10.0.0.6", Display: "r6"}
	factum := []*models.Device{
		{LibrenmsID: 1, CfLocation: "STO", Latitude: f64(59.3), Longitude: f64(18)},
		{LibrenmsID: 4, CfLocation: "STO", Latitude: f64(59.4), Longitude: f64(18.1)},
		{LibrenmsID: 5, Latitude: f64(65), Longitude: f64(22)},
		{LibrenmsID: 6, Latitude: f64(1), Longitude: f64(2)},
	}
	fl := &FactumLibrenmsClient{Librenms: &LibrenmsClient{DB: db}, FactumDevices: factum}
	plan := planLocationCoordinates([]*LibrenmsDevice{other, nms, same, nowhere}, nil, fl.getFactumDeviceForLibrenmsDevice, nil)

	var buf bytes.Buffer
	rep := jobevent.NewConsoleReporter(&buf)
	wrote, err := fl.syncDeviceCoordinates(other, factum[1], plan, rep)
	if err != nil || wrote {
		t.Fatalf("loser wrote=%v err=%v", wrote, err)
	}
	wrote, err = fl.syncDeviceCoordinates(nms, factum[0], plan, rep)
	if err != nil || !wrote {
		t.Fatalf("winner wrote=%v err=%v", wrote, err)
	}
	if !strings.Contains(buf.String(), "different coordinates") || !strings.Contains(buf.String(), "10.0.0.1") {
		t.Fatalf("log = %q", buf.String())
	}
	var lat float64
	var fixed int
	if err := db.QueryRow(`SELECT lat, fixed_coordinates FROM locations WHERE location = 'STO'`).Scan(&lat, &fixed); err != nil {
		t.Fatal(err)
	}
	if !coordsClose(lat, 0, 59.3, 0) || fixed != 1 {
		t.Fatalf("STO lat=%v fixed=%d", lat, fixed)
	}

	wrote, err = fl.syncDeviceCoordinates(same, factum[2], plan, rep)
	if err != nil || wrote {
		t.Fatalf("matching location wrote=%v err=%v", wrote, err)
	}
	wrote, err = fl.syncDeviceCoordinates(nowhere, factum[3], plan, rep)
	if err != nil || !wrote {
		t.Fatalf("no location wrote=%v err=%v", wrote, err)
	}
	dummyName := coordinateLocationName(1, 2, nil)
	if !strings.Contains(buf.String(), dummyName) {
		t.Fatalf("log = %q", buf.String())
	}
	var dummyLat, dummyLng float64
	if err := db.QueryRow(`SELECT lat, lng, fixed_coordinates FROM locations WHERE location = ?`, dummyName).Scan(&dummyLat, &dummyLng, &fixed); err != nil {
		t.Fatal(err)
	}
	if !coordsClose(dummyLat, dummyLng, 1, 2) || fixed != 1 {
		t.Fatalf("dummy lat=%v lng=%v fixed=%d", dummyLat, dummyLng, fixed)
	}
	var locationID, override int
	if err := db.QueryRow(`SELECT location_id, override_sysLocation FROM devices WHERE device_id = 6`).Scan(&locationID, &override); err != nil {
		t.Fatal(err)
	}
	if locationID == 0 || override != 1 {
		t.Fatalf("device location_id=%d override=%d", locationID, override)
	}
	wrote, err = fl.syncDeviceCoordinates(nowhere, factum[3], plan, rep)
	if err != nil || wrote {
		t.Fatalf("dummy unchanged wrote=%v err=%v", wrote, err)
	}
}
