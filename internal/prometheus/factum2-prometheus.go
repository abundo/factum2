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
func NewFactumPrometheusClient(root *util.ConfigAgentRoot) (*FactumPrometheusClient, error) {
	client := new(FactumPrometheusClient)
	client.Config = &root.Factum

	prometheusConfig, err := FetchRemoteConfig(&root.Factum)
	if err != nil {
		return nil, err
	}
	dest, err := util.ResolvePinnedKey(root.Worker.Paths, prometheusConfig.DestFile, util.KeyPrometheusDestFile, false, false)
	if err != nil {
		return nil, err
	}
	prometheusConfig.DestFile = dest
	client.PrometheusConfig = prometheusConfig
	client.Prometheus = NewPrometheusClient(*prometheusConfig)
	return client, nil
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
	ignoreRole         int
	noPrimaryIP        int
}

// filterDevices keeps only devices that should be scraped via snmp_exporter:
// enabled, flagged for Grafana/Prometheus monitoring, not on any of the
// configured ignore lists, and with a primary IPv4 address.
func (fpc *FactumPrometheusClient) filterDevices(all []*models.Device) ([]*models.Device, filterCounts) {
	var counts filterCounts
	var devices []*models.Device
	filter := util.ConfigPrometheus{}.DeviceFilter()
	if fpc.PrometheusConfig != nil {
		filter = fpc.PrometheusConfig.DeviceFilter()
	}

	for _, device := range all {
		if !device.Enabled {
			counts.notEnabled++
			continue
		}
		if !device.CfMonitorGrafana {
			counts.notMonitorGrafana++
			continue
		}
		if models.ListContains(device.Name, filter.Devices) {
			counts.ignoreDevice++
			continue
		}
		if models.ListContains(device.Manufacturer, filter.Manufacturers) {
			counts.ignoreManufacturer++
			continue
		}
		if models.ListContains(device.ModelName, filter.Models) {
			counts.ignoreModel++
			continue
		}
		if models.ListContains(device.Platform, filter.Platforms) {
			counts.ignorePlatform++
			continue
		}
		if !models.RoleMatches(device.Role, filter.Roles) {
			counts.ignoreRole++
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
	if _, err := CompileIgnoreInterfaces(fpc.PrometheusConfig.IgnoreInterfaces); err != nil {
		reporter.EmitErr(err)
		return err
	}
	devices, counts := fpc.filterDevices(all)
	reporter.Emit(jobevent.Info,
		"Filtered devices: %d not enabled, %d not flagged for grafana/prometheus, %d ignored by name, "+
			"%d ignored manufacturer, %d ignored model, %d ignored platform, %d ignored role, %d without primary ip4",
		counts.notEnabled, counts.notMonitorGrafana, counts.ignoreDevice,
		counts.ignoreManufacturer, counts.ignoreModel, counts.ignorePlatform, counts.ignoreRole, counts.noPrimaryIP)
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
	ignoreChanged, err := fpc.installIgnore(reporter)
	if err != nil {
		return err
	}
	if changed || ignoreChanged {
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

// installIgnore rewrites the factum-if-ignore block in the scrape config
// next to the file_sd file. A missing marker is a warning when the list is
// non-empty: the targets file is still written.
func (fpc *FactumPrometheusClient) installIgnore(reporter jobevent.Reporter) (bool, error) {
	list := fpc.PrometheusConfig.IgnoreInterfaces
	path, changed, err := InstallIgnoreInterfaces(fpc.PrometheusConfig.DestFile, list)
	if err != nil {
		if errors.Is(err, ErrNoIgnoreConfig) {
			if strings.TrimSpace(list) != "" {
				reporter.Emit(jobevent.Warning, "Interface ignore list is not applied: %s", err)
			}
			return false, nil
		}
		reporter.EmitErr(err)
		return false, err
	}
	if changed {
		reporter.Emit(jobevent.Info, "Updated interface ignore rules in %s", path)
	}
	return changed, nil
}
