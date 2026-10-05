package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/models"
	"github.com/labstack/echo/v5"
)

type runningConfigStub struct {
	elinePackStub
	text      string
	enter     []string
	cmds      []string
	comment   string
	commitErr error
	reads     int
}

func (s *runningConfigStub) RunningConfigGet(bool) (*drivers.RunningConfigModel, error) {
	s.reads++
	return &drivers.RunningConfigModel{ConfigStr: s.text}, nil
}

func (s *runningConfigStub) CommitRunningContext(enter []string, commands []string, comment string) error {
	s.enter = append([]string{}, enter...)
	s.cmds = append([]string{}, commands...)
	s.comment = comment
	if s.commitErr != nil {
		return s.commitErr
	}
	s.text = strings.Replace(s.text, "description foo", "description bar", 1)
	return nil
}

func TestRunningConfigPlatformRoute(t *testing.T) {
	e := echo.New()
	ctrl := &Controller{}
	e.GET("/dcim/running-config/platforms", ctrl.ApiRunningConfigPlatforms)
	e.GET("/dcim/running-config/:id", func(c *echo.Context) error {
		return c.String(http.StatusOK, "id")
	})
	req := httptest.NewRequest(http.MethodGet, "/dcim/running-config/platforms", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"eos"`) {
		t.Fatalf("platforms route status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestApiRunningConfigPlatforms(t *testing.T) {
	ctrl := &Controller{}
	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/running-config/platforms", nil, nil, nil)
	if err := ctrl.ApiRunningConfigPlatforms(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"eos"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestApiRunningConfigGet(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{Name: "default", Username: "sync", Password: "secret"}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "lab-eos.example", Platform: "EOS"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	stub := &runningConfigStub{text: sampleRunningConfig}
	ctrl := &Controller{DB: db}
	ctrl.driverFn = func(*models.Device, deviceCredentialsRequest, *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/running-config/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiRunningConfigGet(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"bgp/6782/af/ipv4"`) || !strings.Contains(rec.Body.String(), "aaa authentication") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestApiRunningConfigRejectsUnsupported(t *testing.T) {
	db := newTestDB(t)
	dev := models.Device{Name: "lab-sr", Platform: "sros"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	ctrl := &Controller{DB: db}
	ctrl.driverFn = func(*models.Device, deviceCredentialsRequest, *models.Settings) (drivers.DriverClient, error) {
		called = true
		return nil, nil
	}
	c, rec := jsonRequest(t, http.MethodGet, "/api/dcim/running-config/x", nil, []string{"id"}, []string{strconv.FormatUint(uint64(dev.ID), 10)})
	if err := ctrl.ApiRunningConfigGet(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("driver was built for an unsupported platform")
	}
}

func TestApiRunningConfigCommit(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&models.DeviceSyncAuth{Name: "default", Username: "sync", Password: "secret"}).Error; err != nil {
		t.Fatal(err)
	}
	dev := models.Device{Name: "lab-eos.example", Platform: "EOS"}
	if err := db.Create(&dev).Error; err != nil {
		t.Fatal(err)
	}
	stub := &runningConfigStub{text: sampleRunningConfig}
	ctrl := &Controller{DB: db}
	ctrl.driverFn = func(*models.Device, deviceCredentialsRequest, *models.Settings) (drivers.DriverClient, error) {
		return stub, nil
	}
	id := strconv.FormatUint(uint64(dev.ID), 10)

	tree, err := drivers.BuildRunningConfigTree("eos", sampleRunningConfig)
	if err != nil {
		t.Fatal(err)
	}
	eth := drivers.FindConfigContext(tree, "if/Ethernet1")
	body := strings.Replace(eth.Body, "description foo", "description bar", 1)
	c, rec := jsonRequest(t, http.MethodPost, "/api/dcim/running-config/x", runningConfigCommitRequest{
		ContextID: "if/Ethernet1",
		Body:      body,
		BaseBody:  eth.Body,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiRunningConfigCommit(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Join(stub.enter, "|") != "interface Ethernet1" {
		t.Fatalf("enter = %#v", stub.enter)
	}
	if len(stub.cmds) != 2 || stub.cmds[0] != "no description foo" || stub.cmds[1] != "description bar" {
		t.Fatalf("cmds = %#v", stub.cmds)
	}
	var resp struct {
		Diff     string                  `json:"diff"`
		Contexts []drivers.ConfigContext `json:"contexts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Diff, "- description foo") || !strings.Contains(resp.Diff, "+ description bar") {
		t.Fatalf("diff = %q", resp.Diff)
	}
	if drivers.FindConfigContext(resp.Contexts, "if/Ethernet1") == nil {
		t.Fatal("response tree missing Ethernet1")
	}

	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/running-config/x", runningConfigCommitRequest{
		ContextID: "if/Ethernet1",
		Body:      "description other\n",
		BaseBody:  "description stale\n",
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiRunningConfigCommit(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale status %d body %s", rec.Code, rec.Body.String())
	}

	stub.commitErr = errTestCommit
	c, rec = jsonRequest(t, http.MethodPost, "/api/dcim/running-config/x", runningConfigCommitRequest{
		ContextID: "if/Ethernet1",
		Body:      "description other\n",
		BaseBody:  drivers.FindConfigContext(resp.Contexts, "if/Ethernet1").Body,
	}, []string{"id"}, []string{id})
	if err := ctrl.ApiRunningConfigCommit(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "rejected") {
		t.Fatalf("error status %d body %s", rec.Code, rec.Body.String())
	}
}

var errTestCommit = errString("rejected by device")

type errString string

func (e errString) Error() string { return string(e) }

const sampleRunningConfig = `hostname lab
aaa authentication login default group radius local
interface Ethernet1
   description foo
   no shutdown
router bgp 6782
   router-id 1.1.1.1
   address-family ipv4
      network 10.0.0.0/8
router isis CORE
   net 49.0001.0000.0000.0001.00
end
`
