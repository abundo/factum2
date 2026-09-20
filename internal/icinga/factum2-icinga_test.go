package icinga

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

func TestDefaultNotificationTemplateLiteral(t *testing.T) {
	src := "  vars.pe_notify_default = true"
	got, err := executeIcingaTemplate("notify", src, hostTemplateData{Device: &models.Device{Name: "r1"}}, "lab.example")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(got, "\n"); got != src {
		t.Fatalf("literal template: got %q, want %q", got, src)
	}
}

func TestDefaultNotificationTemplateDevice(t *testing.T) {
	src := `  vars.pe_notify_default = true
  vars.factum_role = "{{ .Device.Role }}"`
	got, err := executeIcingaTemplate("notify", src, hostTemplateData{Device: &models.Device{Name: "r1", Role: "core"}}, "lab.example")
	if err != nil {
		t.Fatal(err)
	}
	want := "  vars.pe_notify_default = true\n  vars.factum_role = \"core\""
	if got := strings.TrimRight(got, "\n"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHostTemplateFqdnIp(t *testing.T) {
	src := `object Host "{{ fqdn(.Device.Name) }}" { address = "{{ ip(.Device.PrimaryIPv4) }}" }`
	got, err := executeIcingaTemplate("host", src, hostTemplateData{Device: &models.Device{Name: "r1", PrimaryIPv4: "10.0.0.1/24"}}, "lab.example")
	if err != nil {
		t.Fatal(err)
	}
	want := `object Host "r1.lab.example" { address = "10.0.0.1" }`
	if strings.TrimSpace(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCollectCertChecksSkipsWildcardAndEmptyHost(t *testing.T) {
	got := collectCertChecks([]util.ConfigIcingaCert{
		{Name: "bw", Host: "lu1-vm15.itn.nu", Domains: []string{"bitwarden.itn.nu", "*.itn.nu", ""}},
		{Name: "skip", Host: "", Domains: []string{"unused.example"}},
		{Name: "dup", Host: "lu1-vm15.itn.nu", Domains: []string{"bitwarden.itn.nu"}},
	})
	if len(got) != 1 || got[0].Host != "lu1-vm15.itn.nu" || got[0].Domain != "bitwarden.itn.nu" {
		t.Fatalf("got %#v", got)
	}
}

func TestWriteCerts(t *testing.T) {
	dir := t.TempDir()
	certsFile := filepath.Join(dir, "certs.conf")
	hostsFile := filepath.Join(dir, "hosts.conf")
	if err := os.WriteFile(hostsFile, []byte("object Host \"lu1-vm15.itn.nu\" {\n  address = \"10.0.0.1\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fic := &FactumIcingaClient{
		IcingaConfig: &util.ConfigIcinga{
			HostsFile:    hostsFile,
			CertsFile:    certsFile,
			CertTemplate: DefaultCertTemplate,
			Certificates: []util.ConfigIcingaCert{
				{Name: "bw", Host: "lu1-vm15.itn.nu", Domains: []string{"bitwarden.itn.nu", "*.itn.nu"}},
				{Name: "ip", Host: "192.0.2.10", Domains: []string{"ip.example.com"}},
			},
		},
	}
	changed, err := fic.writeCerts(jobevent.NewConsoleReporter(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected certs file to be written")
	}
	body, err := os.ReadFile(certsFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		`template Service "factum-cert-check"`,
		`object Service "HTTPS cert - bitwarden.itn.nu"`,
		`host_name = "lu1-vm15.itn.nu"`,
		`vars.http_vhost = "bitwarden.itn.nu"`,
		`object Service "HTTPS cert - ip.example.com"`,
		`host_name = "192.0.2.10"`,
		`object Host "192.0.2.10"`,
		`address = "192.0.2.10"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "*.itn.nu") {
		t.Errorf("wildcard should be skipped:\n%s", s)
	}
	if strings.Contains(s, `object Host "lu1-vm15.itn.nu"`) {
		t.Errorf("should not recreate existing host:\n%s", s)
	}
}

func TestWriteCertsSkippedWhenUnconfigured(t *testing.T) {
	fic := &FactumIcingaClient{IcingaConfig: &util.ConfigIcinga{}}
	changed, err := fic.writeCerts(jobevent.NewConsoleReporter(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("empty certs file/template should skip")
	}
}
