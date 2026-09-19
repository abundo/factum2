package icinga

import (
	"strings"
	"testing"

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
