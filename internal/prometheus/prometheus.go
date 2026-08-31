package prometheus

//
// Write a Prometheus file_sd JSON of SNMP targets for snmp_exporter, and
// optionally ask Prometheus to reload.
//
// The scrape job itself is operator-owned. A typical companion config:
//
//	scrape_configs:
//	  - job_name: snmp
//	    metrics_path: /snmp
//	    file_sd_configs:
//	      - files: [/etc/prometheus/snmp_targets.json]
//	    relabel_configs:
//	      - source_labels: [__address__]
//	        target_label: __param_target
//	      - source_labels: [__param_target]
//	        target_label: instance
//	      - source_labels: [module]
//	        target_label: __param_module
//	      - source_labels: [auth]
//	        target_label: __param_auth
//	      - target_label: __address__
//	        replacement: 127.0.0.1:9116  # snmp_exporter
//

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

const (
	defaultModule = "if_mib"
	defaultAuth   = "public_v2"
)

// fileSDTarget is one Prometheus file_sd group: a single SNMP target
// (device primary IPv4) plus labels snmp_exporter/Grafana use.
type fileSDTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type prometheusClient struct {
	c util.ConfigPrometheus
	// reload, if set, replaces httpReload - tests stub it so Sync doesn't
	// need a real Prometheus.
	reload func() error
}

func NewPrometheusClient(c util.ConfigPrometheus) *prometheusClient {
	return &prometheusClient{c: c}
}

func (p *prometheusClient) module() string {
	if strings.TrimSpace(p.c.Module) == "" {
		return defaultModule
	}
	return strings.TrimSpace(p.c.Module)
}

func (p *prometheusClient) auth() string {
	if strings.TrimSpace(p.c.Auth) == "" {
		return defaultAuth
	}
	return strings.TrimSpace(p.c.Auth)
}

func primaryIP(device *models.Device) string {
	return strings.Split(device.PrimaryIPv4, "/")[0]
}

func fileSDLabels(device *models.Device, module, auth string) map[string]string {
	labels := map[string]string{
		"module": module,
		"auth":   auth,
		"device": device.Name,
	}
	if device.Site != "" {
		labels["site"] = device.Site
	}
	if device.Role != "" {
		labels["role"] = device.Role
	}
	return labels
}

// SaveTargets writes Prometheus file_sd JSON for devices to filename.
// Devices are sorted by name so successive writes stay byte-identical
// when the set hasn't changed. Returns the number of targets written.
func (p *prometheusClient) SaveTargets(filename string, devices []*models.Device) (int, error) {
	sorted := append([]*models.Device(nil), devices...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})

	module := p.module()
	auth := p.auth()
	targets := make([]fileSDTarget, 0, len(sorted))
	for _, device := range sorted {
		addr := primaryIP(device)
		if addr == "" {
			continue
		}
		targets = append(targets, fileSDTarget{
			Targets: []string{addr},
			Labels:  fileSDLabels(device, module, auth),
		})
	}

	body, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return 0, err
	}
	body = append(body, '\n')
	if err := os.WriteFile(filename, body, 0o644); err != nil {
		return 0, err
	}
	return len(targets), nil
}

// Reload POSTs PrometheusReloadURL (typically /-/reload). No-op when the
// URL is empty - file_sd re-reads DestFile on its own interval.
func (p *prometheusClient) Reload() error {
	if p.reload != nil {
		return p.reload()
	}
	return p.httpReload()
}

func (p *prometheusClient) httpReload() error {
	url := strings.TrimSpace(p.c.ReloadURL)
	if url == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("prometheus reload %s: %s", url, resp.Status)
	}
	return nil
}

// installConfFile installs tmpFile as dst, but only if its content differs
// from what's already there - this is what tells Sync() whether Prometheus
// actually needs a reload.
func installConfFile(tmpFile, dst string) (bool, error) {
	newContent, err := os.ReadFile(tmpFile)
	if err != nil {
		return false, err
	}
	oldContent, err := os.ReadFile(dst)
	if err == nil && bytes.Equal(oldContent, newContent) {
		os.Remove(tmpFile)
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(tmpFile, dst); err != nil {
		return false, err
	}
	return true, nil
}
