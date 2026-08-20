package web

import (
	"fmt"
	"net/http"
	"strings"

	devicesync "github.com/abundo/factum2/internal/device-sync"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"github.com/labstack/echo/v5"
)

// --------------------------------------------------------------------------
//
//	API: device interfaces (driver-backed refresh/update)
//
//	Both endpoints only support platforms whose driver implements
//	GetInterfacesStatus/SetInterfaceDescriptions (internal/drivers.
//	AristaDriver for "eos", internal/drivers.NokiaDriver for "sros"/
//	"sros-md", internal/drivers.IOSXRDriver for "ios-xr",
//	internal/drivers.VrpDriver for "vrp"). Credentials are supplied
//	per-request by the caller (never persisted), the same way the
//	factum-driver-cli commands take --username/--password.
//
// --------------------------------------------------------------------------

var supportedDriverPlatforms = []string{"eos", "sros", "sros-md", "ios-xr", "vrp"}

type deviceCredentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// isSupportedDriverPlatform checks the device's platform case-insensitively
// against supportedDriverPlatforms - factum stores it as entered by the
// source system (Netbox has it as e.g. "EOS" or "SROS-MD"), while
// internal/drivers.NewDriver switches on the lowercase form.
func isSupportedDriverPlatform(device *models.Device) bool {
	for _, p := range supportedDriverPlatforms {
		if strings.EqualFold(device.Platform, p) {
			return true
		}
	}
	return false
}

// globalVlanPlatforms are the platforms whose driver implements
// SetInterfaceVLANs for real (internal/drivers.AristaDriver for "eos",
// internal/drivers.VrpDriver for "vrp") - both model VLANs as a device-wide
// VLAN database that switchports reference, unlike ios-xr/sros(-md), which
// have no per-interface global-VLAN concept at all (see
// drivers.Interface.SwitchportMode's doc comment). A separate list from
// supportedDriverPlatforms since the two properties are independent.
var globalVlanPlatforms = []string{"eos", "vrp"}

// isGlobalVlanPlatform is isSupportedDriverPlatform's counterpart for the
// VLAN endpoint.
func isGlobalVlanPlatform(device *models.Device) bool {
	for _, p := range globalVlanPlatforms {
		if strings.EqualFold(device.Platform, p) {
			return true
		}
	}
	return false
}

func (ctrl *Controller) newDriverForDevice(device *models.Device, creds deviceCredentialsRequest, settings *models.Settings) (drivers.DriverClient, error) {
	return drivers.NewDriver(drivers.DriverParam{
		Name:     drivers.DeviceFQDN(device.Name, settings.DefaultDomain),
		Platform: strings.ToLower(device.Platform),
		Username: creds.Username,
		Password: creds.Password,
	})
}

func (ctrl *Controller) newNetboxClient(settings *models.Settings) (*netboxtool.NetboxClient, error) {
	return netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
}

// ApiDeviceInterfacesRefresh fetches the live interface descriptions from
// the device itself (via the EOS driver) and overwrites both Netbox's and
// factum's stored interface descriptions with what the device reports.
func (ctrl *Controller) ApiDeviceInterfacesRefresh(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var creds deviceCredentialsRequest
	if err := c.Bind(&creds); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(devices) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	device := devices[0]

	if !isSupportedDriverPlatform(&device) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "driver refresh is not supported for platform " + device.Platform})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	drv, err := ctrl.newDriverForDevice(&device, creds, settings)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	deviceInterfaces, err := drv.GetInterfacesStatus()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to fetch interfaces from device: " + err.Error()})
	}

	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	byName := make(map[string]models.Interface, len(device.Interfaces))
	for _, iface := range device.Interfaces {
		byName[iface.Name] = iface
	}

	for _, devIface := range deviceInterfaces {
		iface, ok := byName[devIface.Name]
		if !ok || iface.Description == devIface.Description {
			continue
		}

		if iface.NetboxID != 0 {
			if err := nb.InterfaceUpdate(int(iface.NetboxID), map[string]any{"description": devIface.Description}); err != nil {
				return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update netbox interface " + iface.Name + ": " + err.Error()})
			}
		}

		if err := ctrl.DB.Model(&models.Interface{}).Where("id = ?", iface.ID).Update("description", devIface.Description).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}

	updated, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(updated) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	return c.JSON(http.StatusOK, updated[0])
}

type interfaceDescriptionUpdate struct {
	ID          uint   `json:"id"`
	Description string `json:"description"`
}

type deviceInterfacesUpdateRequest struct {
	deviceCredentialsRequest
	Interfaces []interfaceDescriptionUpdate `json:"interfaces"`
}

