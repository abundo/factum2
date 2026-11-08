package oxidized

//
// Build oxidized's router.db from device-api (factum), and ask oxidized to
// reload if the file changed.
//

import (
	"bytes"
	"os"

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
func NewFactumOxidizedClient(root *util.ConfigAgentRoot) (*FactumOxidizedClient, error) {
	client := new(FactumOxidizedClient)
	client.Config = &root.Factum

	oxidizedConfig, err := FetchRemoteConfig(&root.Factum)
	if err != nil {
		return nil, err
	}
	if err := pinDest(oxidizedConfig, root.Worker.Paths); err != nil {
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

// filterCounts tallies why devices were skipped, reported once at the end
// of filterDevices - mirrors the Python original's Counter().
type filterCounts struct {
	notEnabled         int
	notBackupOxidized  int
	ignoreDevice       int
	ignoreManufacturer int
	ignoreModel        int
	ignorePlatform     int
	ignoreRole         int
	noPrimaryIP        int
}

// filterDevices keeps only devices that should be backed up by oxidized:
// enabled, flagged for oxidized backup, not on any of the configured ignore
// lists, and with a primary IPv4 address.
func (foc *FactumOxidizedClient) filterDevices(all []*models.Device) ([]*models.Device, filterCounts) {
	var counts filterCounts
	var devices []*models.Device
	filter := util.ConfigOxidized{}.DeviceFilter()
	if foc.OxidizedConfig != nil {
		filter = foc.OxidizedConfig.DeviceFilter()
	}

	for _, device := range all {
		if !device.Enabled {
			counts.notEnabled++
			continue
		}
		if !device.CfBackupOxidized {
			counts.notBackupOxidized++
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
			"%d ignored manufacturer, %d ignored model, %d ignored platform, %d ignored role, %d without primary ip4",
		counts.notEnabled, counts.notBackupOxidized, counts.ignoreDevice,
		counts.ignoreManufacturer, counts.ignoreModel, counts.ignorePlatform, counts.ignoreRole, counts.noPrimaryIP)
	reporter.Emit(jobevent.Info, "Devices: %d of %d total", len(devices), len(allDevices))

	tmpFile := foc.OxidizedConfig.DestFile + ".tmp"
	count, err := foc.Oxidized.SaveDevices(tmpFile, devices)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Wrote %d device(s) to %s", count, tmpFile)

	if count == 0 {
		if err := ensureUsableRouterDB(tmpFile, count); err != nil {
			reporter.EmitErr(err)
			return err
		}
		reporter.Emit(jobevent.Info, "No devices to backup; keeping dummy node so oxidized stays running")
	}

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
