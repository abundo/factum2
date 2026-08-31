package prometheus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func testClient(dest string) *FactumPrometheusClient {
	cfg := &util.ConfigPrometheus{
		DestFile: dest,
		Module:   "if_mib",
		Auth:     "public_v2",
	}
	return &FactumPrometheusClient{
		PrometheusConfig: cfg,
		Prometheus:       NewPrometheusClient(*cfg),
	}
}

func TestFilterDevices(t *testing.T) {
	fpc := testClient("/tmp/snmp.json")
	fpc.PrometheusConfig.IgnoreDevices = "skip-me"
	fpc.PrometheusConfig.IgnorePlatforms = "windows"

	all := []*models.Device{
		{Name: "ok", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.1/24"},
		{Name: "disabled", Enabled: false, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.2/24"},
		{Name: "no-flag", Enabled: true, CfMonitorGrafana: false, PrimaryIPv4: "10.0.0.3/24"},
		{Name: "skip-me", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.4/24"},
		{Name: "win", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.5/24", Platform: "windows"},
		{Name: "no-ip", Enabled: true, CfMonitorGrafana: true},
	}
	got, counts := fpc.filterDevices(all)
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("devices = %v, want [ok]", names(got))
	}
	if counts.notEnabled != 1 || counts.notMonitorGrafana != 1 || counts.ignoreDevice != 1 ||
		counts.ignorePlatform != 1 || counts.noPrimaryIP != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestSaveTargetsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmp.json")
	fpc := testClient(path)

	devices := []*models.Device{
		{Name: "b-sw", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.2/24", Site: "dc1", Role: "access"},
		{Name: "a-sw", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.1/32"},
	}
	n, err := fpc.Prometheus.SaveTargets(path, devices)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []fileSDTarget
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Targets[0] != "10.0.0.1" || got[0].Labels["device"] != "a-sw" {
		t.Fatalf("first = %+v, want a-sw at 10.0.0.1 (sorted)", got[0])
	}
	if got[1].Labels["site"] != "dc1" || got[1].Labels["role"] != "access" {
		t.Fatalf("second labels = %+v", got[1].Labels)
	}
	if got[0].Labels["module"] != "if_mib" || got[0].Labels["auth"] != "public_v2" {
		t.Fatalf("defaults missing: %+v", got[0].Labels)
	}
}

func TestSaveTargetsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snmp.json")
	fpc := testClient(path)
	n, err := fpc.Prometheus.SaveTargets(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("body = %q, want []", raw)
	}
}

func TestSyncDevicesUnchangedSkipsReload(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "snmp.json")
	fpc := testClient(dest)
	reloads := 0
	fpc.PrometheusConfig.ReloadURL = "http://example.invalid/-/reload"
	fpc.Prometheus.c.ReloadURL = fpc.PrometheusConfig.ReloadURL
	fpc.Prometheus.reload = func() error {
		reloads++
		return nil
	}

	devices := []*models.Device{
		{Name: "r1", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.1/24"},
	}
	reporter := jobevent.NewConsoleReporter(os.Stdout)
	if err := fpc.syncDevices(reporter, devices); err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Fatalf("first run reloads = %d, want 1", reloads)
	}
	if err := fpc.syncDevices(reporter, devices); err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Fatalf("second run reloads = %d, want 1 (unchanged)", reloads)
	}
}

func TestSyncDevicesNoReloadURL(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "snmp.json")
	fpc := testClient(dest)
	fpc.Prometheus.reload = func() error {
		t.Fatal("reload should not be called when ReloadURL is empty")
		return nil
	}
	devices := []*models.Device{
		{Name: "r1", Enabled: true, CfMonitorGrafana: true, PrimaryIPv4: "10.0.0.1/24"},
	}
	if err := fpc.syncDevices(jobevent.NewConsoleReporter(os.Stdout), devices); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	fpc := &FactumPrometheusClient{PrometheusConfig: &util.ConfigPrometheus{}}
	if err := fpc.validate(); err == nil {
		t.Fatal("expected dest_file error")
	}
}

func names(devices []*models.Device) []string {
	out := make([]string, len(devices))
	for i, d := range devices {
		out[i] = d.Name
	}
	return out
}
