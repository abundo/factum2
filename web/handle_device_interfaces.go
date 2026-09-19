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
	"gorm.io/gorm"
)

// --------------------------------------------------------------------------
//
//	API: device interfaces (driver-backed refresh/update)
//
//	Refresh also deletes factum/Netbox interfaces that are gone from the
//	device (except device-type template ports). Both endpoints only
//	support platforms whose driver implements
//	GetInterfacesStatus/SetInterfaceDescriptions (internal/drivers.
//	AristaDriver for "eos", internal/drivers.NokiaDriver for "sros"/
//	"sros-md", internal/drivers.IOSXRDriver for "ios-xr",
//	internal/drivers.VrpDriver for "vrp", internal/drivers.CiscoSMBDriver
//	for "ciscosmb"). Device login uses DeviceSyncAuth (exact name, else
//	default), same as service push and factum2-device-sync. factum2-driver-cli
//	still takes --username/--password.
//
// --------------------------------------------------------------------------

var supportedDriverPlatforms = []string{"eos", "sros", "sros-md", "ios-xr", "vrp", "ciscosmb"}

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
// internal/drivers.VrpDriver for "vrp", internal/drivers.CiscoSMBDriver
// for "ciscosmb") - they model VLANs as a device-wide VLAN database that
// switchports reference, unlike ios-xr/sros(-md), which have no
// per-interface global-VLAN concept at all (see
// drivers.Interface.SwitchportMode's doc comment). A separate list from
// supportedDriverPlatforms since the two properties are independent.
var globalVlanPlatforms = []string{"eos", "vrp", "ciscosmb"}

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

func (ctrl *Controller) newDriverForDevice(device *models.Device, creds deviceCredentialsRequest, settings *models.Settings, actor string) (drivers.DriverClient, error) {
	if ctrl.driverFn != nil {
		return ctrl.driverFn(device, creds, settings)
	}
	p := drivers.DriverParam{
		Name:     drivers.DeviceFQDN(device.Name, settings.DefaultDomain),
		Platform: strings.ToLower(device.Platform),
		Username: creds.Username,
		Password: creds.Password,
	}
	return drivers.NewDriver(drivers.WithActor(p, actor))
}

// deviceSyncCredentials is the username/password internal/device-sync would
// use for deviceName: an exact DeviceSyncAuth row if one exists, otherwise
// the literal "default" row. GUI device I/O (service push/delete/unrealize,
// cfgmgmt rebind, interface refresh/update, VLAN push) uses this instead of
// credentials typed in the browser.
func (ctrl *Controller) deviceSyncCredentials(deviceName string) (deviceCredentialsRequest, error) {
	var rows []models.DeviceSyncAuth
	if err := ctrl.DB.Where("name IN ?", []string{deviceName, "default"}).Find(&rows).Error; err != nil {
		return deviceCredentialsRequest{}, err
	}
	var exact, fallback *models.DeviceSyncAuth
	for i := range rows {
		switch rows[i].Name {
		case deviceName:
			exact = &rows[i]
		case "default":
			fallback = &rows[i]
		}
	}
	auth := exact
	if auth == nil {
		auth = fallback
	}
	if auth == nil {
		return deviceCredentialsRequest{}, fmt.Errorf("no device-sync credentials for %q (and no default)", deviceName)
	}
	if auth.Username == "" || auth.Password == "" {
		return deviceCredentialsRequest{}, fmt.Errorf("device-sync credentials %q are incomplete", auth.Name)
	}
	return deviceCredentialsRequest{Username: auth.Username, Password: auth.Password}, nil
}

// sessionUserLabel is the logged-in operator's display name (Name, else
// Username) for device commit comments. Empty when the caller is a service
// token rather than a session user.
func sessionUserLabel(c *echo.Context) string {
	user, ok := c.Get("user").(models.User)
	if !ok {
		return ""
	}
	if name := strings.TrimSpace(user.Name); name != "" {
		return name
	}
	return strings.TrimSpace(user.Username)
}

// serviceCommitComment is the text a GUI-driven service push or teardown
// records on the device (EOS session description, SR OS/IOS-XR commit
// comment), e.g. "factum push CN00042 by Alice Andersson".
func serviceCommitComment(c *echo.Context, serviceID, action string) string {
	id := strings.TrimSpace(serviceID)
	if id == "" {
		id = "service"
	}
	who := sessionUserLabel(c)
	if who == "" {
		return fmt.Sprintf("factum %s %s", action, id)
	}
	return fmt.Sprintf("factum %s %s by %s", action, id, who)
}

func (ctrl *Controller) newNetboxClient(settings *models.Settings) (*netboxtool.NetboxClient, error) {
	return netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
}

// interfaceRefreshNetbox is the Netbox surface GUI interface refresh uses:
// description patches, deletes of interfaces the device no longer has, and
// device-type templates so those ports are not pruned.
type interfaceRefreshNetbox interface {
	InterfaceUpdate(interfaceID int, changes map[string]any) error
	InterfaceDelete(interfaceID int) error
	GetDeviceType(manufacturer, model string) (*netboxtool.NetboxDeviceTypeDetail, error)
}

func (ctrl *Controller) interfaceRefreshNetboxClient(settings *models.Settings) (interfaceRefreshNetbox, error) {
	if ctrl.interfaceNetboxFn != nil {
		return ctrl.interfaceNetboxFn(settings)
	}
	return ctrl.newNetboxClient(settings)
}

