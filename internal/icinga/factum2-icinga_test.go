package icinga

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/abundo/factum2/models"
)

func TestDefaultNotificationTemplateLiteral(t *testing.T) {
	src := "  vars.pe_notify_default = true"
	tmpl, err := template.New("notify").Funcs(hostTemplateFuncs("lab.example")).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, hostTemplateData{Device: &models.Device{Name: "r1"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != src {
		t.Fatalf("literal template: got %q, want %q", got, src)
	}
}

func TestDefaultNotificationTemplateDevice(t *testing.T) {
	src := `  vars.pe_notify_default = true
  vars.factum_role = "{{ .Device.Role }}"`
	tmpl, err := template.New("notify").Funcs(hostTemplateFuncs("lab.example")).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, hostTemplateData{Device: &models.Device{Name: "r1", Role: "core"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "  vars.pe_notify_default = true\n  vars.factum_role = \"core\""
	if got := strings.TrimRight(buf.String(), "\n"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
