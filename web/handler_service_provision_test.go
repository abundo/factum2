package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

type sessionStub struct {
	elinePackStub
	mu       sync.Mutex
	sessions [][]string
	applyErr error
}

func (s *sessionStub) ApplyCLISession(_ string, cmds []string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = true
	cp := make([]string, len(cmds))
	copy(cp, cmds)
	s.sessions = append(s.sessions, cp)
	return s.applyErr
}

func seedDeviceSyncAuth(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "u", Password: "p",
	}).Error; err != nil {
		t.Fatal(err)
	}
}

type fakeServiceNetbox struct {
	mu           sync.Mutex
	next         uint
	ifaces       map[string]*netboxtool.NetboxInterfaceREST
	l2vpns       map[string]*netboxtool.NBL2VPN
	l2vpnByID    map[uint]*netboxtool.NBL2VPN
	vrfs         map[string]*netboxtool.NBVRF
	deletedL2    []uint
	deletedVRF   []uint
	deletedTerm  []uint
	deletedIface []int
}

func (f *fakeServiceNetbox) alloc() uint {
	f.next++
	return f.next
}

func (f *fakeServiceNetbox) CreateInterfaceWithOptions(_ uint, name string, _ map[string]any) (*netboxtool.NetboxInterfaceREST, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ifaces == nil {
		f.ifaces = map[string]*netboxtool.NetboxInterfaceREST{}
	}
	row := &netboxtool.NetboxInterfaceREST{ID: f.alloc(), Name: name}
	f.ifaces[name] = row
	return row, nil
}

func (f *fakeServiceNetbox) InterfaceDelete(id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedIface = append(f.deletedIface, id)
	return nil
}

func (f *fakeServiceNetbox) GetL2VPNByName(name string) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.l2vpns[name], nil
}

func (f *fakeServiceNetbox) GetL2VPNByIdentifier(identifier int) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.l2vpns {
		if l != nil && l.Identifier == identifier {
			return l, nil
		}
	}
	return nil, nil
}

func (f *fakeServiceNetbox) CreateL2VPN(name, _, l2vpnType string, identifier int) (*netboxtool.NBL2VPN, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.l2vpns == nil {
		f.l2vpns = map[string]*netboxtool.NBL2VPN{}
	}
	if f.l2vpnByID == nil {
		f.l2vpnByID = map[uint]*netboxtool.NBL2VPN{}
	}
	row := &netboxtool.NBL2VPN{NetboxID: f.alloc(), Name: name, Type: l2vpnType, Identifier: identifier}
	f.l2vpns[name] = row
	f.l2vpnByID[row.NetboxID] = row
	return row, nil
}

func (f *fakeServiceNetbox) UpdateL2VPN(id uint, changes map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.l2vpnByID[id]
	if row == nil {
		return nil
	}
	if v, ok := changes["identifier"].(int); ok {
		row.Identifier = v
	}
	return nil
}

func (f *fakeServiceNetbox) DeleteL2VPN(id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedL2 = append(f.deletedL2, id)
	return nil
}

func (f *fakeServiceNetbox) CreateL2VPNTermination(l2vpnID, interfaceID uint) (*netboxtool.NBL2VPNTermination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &netboxtool.NBL2VPNTermination{NetboxID: f.alloc(), L2VPNID: l2vpnID, InterfaceID: interfaceID}, nil
}

func (f *fakeServiceNetbox) DeleteL2VPNTermination(id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedTerm = append(f.deletedTerm, id)
	return nil
}

func (f *fakeServiceNetbox) GetVRFByName(name string) (*netboxtool.NBVRF, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vrfs[name], nil
}

func (f *fakeServiceNetbox) CreateVRF(name, rd, description string) (*netboxtool.NBVRF, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vrfs == nil {
		f.vrfs = map[string]*netboxtool.NBVRF{}
	}
	row := &netboxtool.NBVRF{NetboxID: f.alloc(), Name: name, RD: rd, Description: description}
	f.vrfs[name] = row
	return row, nil
}

func (f *fakeServiceNetbox) DeleteVRF(id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedVRF = append(f.deletedVRF, id)
	return nil
}

