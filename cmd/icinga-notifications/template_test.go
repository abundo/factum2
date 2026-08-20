package main

import (
	"strings"
	"testing"
)

const exampleTemplate = "../../examples/icinga-notification-email.tpl"

func TestRenderTemplateHost(t *testing.T) {
	data := emailData{
		NotificationType:    "PROBLEM",
		HostName:            "sw1",
		HostState:           "DOWN",
		Info:                "CRITICAL - Host Unreachable",
		When:                "2024-01-02 15:04:05",
		NotificationComment: `nasty" comment <script>alert(1)</script>` + "\nsecond line",
		Location:            "DC1",
		Comments:            "<b>not bold</b>",
		HostsDown: []hostDownRow{
			{Name: "sw2", Since: "1h2m", Changed: "2024-01-02 14:00:00", Location: "DC2"},
		},
		CustomersDownEstimate: 20,
		ServicesDownError:     "connection refused",
	}

	body, err := renderTemplate(exampleTemplate, data)
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}

	if strings.Contains(body, "<script>") {
		t.Errorf("unescaped <script> tag found in rendered body")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag in body, got:\n%s", body)
	}
	if !strings.Contains(body, "sw2") {
		t.Errorf("expected down-host row for sw2 in body")
	}
	if !strings.Contains(body, "connection refused") {
		t.Errorf("expected services-down error message in body")
	}
}

func TestRenderTemplateService(t *testing.T) {
	data := emailData{
		IsService:          true,
		NotificationType:   "RECOVERY",
		HostName:           "sw1",
		ServiceDisplayName: "ping",
		ServiceState:       "OK",
		Info:               "PING OK",
	}

	body, err := renderTemplate(exampleTemplate, data)
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if !strings.Contains(body, "Service <strong>ping</strong>") {
		t.Errorf("expected service name in body, got:\n%s", body)
	}
}
