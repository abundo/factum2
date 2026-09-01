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
	want := "rtr1.example.com:eos\nrtr1.example.com:eos\nsw1.site.example.com:ios\n10.1.1.3:eos\n"
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
	if string(body) != "10.1.1.3:eos\n" {
		t.Fatalf("got %q", body)
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
