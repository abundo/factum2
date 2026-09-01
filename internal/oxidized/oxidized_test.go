package oxidized

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestSaveDevicesQualifiesShortNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "router.db")
	client := NewOxidizedClient(util.ConfigOxidized{
		CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
	})
	n, err := client.SaveDevices(path, []*models.Device{
		{Name: "rtr1", Platform: "eos", PrimaryIPv4: "10.1.1.1/32"},
		{Name: "rtr1.example.com", Platform: "eos", PrimaryIPv4: "10.1.1.4/32"},
		{Name: "sw1.site", Platform: "ios", PrimaryIPv4: "10.1.1.2/32"},
		{Name: "10.1.1.3", Platform: "eos", PrimaryIPv4: "10.1.1.3/24"},
		{Name: "skipme", Platform: "eos"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("wrote %d, want 4", n)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	want := "rtr1.example.com:10.1.1.1:eos\nrtr1.example.com:10.1.1.4:eos\nsw1.site.example.com:10.1.1.2:ios\n10.1.1.3:10.1.1.3:eos\n"
	if got != want {
		t.Fatalf("router.db =\n%q\nwant\n%q", got, want)
	}
}

func TestSaveDevicesEmptyDomainError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	client := NewOxidizedClient(util.ConfigOxidized{})
	_, err := client.SaveDevices(path, []*models.Device{
		{Name: "rtr1", Platform: "eos", PrimaryIPv4: "10.1.1.1/32"},
	})
	if err == nil || !strings.Contains(err.Error(), "default_domain") {
		t.Fatalf("expected default_domain error, got %v", err)
	}
}

func TestSaveDevicesEmptyDomainAllowsIPNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	client := NewOxidizedClient(util.ConfigOxidized{})
	n, err := client.SaveDevices(path, []*models.Device{
		{Name: "10.1.1.3", Platform: "eos", PrimaryIPv4: "10.1.1.3/24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("wrote %d, want 1", n)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "10.1.1.3:10.1.1.3:eos\n" {
		t.Fatalf("got %q", body)
	}
}

func TestLoadDevicesParsesNameIPModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	if err := os.WriteFile(path, []byte("rtr1.example.com:10.1.1.1:eos\nsw1:ios\n:missing-name:eos\nbadline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := NewOxidizedClient(util.ConfigOxidized{DestFile: path})
	got, err := client.GetDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	rtr := got["rtr1.example.com"]
	if rtr == nil || rtr.IP != "10.1.1.1" || rtr.Model != "eos" {
		t.Fatalf("rtr1 = %+v", rtr)
	}
	sw := got["sw1"]
	if sw == nil || sw.IP != "" || sw.Model != "ios" {
		t.Fatalf("sw1 = %+v", sw)
	}
}

func TestParseRouterDBLine(t *testing.T) {
	tests := []struct {
		line            string
		name, ip, model string
		ok              bool
	}{
		{"rtr1.example.com:10.1.1.1:eos", "rtr1.example.com", "10.1.1.1", "eos", true},
		{"rtr1:eos", "rtr1", "", "eos", true},
		{"10.1.1.3:10.1.1.3:eos", "10.1.1.3", "10.1.1.3", "eos", true},
		{"", "", "", "", false},
		{"onlyname", "", "", "", false},
		{":eos", "", "", "", false},
		{"rtr1:", "", "", "", false},
	}
	for _, tt := range tests {
		name, ip, model, ok := parseRouterDBLine(tt.line)
		if name != tt.name || ip != tt.ip || model != tt.model || ok != tt.ok {
			t.Errorf("parseRouterDBLine(%q) = %q %q %q %v, want %q %q %q %v",
				tt.line, name, ip, model, ok, tt.name, tt.ip, tt.model, tt.ok)
		}
	}
}

func TestDeviceFQDN(t *testing.T) {
	tests := []struct {
		name, domain, want string
	}{
		{"rtr1", "example.com", "rtr1.example.com"},
		{"rtr1.example.com", "example.com", "rtr1.example.com"},
		{"rtr1.example.com.", "example.com", "rtr1.example.com"},
		{"rtr1.site", "example.com", "rtr1.site.example.com"},
		{"10.1.1.1", "example.com", "10.1.1.1"},
		{"rtr1", "", "rtr1"},
		{"", "example.com", ""},
	}
	for _, tt := range tests {
		if got := deviceFQDN(tt.name, tt.domain); got != tt.want {
			t.Errorf("deviceFQDN(%q, %q) = %q, want %q", tt.name, tt.domain, got, tt.want)
		}
	}
}

func TestGetDeviceConfigUsesFQDN(t *testing.T) {
	var fetched string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = r.URL.Path
		_, _ = io.WriteString(w, "hostname rtr1\n")
	}))
	t.Cleanup(srv.Close)
	client := NewOxidizedClient(util.ConfigOxidized{
		CommonConfig: util.CommonConfig{DefaultDomain: "example.com"},
		URL:          srv.URL,
	})
	cfg, err := client.GetDeviceConfig("rtr1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != "hostname rtr1\n" {
		t.Fatalf("config = %q", cfg)
	}
	if fetched != "/node/fetch/rtr1.example.com" {
		t.Fatalf("path = %q", fetched)
	}
}