// ApiDeviceInterfacesRefresh fetches live interfaces from the device and
// reconciles factum/Netbox against that list: descriptions of matching
// names are overwritten with what the device reports, and interfaces that
// no longer exist on the device are deleted (device-type template ports
// are kept, matching internal/device-sync's interfacesDelete).
func (ctrl *Controller) ApiDeviceInterfacesRefresh(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
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
	creds, err := ctrl.deviceSyncCredentials(device.Name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	drv, err := ctrl.newDriverForDevice(&device, creds, settings, sessionUserLabel(c))
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	deviceInterfaces, err := drv.GetInterfacesStatus()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to fetch interfaces from device: " + err.Error()})
	}

	nb, err := ctrl.interfaceRefreshNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	templateTypes, templatesKnown := loadRefreshTemplateTypes(ctrl.DB, nb, device)

	if err := applyLiveInterfaceRefresh(ctrl.DB, nb, device, deviceInterfaces, templateTypes, templatesKnown); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	updated, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(updated) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	return c.JSON(http.StatusOK, updated[0])
}

func loadRefreshTemplateTypes(db *gorm.DB, nb interfaceRefreshNetbox, device models.Device) (map[string]string, bool) {
	if device.DeviceTypeID != 0 && db != nil {
		var tmpls []models.InterfaceTemplate
		if err := db.Where("device_type_id = ?", device.DeviceTypeID).Find(&tmpls).Error; err == nil && len(tmpls) > 0 {
			types := make(map[string]string, len(tmpls))
			for _, tmpl := range tmpls {
				types[tmpl.Name] = tmpl.Type
			}
			return types, true
		}
	}
	if device.Manufacturer == "" || device.ModelName == "" {
		return nil, false
	}
	dt, err := nb.GetDeviceType(device.Manufacturer, device.ModelName)
	if err != nil || dt == nil {
		return nil, false
	}
	types := make(map[string]string, len(dt.Interfaces))
	for _, tmpl := range dt.Interfaces {
		types[tmpl.Name] = tmpl.Type
	}
	return types, true
}

// applyLiveInterfaceRefresh writes live descriptions onto matching stored
// interfaces and removes stored interfaces the device no longer has.
// When templatesKnown is false (device type missing/unreadable), only
// virtual/subinterfaces are deleted so a failed template lookup cannot
// strip physical ports that Netbox would recreate from the device type.
func applyLiveInterfaceRefresh(db *gorm.DB, nb interfaceRefreshNetbox, device models.Device, live []*netboxtool.NBInterface, templateTypes map[string]string, templatesKnown bool) error {
	byName := make(map[string]models.Interface, len(device.Interfaces))
	for _, iface := range device.Interfaces {
		byName[iface.Name] = iface
	}

	liveNames := make(map[string]struct{}, len(live))
	for _, devIface := range live {
		liveNames[devIface.Name] = struct{}{}
		iface, ok := byName[devIface.Name]
		if !ok || iface.Description == devIface.Description {
			continue
		}
		if iface.NetboxID != 0 {
			if err := nb.InterfaceUpdate(int(iface.NetboxID), map[string]any{"description": devIface.Description}); err != nil {
				return fmt.Errorf("failed to update netbox interface %s: %w", iface.Name, err)
			}
		}
		if err := db.Model(&models.Interface{}).Where("id = ?", iface.ID).Update("description", devIface.Description).Error; err != nil {
			return err
		}
	}

	// An empty live list is treated as a fetch/parse failure, not a
	// device with no interfaces: pruning would wipe every stored row.
	if len(live) == 0 {
		return nil
	}

	for _, iface := range device.Interfaces {
		if _, ok := liveNames[iface.Name]; ok {
			continue
		}
		if !shouldDeleteMissingInterface(iface, templateTypes, templatesKnown) {
			continue
		}
		if iface.NetboxID != 0 {
			if err := nb.InterfaceDelete(int(iface.NetboxID)); err != nil {
				return fmt.Errorf("failed to delete netbox interface %s: %w", iface.Name, err)
			}
		}
		if err := netbox.DeleteFactumInterface(db, iface.ID); err != nil {
			return fmt.Errorf("failed to delete factum interface %s: %w", iface.Name, err)
		}
	}
	return nil
}

func shouldDeleteMissingInterface(iface models.Interface, templateTypes map[string]string, templatesKnown bool) bool {
	if _, ok := templateTypes[iface.Name]; ok {
		return false
	}
	if templatesKnown {
		return true
	}
	// No device-type map: only drop subinterfaces / virtual ports.
	// Physical names (Ethernet1, Management1) stay until templates are known.
	if strings.EqualFold(iface.Type, "virtual") {
		return true
	}
	return strings.Contains(iface.Name, ".")
}

type interfaceDescriptionUpdate struct {
	ID          uint   `json:"id"`
	Description string `json:"description"`
}

type deviceInterfacesUpdateRequest struct {
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
	creds, err := ctrl.deviceSyncCredentials(device.Name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	drv, err := ctrl.newDriverForDevice(&device, creds, settings, sessionUserLabel(c))
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
	Interfaces []interfaceVlanUpdate `json:"interfaces"`
}

// ApiDeviceInterfacesUpdateVlans pushes edited switchport/VLAN config out to
// the device itself (EOS/VRP/Cisco SMB drivers only - see globalVlanPlatforms) and
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
	creds, err := ctrl.deviceSyncCredentials(device.Name)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	drv, err := ctrl.newDriverForDevice(&device, creds, settings, sessionUserLabel(c))
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
	mgr := devicesync.NewNetboxMgr(nb, jobevent.NewSlogReporter("source", "netbox"))
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
	if err := netbox.SyncDB(ctrl.DB, device.Name, jobevent.NewSlogReporter("source", "netbox")); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to refresh factum cache from netbox: " + err.Error()})
	}

	updated, err := fetchDevices(c.Request().Context(), ctrl.DB, []uint{id})
	if err != nil || len(updated) == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "device not found"})
	}
	return c.JSON(http.StatusOK, updated[0])
}