func enableTestNetbox(t *testing.T, db *gorm.DB) {
	t.Helper()
	s, err := util.GetOrCreateSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	s.NetboxEnabled = &on
	s.NetboxApiURL = "http://netbox.test"
	s.NetboxApiToken = "token"
	if err := db.Save(s).Error; err != nil {
		t.Fatal(err)
	}
}

func createTestELANType(t *testing.T, db *gorm.DB) models.ServiceType {
	t.Helper()
	st := models.ServiceType{
		Name: "ELAN",
		Interfaces: models.ServiceInterfacesSpec{
			Min: 0, Max: 0,
			Fields: []models.FieldSchema{{Name: "vlan", Type: models.VarTypeVLAN, Required: true}},
		},
		SyncSource: models.SyncSourceELAN,
		NetboxType: models.NetboxTypeVPLS,
	}
	if err := db.Create(&st).Error; err != nil {
		t.Fatal(err)
	}
	return st
}

func seedTwoPEs(t *testing.T, db *gorm.DB, aName, bName string) (models.Device, models.Device, models.Interface, models.Interface) {
	t.Helper()
	devA := models.Device{Name: aName, Platform: "eos", NetboxID: 501}
	devB := models.Device{Name: bName, Platform: "eos", NetboxID: 502}
	if err := db.Create(&devA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&devB).Error; err != nil {
		t.Fatal(err)
	}
	ifa := models.Interface{DeviceID: devA.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 601}
	ifb := models.Interface{DeviceID: devB.ID, Name: "Ethernet1", Type: "1000base-t", NetboxID: 602}
	if err := db.Create(&ifa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}
	return devA, devB, ifa, ifb
}

