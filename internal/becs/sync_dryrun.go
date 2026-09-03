package becs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/netboxtool"
)

const dryRunFakeIDBase uint = 1_000_000_000

func (s *Syncer) emitWrite(format string, args ...any) {
	if s.dryRun {
		s.reporter.Emit(jobevent.Info, "dry-run: would "+format, args...)
		return
	}
	s.reporter.Emit(jobevent.Info, format, args...)
}

func formatPayload(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// dryRunNetbox wraps a netboxAPI so reads still hit the inner client (or
// the in-memory overlay after a simulated write) while every mutating call
// is applied only to the local device list. Used by Sync --dry-run so later
// phases can plan interfaces/addresses of devices that would be created.
type dryRunNetbox struct {
	inner   netboxAPI
	devices []*netboxtool.NBDevice
	nextID  uint
}

func newDryRunAPI(inner netboxAPI, devices []*netboxtool.NBDevice) *dryRunNetbox {
	return &dryRunNetbox{inner: inner, devices: devices, nextID: dryRunFakeIDBase}
}

func (d *dryRunNetbox) alloc() uint {
	d.nextID++
	return d.nextID
}

func (d *dryRunNetbox) GetDevices() ([]*netboxtool.NBDevice, error) {
	return d.devices, nil
}

func (d *dryRunNetbox) GetDevice(name string, id int) (*netboxtool.NBDevice, error) {
	for _, dev := range d.devices {
		if name != "" && strings.EqualFold(dev.Name, name) {
			return dev, nil
		}
		if id > 0 && int(dev.NetboxID) == id {
			return dev, nil
		}
	}
	return nil, nil
}

func (d *dryRunNetbox) GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error) {
	return d.inner.GetDeviceType(manufacturer, model)
}

func (d *dryRunNetbox) CreateDevice(name string, extra map[string]any) (*netboxtool.NetboxDeviceREST, error) {
	id := d.alloc()
	dev := &netboxtool.NBDevice{NetboxID: id, Name: name}
	applyDeviceChanges(dev, extra)
	d.devices = append(d.devices, dev)
	return &netboxtool.NetboxDeviceREST{ID: id, Name: name}, nil
}

func (d *dryRunNetbox) UpdateDevice(deviceID uint, changes map[string]any) error {
	for _, dev := range d.devices {
		if dev.NetboxID != deviceID {
			continue
		}
		applyDeviceChanges(dev, changes)
		return nil
	}
	return nil
}

func (d *dryRunNetbox) DeleteDevice(id int) error {
	keep := d.devices[:0]
	for _, dev := range d.devices {
		if int(dev.NetboxID) != id {
			keep = append(keep, dev)
		}
	}
	d.devices = keep
	return nil
}

func (d *dryRunNetbox) CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error) {
	id := d.alloc()
	iface := netboxtool.NBInterface{NetboxID: id, Name: name}
	applyInterfaceChanges(&iface, extra)
	for _, dev := range d.devices {
		if dev.NetboxID == deviceID {
			dev.Interfaces = append(dev.Interfaces, iface)
			break
		}
	}
	return &netboxtool.NetboxInterfaceREST{ID: id, Name: name}, nil
}

func (d *dryRunNetbox) InterfaceUpdate(interfaceID int, changes map[string]any) error {
	for _, dev := range d.devices {
		for i := range dev.Interfaces {
			if int(dev.Interfaces[i].NetboxID) != interfaceID {
				continue
			}
			applyInterfaceChanges(&dev.Interfaces[i], changes)
			return nil
		}
	}
	return nil
}

func (d *dryRunNetbox) InterfaceDelete(interfaceID int) error {
	for _, dev := range d.devices {
		keep := dev.Interfaces[:0]
		for _, iface := range dev.Interfaces {
			if int(iface.NetboxID) != interfaceID {
				keep = append(keep, iface)
			}
		}
		dev.Interfaces = keep
	}
	return nil
}

func (d *dryRunNetbox) CreateInterfaceAddress(interfaceID uint, address string, extra map[string]any) (*netboxtool.NBAddress, error) {
	id := d.alloc()
	addr := &netboxtool.NBAddress{NetboxID: id, Address: address, NBInterfaceID: interfaceID}
	for _, dev := range d.devices {
		for i := range dev.Interfaces {
			if dev.Interfaces[i].NetboxID != interfaceID {
				continue
			}
			dev.Interfaces[i].Addresses = append(dev.Interfaces[i].Addresses, *addr)
			return addr, nil
		}
	}
	return addr, nil
}

