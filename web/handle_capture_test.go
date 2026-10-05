package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/models"
)

func TestApiCaptureRejectsFilter(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodPost, "/api/capture", captureStartRequest{
		DeviceID: 1, InterfaceID: 2, Filter: "host 192.0.2.1; reboot",
	}, nil, nil)
	if err := ctrl.ApiCapture(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestApiCaptureRejectsUnsupportedPlatform(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "lab-sw", Platform: "sros"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "1/1/1"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{DB: db}
	called := false
	ctrl.driverFn = func(*models.Device, deviceCredentialsRequest, *models.Settings) (drivers.DriverClient, error) {
		called = true
		return nil, nil
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/capture", captureStartRequest{
		DeviceID: dev.ID, InterfaceID: iface.ID,
	}, nil, nil)
	if err := ctrl.ApiCapture(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("driver was built for an unsupported platform")
	}
}

type captureDriverStub struct {
	elinePackStub
	packets  []byte
	tornDown bool
}

func (s *captureDriverStub) SetupPortMirror(context.Context, drivers.PortMirrorRequest) (*drivers.PortMirrorSession, error) {
	return &drivers.PortMirrorSession{
		Name:      "factum2",
		Interface: "mirror0",
		Packets:   io.NopCloser(bytes.NewReader(s.packets)),
	}, nil
}

func (s *captureDriverStub) TeardownPortMirror(context.Context, *drivers.PortMirrorSession) error {
	s.tornDown = true
	return nil
}

func TestApiCaptureStreamsPcapAndTearsDown(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{
		Name: "default", Username: "sync", Password: "secret",
	}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "lab-eos.example", Platform: "EOS"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	iface := models.Interface{DeviceID: dev.ID, Name: "Ethernet1"}
	if err := db.Create(&iface).Error; err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header, 0xa1b2c3d4)
	stub := &captureDriverStub{packets: append(header, 0x01, 0x02, 0x03, 0x04)}
	ctrl := &Controller{DB: db}
	ctrl.driverFn = func(*models.Device, deviceCredentialsRequest, *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	c, rec := jsonRequest(t, http.MethodPost, "/api/capture", captureStartRequest{
		DeviceID: dev.ID, InterfaceID: iface.ID, Filter: "udp port 4789", MaxPackets: 10, MaxSeconds: 30,
	}, nil, nil)
	if err := ctrl.ApiCapture(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), stub.packets) {
		t.Fatalf("body = %d bytes, want %d", rec.Body.Len(), len(stub.packets))
	}
	if !stub.tornDown {
		t.Fatal("mirror was not torn down")
	}
}

func TestApiCapturePlatforms(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/capture/platforms", nil, nil, nil)
	if err := ctrl.ApiCapturePlatforms(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"eos"`)) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandleWiregasm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wiregasm.js"), []byte("/* wiregasm */"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{WiregasmDir: dir}
	c, rec := jsonRequest(t, http.MethodGet, "/wiregasm/wiregasm.js", nil, []string{"name"}, []string{"wiregasm.js"})
	if err := ctrl.handleWiregasm(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("wiregasm")) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	c, rec = jsonRequest(t, http.MethodGet, "/wiregasm/other.js", nil, []string{"name"}, []string{"other.js"})
	if err := ctrl.handleWiregasm(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