func TestApiServiceDeleteRefusesLime(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	svc := models.Service{ServiceID: "CN00010", Source: "lime", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	services := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)
	c, rec := jsonRequest(t, http.MethodDelete, "/api/service/x", map[string]any{
		"remove_from_device": true,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceDelete(services)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	var still models.Service
	if err := db.First(&still, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestApiServiceUnrealizeKeepsLimeRow(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	svc := models.Service{
		CustomerID: cust.ID, ServiceID: "CN00011", Name: "lime-cn", Source: "lime",
		ServiceType: st.Name, Comment: "keep me", Product: "capacity",
		PseudowireID: 99, L2VPNNetboxID: 7, BandwidthMbps: 1000, MaxMacAddresses: 50,
		Fields: json.RawMessage(`{"vlan":1}`),
	}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	devA, _, ifa, _ := seedTwoPEs(t, db, "pe-u1", "pe-u2")
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID, Fields: cfgmgmt.EncodeEndpointFields(10, 0, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	root, err := cfgmgmt.RootScope(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfgmgmt.AttachService(db, root.ID, svc.ID); err != nil {
		t.Fatal(err)
	}

	c, rec := jsonRequest(t, http.MethodPost, "/api/service/x/unrealize", map[string]any{}, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceUnrealize(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got models.Service
	if err := db.First(&got, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Source != "lime" || got.ServiceID != "CN00011" || got.Name != "lime-cn" || got.Comment != "keep me" {
		t.Fatalf("commercial columns changed: %+v", got)
	}
	if got.ServiceType != "" || got.PseudowireID != 0 || got.L2VPNNetboxID != 0 {
		t.Fatalf("cfgmgmt columns still set: type=%q pw=%d l2=%d", got.ServiceType, got.PseudowireID, got.L2VPNNetboxID)
	}
	if got.BandwidthMbps != 0 || got.MaxMacAddresses != 0 {
		t.Fatalf("list-view copies still set: bw=%d mac=%d", got.BandwidthMbps, got.MaxMacAddresses)
	}
	if len(got.Fields) > 0 && string(got.Fields) != "{}" && string(got.Fields) != "null" {
		t.Fatalf("fields still set: %s", got.Fields)
	}
	eps, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 0 {
		t.Fatalf("endpoints leftover: %d", len(eps))
	}
	var n int64
	if err := db.Model(&models.ConfigScope{}).Where("kind = ? AND service_id = ?", models.ConfigScopeKindService, svc.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("canonical node still attached: %d", n)
	}
}

func TestApiServiceDeleteCleansNonELINEType(t *testing.T) {
	db := newTestDB(t)
	seedDeviceSyncAuth(t, db)
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	createTestTranslationCLI(t, db, st, "eos", "add {{.Name}}")
	feat := models.ConfigCLIFeature{}
	if err := db.Where("name = ?", "apply").First(&feat).Error; err == nil {
		db.Model(&feat).Update("remove_commands", "remove {{.Name}}")
	}
	stub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	svc := models.Service{ServiceID: "CN00012", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	devA, _, ifa, _ := seedTwoPEs(t, db, "pe-d1", "pe-d2")
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	services := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)
	c, rec := jsonRequest(t, http.MethodDelete, "/api/service/x", map[string]any{
		"remove_from_device": true,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceDelete(services)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !stub.applied {
		t.Fatal("expected CLI remove on delete of non-ELINE type")
	}
	if err := db.First(&models.Service{}, svc.ID).Error; err == nil {
		t.Fatal("service row still present")
	}
}

func TestApiServiceEndpointsPutAssignsPseudowireOnlyForEVPL(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	nb := &fakeServiceNetbox{}
	ctrl.netboxFn = func(_ *models.Settings) (serviceNetboxAPI, error) { return nb, nil }
	enableTestNetbox(t, db)

	elan := createTestELANType(t, db)
	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-pw1", "pe-pw2")
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00013", ServiceType: elan.Name}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devA.ID, "interface_id": ifa.ID, "fields": map[string]any{"vlan": 10}},
			{"device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 20}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("elan put status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&svc, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if svc.PseudowireID != 0 {
		t.Fatalf("vpls assigned pseudowire_id=%d", svc.PseudowireID)
	}
	if svc.L2VPNNetboxID == 0 {
		t.Fatal("expected vpls l2vpn id")
	}

	eline := createTestELINEType(t, db)
	svc2 := models.Service{CustomerID: cust.ID, ServiceID: "CN00014", ServiceType: eline.Name}
	if err := db.Create(&svc2).Error; err != nil {
		t.Fatal(err)
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc2.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("evpl put status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&svc2, svc2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if svc2.PseudowireID == 0 {
		t.Fatal("evpl did not assign pseudowire_id")
	}
}

func TestApiServiceEndpointsPutDoesNotAssignPseudowireWithoutNetbox(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	createTestELINEType(t, db)
	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-off1", "pe-off2")
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00015", ServiceType: "ELINE"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devA.ID, "interface_id": ifa.ID, "fields": map[string]any{"vlan": 10}},
			{"device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 20}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := db.First(&svc, svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if svc.PseudowireID != 0 || svc.L2VPNNetboxID != 0 {
		t.Fatalf("assigned netbox state without integration: pw=%d l2=%d", svc.PseudowireID, svc.L2VPNNetboxID)
	}
}

func TestApiServicePushStampsApplied(t *testing.T) {
	db := newTestDB(t)
	seedDeviceSyncAuth(t, db)
	ctrl := &Controller{DB: db}
	st := createTestELINEType(t, db)
	createTestTranslationCLI(t, db, st, "eos", "add {{.Name}}")
	stub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	cust := models.Customer{Name: "Acme"}
	if err := db.Create(&cust).Error; err != nil {
		t.Fatal(err)
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-st1", "pe-st2")
	svc := models.Service{CustomerID: cust.ID, ServiceID: "CN00016", ServiceType: "ELINE"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID, Fields: cfgmgmt.EncodeEndpointFields(10, 0, 0)},
		{Role: models.EndpointRoleInterface, DeviceID: devB.ID, InterfaceID: ifb.ID, Fields: cfgmgmt.EncodeEndpointFields(20, 0, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/service/x/push", map[string]any{}, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServicePush(c); err != nil {
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
		t.Fatalf("endpoints = %d", len(eps))
	}
	for _, ep := range eps {
		if ep.AppliedDeviceID != ep.DeviceID || ep.AppliedIface != "Ethernet1" || ep.AppliedPlatform != "eos" {
			t.Fatalf("applied not stamped: %+v", ep)
		}
	}
}

func TestApiServiceEndpointsPutTeardownOnRebind(t *testing.T) {
	db := newTestDB(t)
	seedDeviceSyncAuth(t, db)
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	parent, err := cfgmgmt.CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := cfgmgmt.CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	feat := models.ConfigCLIFeature{ScopeID: cli.ID, Name: "apply", AddCommands: "add {{.Name}}", RemoveCommands: "remove {{.Name}}"}
	if err := db.Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	stub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-rb1", "pe-rb2")
	svc := models.Service{ServiceID: "CN00017", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return &sessionStub{applyErr: errTeardown}, nil
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 10}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("teardown fail status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
	eps, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].DeviceID != devA.ID {
		t.Fatalf("replace ran despite teardown failure: %+v", eps)
	}

	okStub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return okStub, nil
	}
	c, rec = jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("rebind status = %d, body=%s", rec.Code, rec.Body.String())
	}
	eps, err = cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].DeviceID != devB.ID {
		t.Fatalf("after rebind: %+v", eps)
	}
	if eps[0].AppliedDeviceID != devB.ID || eps[0].AppliedPlatform != "eos" {
		t.Fatalf("applied not stamped on new PE: %+v", eps[0])
	}
	if len(okStub.sessions) < 2 {
		t.Fatalf("want remove then add sessions, got %d", len(okStub.sessions))
	}
	joined := strings.Join(okStub.sessions[0], "\n")
	if !strings.Contains(joined, "remove") {
		t.Fatalf("first session not remove: %q", joined)
	}
}

func TestApiServiceEndpointsPutRebindUsesDeviceSyncAuth(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "sync-user", Password: "sync-pass",
	}).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	parent, err := cfgmgmt.CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := cfgmgmt.CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	feat := models.ConfigCLIFeature{ScopeID: cli.ID, Name: "apply", AddCommands: "add {{.Name}}", RemoveCommands: "remove {{.Name}}"}
	if err := db.Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	var gotCreds []deviceCredentialsRequest
	ctrl.driverFn = func(_ *models.Device, creds deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		gotCreds = append(gotCreds, creds)
		return &sessionStub{}, nil
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-auth1", "pe-auth2")
	svc := models.Service{ServiceID: "CN00019", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 10}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(gotCreds) == 0 {
		t.Fatal("driver was not opened")
	}
	for _, creds := range gotCreds {
		if creds.Username != "sync-user" || creds.Password != "sync-pass" {
			t.Fatalf("creds = %+v, want DeviceSyncAuth default", creds)
		}
	}
}

func TestApiServiceEndpointsPutRebindRequiresDeviceSyncAuth(t *testing.T) {
	db := newTestDB(t)
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	parent, err := cfgmgmt.CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := cfgmgmt.CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	feat := models.ConfigCLIFeature{ScopeID: cli.ID, Name: "apply", AddCommands: "add {{.Name}}", RemoveCommands: "remove {{.Name}}"}
	if err := db.Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-noauth1", "pe-noauth2")
	svc := models.Service{ServiceID: "CN00020", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devB.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 10}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no device-sync credentials") {
		t.Fatalf("body = %s, want stored-auth error", rec.Body.String())
	}
}

func TestApiServiceEndpointsPutTeardownSameDeviceIface(t *testing.T) {
	db := newTestDB(t)
	seedDeviceSyncAuth(t, db)
	ctrl := &Controller{DB: db}
	st := createTestELANType(t, db)
	parent, err := cfgmgmt.CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := cfgmgmt.CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	feat := models.ConfigCLIFeature{ScopeID: cli.ID, Name: "apply", AddCommands: "add {{.LocalIface}}", RemoveCommands: "remove {{.LocalIface}}"}
	if err := db.Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	devA, _, ifa, _ := seedTwoPEs(t, db, "pe-same1", "pe-same2")
	ifb := models.Interface{DeviceID: devA.ID, Name: "Ethernet2", Type: "1000base-t", NetboxID: 603}
	if err := db.Create(&ifb).Error; err != nil {
		t.Fatal(err)
	}
	svc := models.Service{ServiceID: "CN00018", ServiceType: "ELAN"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	okStub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return okStub, nil
	}
	body := map[string]any{
		"endpoints": []map[string]any{
			{"device_id": devA.ID, "interface_id": ifb.ID, "fields": map[string]any{"vlan": 10}},
		},
	}
	c, rec := jsonRequest(t, http.MethodPut, "/api/service/x/endpoints", body, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceEndpointsPut(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("same-device rebind status = %d, body=%s", rec.Code, rec.Body.String())
	}
	eps, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].InterfaceID != ifb.ID {
		t.Fatalf("after same-device rebind: %+v", eps)
	}
	if eps[0].AppliedDeviceID != devA.ID || eps[0].AppliedIface != "Ethernet2" {
		t.Fatalf("applied not stamped on new iface: %+v", eps[0])
	}
	if len(okStub.sessions) < 2 {
		t.Fatalf("want remove then add, got %d sessions", len(okStub.sessions))
	}
	if joined := strings.Join(okStub.sessions[0], "\n"); !strings.Contains(joined, "Ethernet1") {
		t.Fatalf("remove session missing old iface: %q", joined)
	}
	if joined := strings.Join(okStub.sessions[1], "\n"); !strings.Contains(joined, "Ethernet2") {
		t.Fatalf("add session missing new iface: %q", joined)
	}
}

func TestApiServiceDeleteTeardownRendersOthers(t *testing.T) {
	db := newTestDB(t)
	seedDeviceSyncAuth(t, db)
	ctrl := &Controller{DB: db}
	st := createTestELINEType(t, db)
	parent, err := cfgmgmt.CatalogCLITypeFolder(db, st.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ID
	cli, err := cfgmgmt.CreateScope(db, &models.ConfigScope{
		ParentID: &parent.ID, Name: "eos", Kind: models.ConfigScopeKindCLI,
		ServiceTypeID: &id, Platform: "eos", PayloadKind: models.PayloadKindCLI, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	feat := models.ConfigCLIFeature{
		ScopeID: cli.ID, Name: "apply", AddCommands: "add {{.Name}}",
		RemoveCommands: "remove {{.Name}} {{ (index .Others 0).LocalIface }}",
	}
	if err := db.Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	stub := &sessionStub{}
	ctrl.driverFn = func(_ *models.Device, _ deviceCredentialsRequest, _ *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	devA, devB, ifa, ifb := seedTwoPEs(t, db, "pe-oth1", "pe-oth2")
	svc := models.Service{ServiceID: "CN00019", ServiceType: "ELINE"}
	if err := db.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, []models.ServiceEndpoint{
		{
			Role: models.EndpointRoleInterface, DeviceID: devA.ID, InterfaceID: ifa.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(10, 0, 0),
			AppliedDeviceID: devA.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(10, 0, 0),
		},
		{
			Role: models.EndpointRoleInterface, DeviceID: devB.ID, InterfaceID: ifb.ID,
			Fields:          cfgmgmt.EncodeEndpointFields(20, 0, 0),
			AppliedDeviceID: devB.ID, AppliedIface: "Ethernet1", AppliedPlatform: "eos",
			AppliedFields: cfgmgmt.EncodeEndpointFields(20, 0, 0),
		},
	}); err != nil {
		t.Fatal(err)
	}
	services := NewSecureCRUDHandler[models.Service, models.ServiceDTO](db)
	c, rec := jsonRequest(t, http.MethodDelete, "/api/service/x", map[string]any{
		"remove_from_device": true,
	}, []string{"id"}, []string{strconv.FormatUint(uint64(svc.ID), 10)})
	if err := ctrl.ApiServiceDelete(services)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !stub.applied {
		t.Fatal("expected CLI remove")
	}
	foundPeer := false
	for _, sess := range stub.sessions {
		if strings.Contains(strings.Join(sess, "\n"), "Ethernet1") {
			foundPeer = true
		}
	}
	if !foundPeer {
		t.Fatalf("remove cmds missing peer LocalIface: %+v", stub.sessions)
	}
}

var errTeardown = &elineHTTPError{status: http.StatusBadGateway, msg: "teardown failed"}
