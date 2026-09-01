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
		{Name: "sw1.other.net", Platform: "ios", PrimaryIPv4: "10.1.1.2/32"},
		{Name: "10.1.1.3", Platform: "eos", PrimaryIPv4: "10.1.1.3/24"},
		{Name: "skipme", Platform: "eos"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("wrote %d, want 3", n)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	want := "rtr1.example.com:eos\nsw1.other.net:ios\n10.1.1.3:eos\n"
	if got != want {
		t.Fatalf("router.db =\n%q\nwant\n%q", got, want)
	}
}

func TestSaveDevicesEmptyDomainLeavesShortName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	client := NewOxidizedClient(util.ConfigOxidized{})
	if _, err := client.SaveDevices(path, []*models.Device{
		{Name: "rtr1", Platform: "eos", PrimaryIPv4: "10.1.1.1/32"},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "rtr1:eos") {
		t.Fatalf("got %q", body)
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
