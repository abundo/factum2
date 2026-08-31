package prometheus

//
// Build Prometheus's snmp_exporter file_sd from device-api (factum), and
// ask Prometheus to reload if the file changed.
//

import (
	"errors"
	"strings"

	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

type FactumPrometheusClient struct {
	Config           *util.ConfigFactum
	PrometheusConfig *util.ConfigPrometheus
	Prometheus       *prometheusClient
}

// NewFactumPrometheusClient fetches the Prometheus/snmp_exporter sync
// settings from the primary over REST - config.Prometheus is never used,
// since factum2-prometheus typically runs on a different host than the
// primary and has no local prometheus config of its own (see
// util.ConfigPrometheus's doc comment).
func NewFactumPrometheusClient(config *util.ConfigFactum) (*FactumPrometheusClient, error) {
	client := new(FactumPrometheusClient)
	client.Config = config

	prometheusConfig, err := FetchRemoteConfig(config)
	if err != nil {
		return nil, err
	}
	client.PrometheusConfig = prometheusConfig
	client.Prometheus = NewPrometheusClient(*prometheusConfig)
	return client, nil
}

// isInList reports whether value appears in list, a newline-separated set
// of values (Settings.PrometheusIgnore*).
func isInList(value, list string) bool {
	if value == "" {
		return false
	}
	for _, item := range strings.Split(list, "\n") {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

// filterCounts tallies why devices were skipped, reported once at the end
// of filterDevices.
type filterCounts struct {
	notEnabled         int
	notMonitorGrafana  int
	ignoreDevice       int
	ignoreManufacturer int
	ignoreModel        int
	ignorePlatform     int
	noPrimaryIP        int
}

// filterDevices keeps only devices that should be scraped via snmp_exporter:
// enabled, flagged for Grafana/Prometheus monitoring, not on any of the
// configured ignore lists, and with a primary IPv4 address.
func (fpc *FactumPrometheusClient) filterDevices(all []*models.Device) ([]*models.Device, filterCounts) {
	var counts filterCounts
	var devices []*models.Device

	for _, device := range all {
		if !device.Enabled {
			counts.notEnabled++
			continue
		}
		if !device.CfMonitorGrafana {
			counts.notMonitorGrafana++
			continue
		}
		if isInList(device.Name, fpc.PrometheusConfig.IgnoreDevices) {
			counts.ignoreDevice++
			continue
		}
		if isInList(device.Manufacturer, fpc.PrometheusConfig.IgnoreManufacturers) {
			counts.ignoreManufacturer++
			continue
		}
		if isInList(device.ModelName, fpc.PrometheusConfig.IgnoreModels) {
			counts.ignoreModel++
			continue
		}
		if isInList(device.Platform, fpc.PrometheusConfig.IgnorePlatforms) {
			counts.ignorePlatform++
			continue
		}
		if device.PrimaryIPv4 == "" {
			counts.noPrimaryIP++
			continue
		}
		devices = append(devices, device)
	}
	return devices, counts
}

func (fpc *FactumPrometheusClient) validate() error {
	if fpc.PrometheusConfig == nil {
		return errors.New("prometheus config is not loaded")
	}
	if strings.TrimSpace(fpc.PrometheusConfig.DestFile) == "" {
		return errors.New("prometheus dest_file is not configured")
	}
	return nil
}

// Sync fetches devices from the Factum API, filters out those that
// shouldn't be scraped, writes Prometheus file_sd JSON, and asks Prometheus
// to reload if anything changed (and a reload URL is configured).
func (fpc *FactumPrometheusClient) Sync(reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "Prometheus sync started")
	if err := fpc.validate(); err != nil {
		reporter.EmitErr(err)
		return err
	}
	factumClient := factum.NewFactumClient(fpc.Config)
	allDevices, err := factumClient.GetDevices()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	return fpc.syncDevices(reporter, allDevices)
}

func (fpc *FactumPrometheusClient) syncDevices(reporter jobevent.Reporter, all []*models.Device) error {
	devices, counts := fpc.filterDevices(all)
	reporter.Emit(jobevent.Info,
		"Filtered devices: %d not enabled, %d not flagged for grafana/prometheus, %d ignored by name, "+
			"%d ignored manufacturer, %d ignored model, %d ignored platform, %d without primary ip4",
		counts.notEnabled, counts.notMonitorGrafana, counts.ignoreDevice,
		counts.ignoreManufacturer, counts.ignoreModel, counts.ignorePlatform, counts.noPrimaryIP)
	reporter.Emit(jobevent.Info, "Devices: %d of %d total", len(devices), len(all))

	tmpFile := fpc.PrometheusConfig.DestFile + ".tmp"
	count, err := fpc.Prometheus.SaveTargets(tmpFile, devices)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Wrote %d target(s) to %s", count, tmpFile)

	changed, err := installConfFile(tmpFile, fpc.PrometheusConfig.DestFile)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if changed {
		if strings.TrimSpace(fpc.PrometheusConfig.ReloadURL) == "" {
			reporter.Emit(jobevent.Info, "Configuration changed (no reload URL configured)")
			return nil
		}
		reporter.Emit(jobevent.Info, "Configuration changed, reloading prometheus")
		if err := fpc.Prometheus.Reload(); err != nil {
			reporter.EmitErr(err)
			return err
		}
	} else {
		reporter.Emit(jobevent.Info, "Configuration unchanged")
	}
	return nil
}