// ApiDeviceInterfacesUpdate pushes edited interface descriptions out to the
// device itself (via the EOS driver), Netbox, and factum's own interface
// table.
func (ctrl *Controller) ApiDeviceInterfacesUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var req deviceInterfacesUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(devices) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	device := devices[0]

	if !isSupportedDriverPlatform(&device) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "driver update is not supported for platform " + device.Platform})
	}

	byID := make(map[uint]models.Interface, len(device.Interfaces))
	for _, iface := range device.Interfaces {
		byID[iface.ID] = iface
	}

	names := make([]string, 0, len(req.Interfaces))
	nbIfaces := make([]*netboxtool.NBInterface, 0, len(req.Interfaces))
	targets := make([]models.Interface, 0, len(req.Interfaces))
	for _, upd := range req.Interfaces {
		iface, ok := byID[upd.ID]
		if !ok {
			continue
		}
		iface.Description = upd.Description
		names = append(names, iface.Name)
		nbIfaces = append(nbIfaces, &netboxtool.NBInterface{Name: iface.Name, Description: iface.Description})
		targets = append(targets, iface)
	}
	if len(targets) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "no matching interfaces to update"})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	drv, err := ctrl.newDriverForDevice(&device, req.deviceCredentialsRequest, settings)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	if err := drv.SetInterfaceDescriptions(names, nbIfaces); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update device interfaces: " + err.Error()})
	}

	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	for _, iface := range targets {
		if iface.NetboxID != 0 {
			if err := nb.InterfaceUpdate(int(iface.NetboxID), map[string]any{"description": iface.Description}); err != nil {
				return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update netbox interface " + iface.Name + ": " + err.Error()})
			}
		}
		if err := ctrl.DB.Model(&models.Interface{}).Where("id = ?", iface.ID).Update("description", iface.Description).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}

	updated, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(updated) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	return c.JSON(http.StatusOK, updated[0])
}

type interfaceVlanUpdate struct {
	ID             uint   `json:"id"`
	SwitchportMode string `json:"switchport_mode"`
	UntaggedVLAN   int    `json:"untagged_vlan"`
	TaggedVLANs    []int  `json:"tagged_vlans"`
}

type deviceInterfacesUpdateVlansRequest struct {
	deviceCredentialsRequest
	Interfaces []interfaceVlanUpdate `json:"interfaces"`
}

// ApiDeviceInterfacesUpdateVlans pushes edited switchport/VLAN config out to
// the device itself (EOS/VRP drivers only - see globalVlanPlatforms) and
// Netbox (creating any new VLAN in Settings.DeviceSyncVlanGroupName's Netbox
// VLAN group as needed, via the same NetboxMgr internal/device-sync uses for
// its own VLAN sync), then refreshes factum's own interface/VLAN cache for
// the device by running the single-device netbox.SyncDB used by the Netbox
// webhook. Unlike ApiDeviceInterfacesUpdate's description path, this fails
// the whole request if any part of it fails - there's no useful "partially
// applied" VLAN edit to leave in place.
func (ctrl *Controller) ApiDeviceInterfacesUpdateVlans(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var req deviceInterfacesUpdateVlansRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(devices) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	device := devices[0]

	if !isGlobalVlanPlatform(&device) {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "vlan configuration is not supported for platform " + device.Platform})
	}

	byID := make(map[uint]models.Interface, len(device.Interfaces))
	for _, iface := range device.Interfaces {
		byID[iface.ID] = iface
	}

	names := make([]string, 0, len(req.Interfaces))
	vlanConfigs := make([]*drivers.VLANConfig, 0, len(req.Interfaces))
	targets := make([]models.Interface, 0, len(req.Interfaces))
	for _, upd := range req.Interfaces {
		iface, ok := byID[upd.ID]
		if !ok {
			continue
		}
		iface.SwitchportMode = upd.SwitchportMode
		iface.UntaggedVLAN = upd.UntaggedVLAN
		iface.TaggedVLANs = upd.TaggedVLANs
		names = append(names, iface.Name)
		vlanConfigs = append(vlanConfigs, &drivers.VLANConfig{
			SwitchportMode: iface.SwitchportMode,
			UntaggedVLAN:   iface.UntaggedVLAN,
			TaggedVLANs:    iface.TaggedVLANs,
		})
		targets = append(targets, iface)
	}
	if len(targets) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "no matching interfaces to update"})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if settings.DeviceSyncVlanGroupName == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "vlan sync is disabled: no Netbox VLAN group configured (see the Device sync admin page)"})
	}

	drv, err := ctrl.newDriverForDevice(&device, req.deviceCredentialsRequest, settings)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	if err := drv.SetInterfaceVLANs(names, vlanConfigs); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update device interfaces: " + err.Error()})
	}

	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	mgr := devicesync.NewNetboxMgr(nb, jobevent.NewSlogReporter())
	if _, err := mgr.EnsureVlanGroup(settings.DeviceSyncVlanGroupName); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to resolve netbox vlan group: " + err.Error()})
	}

	for _, iface := range targets {
		vids := append([]int{}, iface.TaggedVLANs...)
		if iface.UntaggedVLAN != 0 {
			vids = append(vids, iface.UntaggedVLAN)
		}
		for _, vid := range vids {
			if _, err := mgr.EnsureVlan(vid, fmt.Sprintf("VLAN-%d", vid)); err != nil {
				return c.JSON(http.StatusBadGateway, map[string]any{"error": fmt.Sprintf("failed to ensure vlan %d in netbox: %v", vid, err)})
			}
		}
	}

	for _, iface := range targets {
		if iface.NetboxID == 0 {
			continue
		}
		changes, err := mgr.BuildInterfaceVlanChanges(iface.SwitchportMode, iface.UntaggedVLAN, iface.TaggedVLANs)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update netbox interface " + iface.Name + ": " + err.Error()})
		}
		if err := nb.InterfaceUpdate(int(iface.NetboxID), changes); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to update netbox interface " + iface.Name + ": " + err.Error()})
		}
	}

	// Re-pull the device (interfaces + VLAN assignments) from Netbox so
	// factum's cache matches what we just wrote, including any side-effects
	// Netbox applies (mode/qinq field mapping, VID resolution, etc.).
	if err := netbox.SyncDB(ctrl.DB, device.Name, jobevent.NewSlogReporter()); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to refresh factum cache from netbox: " + err.Error()})
	}

	updated, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(updated) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	return c.JSON(http.StatusOK, updated[0])
}
