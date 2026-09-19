package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/abundo/factum2/internal/dcim"
	"github.com/abundo/factum2/models"
)

func TestDCIMRackCRUDAndPlacement(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	site := models.Site{Name: "Hall", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	mfr := models.Manufacturer{Name: "Acme", Slug: "acme"}
	if err := db.Create(&mfr).Error; err != nil {
		t.Fatal(err)
	}
	h := 2
	full := true
	dt := models.DeviceType{ManufacturerID: mfr.ID, Model: "SW", Slug: "sw", HeightTicks: &h, FullDepth: &full}
	if err := db.Create(&dt).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "sw1", DeviceTypeID: dt.ID, SiteID: site.ID, CfSource: "factum"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/racks", dcim.RackWrite{
		SiteID: site.ID, Name: "R1", HeightU: 42,
	}, nil, nil)
	if err := ctrl.ApiDCIMRackCreate(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var rack models.Rack
	if err := json.Unmarshal(rec.Body.Bytes(), &rack); err != nil {
		t.Fatal(err)
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/dcim/racks?site_id="+strconv.FormatUint(uint64(site.ID), 10), nil, nil, nil)
	if err := ctrl.ApiDCIMRacksList(c); err != nil {
		t.Fatal(err)
	}
	var list []dcim.RackSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "R1" {
		t.Fatalf("list %+v", list)
	}

	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/devices/x/placement", placementWrite{
		RackID: rack.ID, OffsetTicks: 0, Face: "front",
	}, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiDCIMDevicePlace(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("place %d %s", rec.Code, rec.Body.String())
	}

	c, rec = jsonRequest(t, http.MethodGet, "/api/dcim/racks/x/elevation", nil, []string{"id"}, []string{strconv.FormatUint(uint64(rack.ID), 10)})
	if err := ctrl.ApiDCIMRackElevation(c); err != nil {
		t.Fatal(err)
	}
	var elev dcim.ElevationDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &elev); err != nil {
		t.Fatal(err)
	}
	if len(elev.Placements) != 1 || elev.Placements[0].DeviceName != "sw1" {
		t.Fatalf("elev %+v", elev)
	}

	imported := models.Device{Name: "nb-sw", NetboxID: 9, DeviceTypeID: dt.ID, SiteID: 77, CfSource: "netbox"}
	if err := db.Create(&imported).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DevicePlacement{
		DeviceID: imported.ID, RackID: rack.ID, OffsetTicks: 10, Face: "front",
		Source: models.DCIMSourceNetbox, Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/devices/x/placement", placementWrite{
		RackID: rack.ID, OffsetTicks: 12, Face: "front", Version: 1,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(imported.ID), 10)})
	if err := ctrl.ApiDCIMDevicePlace(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("imported place %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["reason"] != dcim.ReasonImportedReadOnly {
		t.Fatalf("reason %+v", body)
	}
}

func TestDCIMFloorPlanSaveAndGraph(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	site := models.Site{Name: "Hall", Source: models.SiteSourceFactum}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	rack := models.Rack{SiteID: site.ID, Name: "R1", HeightU: 42, WidthMM: 600, DepthMM: 1000, Source: models.DCIMSourceFactum}
	if err := db.Create(&rack).Error; err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/floor-plans", floorPlanCreate{
		SiteID: site.ID, Name: "Room A", WidthMM: 20000, HeightMM: 15000, GridMM: 600,
	}, nil, nil)
	if err := ctrl.ApiDCIMFloorPlanCreate(c); err != nil {
		t.Fatal(err)
	}
	var plan models.FloorPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/floor-plans/x/layout", dcim.FloorLayoutWrite{
		Revision: 1,
		Racks:    []dcim.FloorPlanRackDTO{{RackID: rack.ID, XMM: 600, YMM: 600, Rotation: 0}},
	}, []string{"id"}, []string{strconv.FormatUint(uint64(plan.ID), 10)})
	if err := ctrl.ApiDCIMFloorPlanLayout(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("layout %d %s", rec.Code, rec.Body.String())
	}

	devA := models.Device{Name: "a", SiteID: site.ID}
	devB := models.Device{Name: "b", SiteID: site.ID}
	db.Create(&devA)
	db.Create(&devB)
	ia := models.Interface{DeviceID: devA.ID, Name: "Eth1"}
	ib := models.Interface{DeviceID: devB.ID, Name: "Eth2"}
	db.Create(&ia)
	db.Create(&ib)
	db.Create(&models.Connection{DeviceAID: devA.ID, InterfaceAID: ia.ID, DeviceBID: devB.ID, InterfaceBID: ib.ID})

	c, rec = jsonRequest(t, http.MethodGet, "/api/dcim/connections/graph?site_id="+strconv.FormatUint(uint64(site.ID), 10), nil, nil, nil)
	if err := ctrl.ApiDCIMConnectionGraph(c); err != nil {
		t.Fatal(err)
	}
	var g dcim.GraphDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) < 2 || len(g.Edges) != 1 {
		t.Fatalf("graph %+v", g)
	}

	user := models.User{Username: "ada"}
	db.Create(&user)
	c, rec = jsonRequest(t, http.MethodPut, "/api/dcim/connection-view-layouts/x", dcim.LayoutDTO{
		Revision: 1, Nodes: map[string]dcim.XY{"1": {X: 10, Y: 20}},
	}, []string{"scope"}, []string{"site:" + strconv.FormatUint(uint64(site.ID), 10)})
	c.Set("user", user)
	if err := ctrl.ApiDCIMConnectionLayoutPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("layout put %d %s", rec.Code, rec.Body.String())
	}
}
