package oxidized

//
// Build oxidized's router.db from device-api (factum), and ask oxidized to
// reload if the file changed.
//

import (
	"bytes"
	"os"
	"strings"

	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
)

type FactumOxidizedClient struct {
	Config         *util.ConfigFactum
	OxidizedConfig *util.ConfigOxidized
	Oxidized       *oxidizedClient
}

// NewFactumOxidizedClient fetches the Oxidized API connection settings and
// the oxidized-sync settings (router.db path, ignore lists) from the
// primary over REST - config.Oxidized is never used, since factum2-oxidized
// typically runs on a different host than the primary and has no local
// oxidized config of its own (see util.ConfigOxidized's doc comment).
func NewFactumOxidizedClient(config *util.ConfigFactum) (*FactumOxidizedClient, error) {
	client := new(FactumOxidizedClient)
	client.Config = config

	oxidizedConfig, err := FetchRemoteConfig(config)
	if err != nil {
		return nil, err
	}
	client.OxidizedConfig = oxidizedConfig
	client.Oxidized = NewOxidizedClient(*oxidizedConfig)
	return client, nil
}

// installConfFile installs tmpFile as dst, but only if its content differs
// from what's already there - this is what tells Sync() whether oxidized
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

// isInList reports whether value appears in list, a newline-separated set
// of values (Settings.OxidizedIgnore*).
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
// of filterDevices - mirrors the Python original's Counter().
type filterCounts struct {
	notEnabled         int
	notBackupOxidized  int
	ignoreDevice       int
	ignoreManufacturer int
	ignoreModel        int
	ignorePlatform     int
	noPrimaryIP        int
}

// filterDevices keeps only devices that should be backed up by oxidized:
// enabled, flagged for oxidized backup, not on any of the configured ignore
// lists, and with a primary IPv4 address.
func (foc *FactumOxidizedClient) filterDevices(all []*models.Device) ([]*models.Device, filterCounts) {
	var counts filterCounts
	var devices []*models.Device

	for _, device := range all {
		if !device.Enabled {
			counts.notEnabled++
			continue
		}
		if !device.CfBackupOxidized {
			counts.notBackupOxidized++
			continue
		}
		if isInList(device.Name, foc.OxidizedConfig.IgnoreDevices) {
			counts.ignoreDevice++
			continue
		}
		if isInList(device.Manufacturer, foc.OxidizedConfig.IgnoreManufacturers) {
			counts.ignoreManufacturer++
			continue
		}
		if isInList(device.ModelName, foc.OxidizedConfig.IgnoreModels) {
			counts.ignoreModel++
			continue
		}
		if isInList(device.Platform, foc.OxidizedConfig.IgnorePlatforms) {
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

// Sync fetches devices from the Factum API, filters out those that don't
// need backup, writes oxidized's router.db, and asks oxidized to reload if
// anything changed.
func (foc *FactumOxidizedClient) Sync(reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "Oxidized sync started")
	factumClient := factum.NewFactumClient(foc.Config)
	allDevices, err := factumClient.GetDevices()
	if err != nil {
		reporter.EmitErr(err)
		return err
	}

	devices, counts := foc.filterDevices(allDevices)
	reporter.Emit(jobevent.Info,
		"Filtered devices: %d not enabled, %d not flagged for oxidized backup, %d ignored by name, "+
			"%d ignored manufacturer, %d ignored model, %d ignored platform, %d without primary ip4",
		counts.notEnabled, counts.notBackupOxidized, counts.ignoreDevice,
		counts.ignoreManufacturer, counts.ignoreModel, counts.ignorePlatform, counts.noPrimaryIP)
	reporter.Emit(jobevent.Info, "Devices: %d of %d total", len(devices), len(allDevices))

	tmpFile := foc.OxidizedConfig.DestFile + ".tmp"
	count, err := foc.Oxidized.SaveDevices(tmpFile, devices)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Wrote %d device(s) to %s", count, tmpFile)

	changed, err := installConfFile(tmpFile, foc.OxidizedConfig.DestFile)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	if changed {
		reporter.Emit(jobevent.Info, "Configuration changed, reloading oxidized")
		if _, err := foc.Oxidized.Reload(); err != nil {
			reporter.EmitErr(err)
			return err
		}
	} else {
		reporter.Emit(jobevent.Info, "Configuration unchanged")
	}
	return nil
}