func (d *dryRunNetbox) AddressDelete(addressID int) error {
	for _, dev := range d.devices {
		for i := range dev.Interfaces {
			keep := dev.Interfaces[i].Addresses[:0]
			for _, addr := range dev.Interfaces[i].Addresses {
				if int(addr.NetboxID) != addressID {
					keep = append(keep, addr)
				}
			}
			dev.Interfaces[i].Addresses = keep
		}
	}
	return nil
}

func (d *dryRunNetbox) GetSiteByName(name string) (*netboxtool.NetboxNamedRef, error) {
	return d.inner.GetSiteByName(name)
}

func (d *dryRunNetbox) GetDeviceRoleBySlug(slug string) (*netboxtool.NetboxNamedRef, error) {
	return d.inner.GetDeviceRoleBySlug(slug)
}

func (d *dryRunNetbox) GetPlatformBySlug(slug string) (*netboxtool.NetboxNamedRef, error) {
	return d.inner.GetPlatformBySlug(slug)
}

func (d *dryRunNetbox) RequireCustomField(name string, objectTypes ...string) error {
	return d.inner.RequireCustomField(name, objectTypes...)
}

func applyDeviceChanges(d *netboxtool.NBDevice, changes map[string]any) {
	if changes == nil {
		return
	}
	if name, ok := changes["name"].(string); ok {
		d.Name = name
	}
	if status, ok := changes["status"].(string); ok {
		d.Status = status
		d.Enabled = status == "active"
	}
	if v, ok := asUint(changes["site"]); ok {
		d.SiteID = v
	}
	if v, ok := asUint(changes["device_type"]); ok {
		d.ModelID = v
	}
	if v, ok := asUint(changes["role"]); ok {
		d.RoleID = v
	}
	if v, ok := asUint(changes["platform"]); ok {
		d.PlatformID = v
	}
	if v, ok := asUint(changes["primary_ip4"]); ok {
		d.PrimaryIPv4ID = v
	} else if changes["primary_ip4"] == nil {
		if _, present := changes["primary_ip4"]; present {
			d.PrimaryIPv4ID = 0
			d.PrimaryIPv4 = ""
		}
	}
	if cf, ok := changes["custom_fields"].(map[string]any); ok {
		applyCustomFields(d, cf)
	}
}

func applyCustomFields(d *netboxtool.NBDevice, cf map[string]any) {
	if d.CustomFields == nil {
		d.CustomFields = map[string]any{}
	}
	for k, v := range cf {
		d.CustomFields[k] = v
		switch k {
		case "parents":
			if s, ok := v.(string); ok {
				d.CfParents = s
			}
		case "alarm_destination":
			if s, ok := v.(string); ok {
				d.CfAlarmDestination = s
			}
		case "alarm_timeperiod":
			if s, ok := v.(string); ok {
				d.CfAlarmTimeperiod = s
			}
		case "connection_method":
			if s, ok := v.(string); ok {
				d.CfConnectionMethod = s
			}
		case "backup_oxidized":
			if b, ok := v.(bool); ok {
				d.CfBackupOxidized = b
			}
		case "alarm_interfaces":
			if b, ok := v.(bool); ok {
				d.CfAlarmInterfaces = b
			}
		case "monitor_grafana":
			if b, ok := v.(bool); ok {
				d.CfMonitorGrafana = b
			}
		case "monitor_icinga":
			if b, ok := v.(bool); ok {
				d.CfMonitorIcinga = b
			}
		case "monitor_librenms":
			if b, ok := v.(bool); ok {
				d.CfMonitorLibrenms = b
			}
		}
	}
}

func applyInterfaceChanges(iface *netboxtool.NBInterface, changes map[string]any) {
	if changes == nil {
		return
	}
	if name, ok := changes["name"].(string); ok {
		iface.Name = name
	}
	if typ, ok := changes["type"].(string); ok {
		iface.Type = typ
	}
	if en, ok := changes["enabled"].(bool); ok {
		iface.Enabled = en
	}
	if cf, ok := changes["custom_fields"].(map[string]any); ok {
		if iface.CustomFields == nil {
			iface.CustomFields = map[string]any{}
		}
		for k, v := range cf {
			iface.CustomFields[k] = v
		}
	}
}

func asUint(v any) (uint, bool) {
	switch n := v.(type) {
	case uint:
		return n, true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint(n), true
	case float64:
		if n < 0 {
			return 0, false
		}
		return uint(n), true
	default:
		return 0, false
	}
}

var _ netboxAPI = (*dryRunNetbox)(nil)
