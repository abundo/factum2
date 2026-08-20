package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --------------------------------------------------------------------------
//
//	API: ELINE endpoint provisioning (Netbox storage only - pushing config
//	to the live device is a separate follow-up, see internal/drivers)
//
// --------------------------------------------------------------------------

type ServiceElineDTO struct {
	EndpointADeviceID    uint `json:"endpoint_a_device_id"`
	EndpointAInterfaceID uint `json:"endpoint_a_interface_id"`
	EndpointAVlan        int  `json:"endpoint_a_vlan"`

	EndpointBDeviceID    uint `json:"endpoint_b_device_id"`
	EndpointBInterfaceID uint `json:"endpoint_b_interface_id"`
	EndpointBVlan        int  `json:"endpoint_b_vlan"`
}

// serviceIDDigitsRe matches the numeric run in a ServiceID (e.g. "1234" in
// "CN1234", "00001" in "CN00001") - used by pseudowireIDFromServiceID.
var serviceIDDigitsRe = regexp.MustCompile(`\d+`)

// pseudowireCategoryRe matches a ServiceID's leading two-letter category,
// independent of how many digits follow - deliberately looser than
// serviceIDRe (web/handler_service.go), which requires exactly 5 digits and
// is about validating new auto-assigned IDs, not about tolerating older/
// free-text ones the way pseudowireIDFromServiceID's own digit extraction
// already does.
var pseudowireCategoryRe = regexp.MustCompile(`^([A-Z]{2})`)

// pseudowireIDPrefix maps a service's category (its ServiceID's leading two
// letters) to the numeric prefix pseudowireIDFromServiceID uses, so
// pseudowire IDs derived from different categories' ServiceIDs can't
// collide with each other. Only CN/CI carry ServiceType == "ELINE" today
// (capacityCategories, web/handler_service.go) - VL/VI/LF/LI get a prefix
// added here if/when they ever need one.
var pseudowireIDPrefix = map[string]string{
	"CN": "10",
	"CI": "11",
}

// pseudowireIDFromServiceID derives a Netbox L2VPN "identifier" (pseudowire
// ID) from a service's ServiceID: the numeric part, left-padded to 5
// digits, prefixed with its category's pseudowireIDPrefix - e.g.
// "CN1234" -> 1234 -> "01234" -> 1001234, "CI1234" -> 1101234.
func pseudowireIDFromServiceID(serviceID string) (int, error) {
	var category string
	if m := pseudowireCategoryRe.FindStringSubmatch(serviceID); m != nil {
		category = m[1]
	}
	prefix, ok := pseudowireIDPrefix[category]
	if !ok {
		return 0, fmt.Errorf("service ID %q: no pseudowire ID prefix for category %q", serviceID, category)
	}
	digits := serviceIDDigitsRe.FindString(serviceID)
	if digits == "" {
		return 0, fmt.Errorf("service ID %q has no numeric part", serviceID)
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("service ID %q: %w", serviceID, err)
	}
	return strconv.Atoi(fmt.Sprintf("%s%05d", prefix, n))
}

// isPhysicalInterfaceType reports whether t (a Netbox dcim interface type,
// e.g. "1000base-t") is a real port rather than a subinterface/LAG - same
// split as eosInterfaceType (internal/drivers/driver_arista_eos.go), which
// classifies "virtual" and "lag" as non-physical.
func isPhysicalInterfaceType(t string) bool {
	return t != "" && t != "virtual" && t != "lag"
}

// eLineEndpoint is one resolved and validated side (A or B) of an ELINE
// service, checked against factum's device/interface tables.
type eLineEndpoint struct {
	device *models.Device
	iface  models.Interface
	vlan   int
}

func resolveELineEndpoint(devices map[uint]models.Device, deviceID, interfaceID uint, vlan int, label string) (*eLineEndpoint, error) {
	device, ok := devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("endpoint %s: device not found", label)
	}
	var iface models.Interface
	found := false
	for _, i := range device.Interfaces {
		if i.ID == interfaceID {
			iface = i
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("endpoint %s: interface not found on device %q", label, device.Name)
	}
	if !isPhysicalInterfaceType(iface.Type) {
		return nil, fmt.Errorf("endpoint %s: interface %q is not a physical interface", label, iface.Name)
	}
	if vlan < 1 || vlan > 4094 {
		return nil, fmt.Errorf("endpoint %s: VLAN must be between 1 and 4094", label)
	}
	return &eLineEndpoint{device: &device, iface: iface, vlan: vlan}, nil
}

// eLineReconcileResult is what reconcileELineSubinterface produces for one
// side, to be persisted on the Service row and used to create the new
// L2VPN termination.
type eLineReconcileResult struct {
	subinterfaceNetboxID uint
}

// elineInterfaceDescription is the description written on every ELINE
// subinterface (Netbox, factum's interfaces table, and the live device
// config via ELINEIntent.Description) - "ID=<ServiceID> <customer name>".
func elineInterfaceDescription(serviceID, customerName string) string {
	return fmt.Sprintf("ID=%s %s", serviceID, customerName)
}

// deleteFactumInterfaceByNetboxID removes the factum interfaces row (and its
// addresses/tags/connections) that mirrors a Netbox interface, if any.
// Looked up by netbox_id alone so an endpoint move (subinterface was on a
// different device) still cleans up. Best-effort: a missing row is fine
// (e.g. never written, or already cleaned up); real DB errors are logged
// rather than failing the Netbox-side reconcile, matching the
// log-and-continue style of the Netbox deletes.
func deleteFactumInterfaceByNetboxID(db *gorm.DB, netboxID uint, label string) {
	if netboxID == 0 {
		return
	}
	var iface models.Interface
	err := db.Where("netbox_id = ?", netboxID).First(&iface).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			slog.Warn("eline: failed to look up old factum subinterface", "endpoint", label, "netbox_id", netboxID, "err", err)
		}
		return
	}
	if err := db.Where("interface_id = ?", iface.ID).Delete(&models.Address{}).Error; err != nil {
		slog.Warn("eline: failed to delete addresses for old factum subinterface", "endpoint", label, "err", err)
	}
	if err := db.Where("interface_id = ?", iface.ID).Delete(&models.Tag{}).Error; err != nil {
		slog.Warn("eline: failed to delete tags for old factum subinterface", "endpoint", label, "err", err)
	}
	if err := db.Where("interface_a_id = ? OR interface_b_id = ?", iface.ID, iface.ID).Delete(&models.Connection{}).Error; err != nil {
		slog.Warn("eline: failed to delete connections for old factum subinterface", "endpoint", label, "err", err)
	}
	if err := db.Delete(&models.Interface{}, iface.ID).Error; err != nil {
		slog.Warn("eline: failed to delete old factum subinterface", "endpoint", label, "err", err)
	}
}

// upsertFactumSubinterface records the Netbox-created ELINE subinterface in
// factum's interfaces table so the device's interface list shows it (with
// description) without waiting for a full Netbox sync. Matched on
// (device_id, netbox_id) - same unique key syncInterfaces uses.
func upsertFactumSubinterface(db *gorm.DB, deviceID, netboxID, parentNetboxID uint, name, description string) error {
	iface := models.Interface{
		DeviceID:    deviceID,
		NetboxID:    netboxID,
		Name:        name,
		Description: description,
		Type:        "virtual",
		ParentID:    parentNetboxID,
		Enabled:     true,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_id"}, {Name: "netbox_id"}},
		UpdateAll: true,
	}).Create(&iface).Error
}

// reconcileELineSubinterface ensures a "<physical-interface-name>.<vlan>"
// subinterface exists in Netbox for ep, deleting the previously recorded
// subinterface/termination first if one was already provisioned for this
// side - this makes ApiServiceElineUpdate idempotent across edits (VLAN
// change, swapped interface), not just the first provisioning call.
// description is written on the Netbox interface and mirrored into factum's
// interfaces table (elineInterfaceDescription). Deletes are best-effort: if
// the old objects are already gone (e.g. removed by hand in Netbox), it
// logs and continues rather than blocking a legitimate edit.
func reconcileELineSubinterface(db *gorm.DB, nb *netboxtool.NetboxClient, ep *eLineEndpoint, prevSubinterfaceNetboxID, prevTerminationNetboxID uint, description, label string) (*eLineReconcileResult, error) {
	if prevTerminationNetboxID != 0 {
		if err := nb.DeleteL2VPNTermination(prevTerminationNetboxID); err != nil {
			slog.Warn("eline: failed to delete old l2vpn termination", "endpoint", label, "err", err)
		}
	}
	if prevSubinterfaceNetboxID != 0 {
		if err := nb.InterfaceDelete(int(prevSubinterfaceNetboxID)); err != nil {
			slog.Warn("eline: failed to delete old subinterface", "endpoint", label, "err", err)
		}
		deleteFactumInterfaceByNetboxID(db, prevSubinterfaceNetboxID, label)
	}

	desiredName := fmt.Sprintf("%s.%d", ep.iface.Name, ep.vlan)
	created, err := nb.CreateInterfaceWithOptions(ep.device.NetboxID, desiredName, map[string]any{
		"type":        "virtual",
		"parent":      ep.iface.NetboxID,
		"description": description,
	})
	if err != nil {
		return nil, fmt.Errorf("endpoint %s: create subinterface %q: %w", label, desiredName, err)
	}
	if err := upsertFactumSubinterface(db, ep.device.ID, created.ID, ep.iface.NetboxID, desiredName, description); err != nil {
		return nil, fmt.Errorf("endpoint %s: persist subinterface %q in factum: %w", label, desiredName, err)
	}
	return &eLineReconcileResult{subinterfaceNetboxID: created.ID}, nil
}

// ApiServiceElineUpdate provisions (or re-provisions, on later edits) the
// device/interface/VLAN endpoints of an ELINE service: it creates a
// per-VLAN subinterface on each side (with description
// elineInterfaceDescription) and an ipam.L2VPN (type "evpl") in Netbox with
// two L2VPNTerminations, assigned to the service's customer's Netbox tenant
// (netbox.FindOrCreateTenant), mirrors each new subinterface into factum's
// interfaces table, then persists the choice on the Service row. It does
// NOT push any config to the live devices - that's a separate follow-up
// (internal/drivers).
func (ctrl *Controller) ApiServiceElineUpdate(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var service models.Service
	if err := ctrl.DB.First(&service, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if service.ServiceType != "ELINE" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service is not an ELINE service"})
	}
	var customer models.Customer
	if err := ctrl.DB.First(&customer, service.CustomerID).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service has no valid customer"})
	}

	var dto ServiceElineDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if dto.EndpointADeviceID == dto.EndpointBDeviceID && dto.EndpointAInterfaceID == dto.EndpointBInterfaceID {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "endpoint A and endpoint B must not be the same device/interface"})
	}

	ctx := c.Request().Context()
	devices, err := fetchDevices(ctx, ctrl.DB, []uint{dto.EndpointADeviceID, dto.EndpointBDeviceID})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	devicesByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devicesByID[d.ID] = d
	}

	epA, err := resolveELineEndpoint(devicesByID, dto.EndpointADeviceID, dto.EndpointAInterfaceID, dto.EndpointAVlan, "A")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	epB, err := resolveELineEndpoint(devicesByID, dto.EndpointBDeviceID, dto.EndpointBInterfaceID, dto.EndpointBVlan, "B")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	pseudowireID, err := pseudowireIDFromServiceID(service.ServiceID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	tenant, err := netbox.FindOrCreateTenant(nb, customer)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to resolve netbox tenant for customer: " + err.Error()})
	}

	description := elineInterfaceDescription(service.ServiceID, customer.Name)
	resA, err := reconcileELineSubinterface(ctrl.DB, nb, epA, service.EndpointASubinterfaceNetboxID, service.EndpointATerminationNetboxID, description, "A")
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
	resB, err := reconcileELineSubinterface(ctrl.DB, nb, epB, service.EndpointBSubinterfaceNetboxID, service.EndpointBTerminationNetboxID, description, "B")
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": err.Error()})
	}

	l2vpnID := service.L2VPNNetboxID
	if l2vpnID == 0 {
		created, err := nb.CreateL2VPN(service.ServiceID, strings.ToLower(service.ServiceID), "evpl", pseudowireID)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to create netbox l2vpn: " + err.Error()})
		}
		l2vpnID = created.NetboxID
	}
	if err := nb.UpdateL2VPN(l2vpnID, map[string]any{"tenant": tenant.NetboxID}); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to set netbox l2vpn tenant: " + err.Error()})
	}

	termA, err := nb.CreateL2VPNTermination(l2vpnID, resA.subinterfaceNetboxID)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to create l2vpn termination A: " + err.Error()})
	}
	termB, err := nb.CreateL2VPNTermination(l2vpnID, resB.subinterfaceNetboxID)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"error": "failed to create l2vpn termination B: " + err.Error()})
	}

	updates := map[string]any{
		"endpoint_a_device_id":              dto.EndpointADeviceID,
		"endpoint_a_interface_id":           dto.EndpointAInterfaceID,
		"endpoint_a_vlan":                   dto.EndpointAVlan,
		"endpoint_a_subinterface_netbox_id": resA.subinterfaceNetboxID,
		"endpoint_a_termination_netbox_id":  termA.NetboxID,
		"endpoint_b_device_id":              dto.EndpointBDeviceID,
		"endpoint_b_interface_id":           dto.EndpointBInterfaceID,
		"endpoint_b_vlan":                   dto.EndpointBVlan,
		"endpoint_b_subinterface_netbox_id": resB.subinterfaceNetboxID,
		"endpoint_b_termination_netbox_id":  termB.NetboxID,
		"pseudowire_id":                     pseudowireID,
		"l2_vpn_netbox_id":                  l2vpnID,
	}
	if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	var updated models.Service
	if err := ctrl.DB.First(&updated, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, updated)
}

// --------------------------------------------------------------------------
//
//	API: ELINE device provisioning - pushes an already-NetBox-provisioned
//	ELINE service's config out to its endpoint device(s). Deliberately a
//	separate, explicit action from ApiServiceElineUpdate above: this one
//	mutates live devices, and a cross-device push has no way to roll back
//	one side if the other fails, so it shouldn't happen silently as a side
//	effect of the NetBox-only save.
//
// --------------------------------------------------------------------------

// elineDefaultMTU/elineDefaultControlWord are hardcoded for v1 rather than
// fields on Service - matches the real fixture in
// driver_arista_eos_config_test.go. Worth revisiting as a per-service field
// if any service ever needs a different value.
const (
	elineDefaultMTU         = 9100
	elineDefaultControlWord = true
)

// deviceLoopbackIfaceName returns the interface name a device's platform
// uses for its loopback/system address - "Loopback0" everywhere except SR
// OS, whose loopback is always named "system" and, unlike EOS's
// operator-chosen "Loopback0" convention, can't be renamed (confirmed: not
// a lab-specific naming choice, SR OS names it "system" universally).
func deviceLoopbackIfaceName(device *models.Device) string {
	if strings.EqualFold(device.Platform, "sros") || strings.EqualFold(device.Platform, "sros-md") {
		return "system"
	}
	return "Loopback0"
}

// deviceLoopback0Address returns device's loopback interface address
// (deviceLoopbackIfaceName), stripped of any CIDR suffix - used as the
// far-end IP for an MPLS LDP pseudowire/spoke-sdp targeting that device.
// Requires device.Interfaces (and each interface's Addresses) to already be
// populated, i.e. device must come from fetchDevices rather than a bare
// ctrl.DB.First.
func deviceLoopback0Address(device *models.Device) (string, error) {
	ifaceName := deviceLoopbackIfaceName(device)
	for _, iface := range device.Interfaces {
		if iface.Name != ifaceName || len(iface.Addresses) == 0 {
			continue
		}
		addr, _, _ := strings.Cut(iface.Addresses[0].Address, "/")
		return addr, nil
	}
	return "", fmt.Errorf("device %q has no %s address", device.Name, ifaceName)
}

// ApiServiceElinePushResult is one endpoint device's outcome, returned
// per-device since a cross-device push can partially fail with no way to
// roll back the other side.
type ApiServiceElinePushResult struct {
	Device string `json:"device"`
	Error  string `json:"error,omitempty"`
}

// applyELINEToDevice opens a driver for device, checks it can apply an
// ELINE, and does so - returning the outcome rather than an error, so
// ApiServiceElinePush can report both endpoints' results even when only one
// fails.
func (ctrl *Controller) applyELINEToDevice(device *models.Device, creds deviceCredentialsRequest, settings *models.Settings, intent *drivers.ELINEIntent) ApiServiceElinePushResult {
	result := ApiServiceElinePushResult{Device: device.Name}

	if !isSupportedDriverPlatform(device) {
		result.Error = "driver provisioning is not supported for platform " + device.Platform
		return result
	}
	drv, err := ctrl.newDriverForDevice(device, creds, settings)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	applier, ok := drv.(drivers.ELINEApplier)
	if !ok {
		result.Error = "ELINE provisioning is not yet supported for platform " + device.Platform
		return result
	}
	if err := applier.ApplyELINE(intent); err != nil {
		result.Error = err.Error()
	}
	return result
}

// removeELINEFromDevice mirrors applyELINEToDevice for the teardown-only
// path: device is no longer one of the service's current endpoints (its
// side moved elsewhere), so its stale pseudowire/patch/subinterfaces must
// be removed without configuring anything new.
func (ctrl *Controller) removeELINEFromDevice(device *models.Device, creds deviceCredentialsRequest, settings *models.Settings, removal *drivers.ELINERemoval) ApiServiceElinePushResult {
	result := ApiServiceElinePushResult{Device: device.Name}

	if !isSupportedDriverPlatform(device) {
		result.Error = "driver provisioning is not supported for platform " + device.Platform
		return result
	}
	drv, err := ctrl.newDriverForDevice(device, creds, settings)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	remover, ok := drv.(drivers.ELINERemover)
	if !ok {
		result.Error = "ELINE provisioning is not yet supported for platform " + device.Platform
		return result
	}
	if err := remover.RemoveELINE(removal); err != nil {
		result.Error = err.Error()
	}
	return result
}

// removeELINEServiceFromDevices tears down service's ELINE config on every
// device it was actually pushed to (AppliedEndpointX*, not EndpointX* -
// the latter just reflects what's provisioned in Netbox, which may never
// have been pushed at all) - used by ApiServiceDelete's optional "remove
// from device" cleanup. Returns (nil, nil) if the service was never
// pushed to any device, so the caller doesn't need to special-case that
// itself.
func (ctrl *Controller) removeELINEServiceFromDevices(c *echo.Context, service *models.Service, username, password string) ([]ApiServiceElinePushResult, error) {
	if service.AppliedEndpointADeviceID == 0 && service.AppliedEndpointBDeviceID == 0 {
		return nil, nil
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("device credentials are required to remove ELINE config from device(s)")
	}
	creds := deviceCredentialsRequest{Username: username, Password: password}

	// Group by device ID first, so a same-device ELINE (both sides applied
	// to the same device) gets one RemoveELINE call carrying both stale
	// subinterfaces, not two separate driver sessions.
	subsByDevice := map[uint][]drivers.ELINEStaleSubinterface{}
	var deviceIDs []uint
	addSide := func(deviceID uint, iface string, vlan int) {
		if deviceID == 0 {
			return
		}
		if _, ok := subsByDevice[deviceID]; !ok {
			deviceIDs = append(deviceIDs, deviceID)
		}
		subsByDevice[deviceID] = append(subsByDevice[deviceID], drivers.ELINEStaleSubinterface{Iface: iface, VLAN: vlan})
	}
	addSide(service.AppliedEndpointADeviceID, service.AppliedEndpointAIface, service.AppliedEndpointAVlan)
	addSide(service.AppliedEndpointBDeviceID, service.AppliedEndpointBIface, service.AppliedEndpointBVlan)

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, deviceIDs)
	if err != nil {
		return nil, err
	}
	devicesByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devicesByID[d.ID] = d
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return nil, err
	}

	results := make([]ApiServiceElinePushResult, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		device, ok := devicesByID[deviceID]
		if !ok {
			results = append(results, ApiServiceElinePushResult{
				Device: fmt.Sprintf("device #%d", deviceID),
				Error:  "device no longer exists in factum - stale config was not removed",
			})
			continue
		}
		results = append(results, ctrl.removeELINEFromDevice(&device, creds, settings, &drivers.ELINERemoval{
			Name:               service.ServiceID,
			StaleSubinterfaces: subsByDevice[deviceID],
		}))
	}
	return results, nil
}

// removeELINEServiceFromNetbox tears down every Netbox object
// ApiServiceElineUpdate created for service: both sides' L2VPN
// terminations and subinterfaces, then the L2VPN itself - used by
// ApiServiceDelete's optional "remove from netbox" cleanup. Unlike
// reconcileELineSubinterface's best-effort deletes (log-and-continue,
// since a legitimate re-provisioning edit shouldn't be blocked by stale
// bookkeeping), this fails fast on the first error: ApiServiceDelete uses
// success here as its gate for actually deleting the row, so a real
// failure (as opposed to "already removed by hand", which the operator
// signals by simply unchecking this option) must stop the delete rather
// than be swallowed.
func (ctrl *Controller) removeELINEServiceFromNetbox(service *models.Service) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return err
	}
	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return err
	}

	if service.EndpointATerminationNetboxID != 0 {
		if err := nb.DeleteL2VPNTermination(service.EndpointATerminationNetboxID); err != nil {
			return fmt.Errorf("endpoint A termination: %w", err)
		}
	}
	if service.EndpointBTerminationNetboxID != 0 {
		if err := nb.DeleteL2VPNTermination(service.EndpointBTerminationNetboxID); err != nil {
			return fmt.Errorf("endpoint B termination: %w", err)
		}
	}
	if service.EndpointASubinterfaceNetboxID != 0 {
		if err := nb.InterfaceDelete(int(service.EndpointASubinterfaceNetboxID)); err != nil {
			return fmt.Errorf("endpoint A subinterface: %w", err)
		}
		deleteFactumInterfaceByNetboxID(ctrl.DB, service.EndpointASubinterfaceNetboxID, "A")
	}
	if service.EndpointBSubinterfaceNetboxID != 0 {
		if err := nb.InterfaceDelete(int(service.EndpointBSubinterfaceNetboxID)); err != nil {
			return fmt.Errorf("endpoint B subinterface: %w", err)
		}
		deleteFactumInterfaceByNetboxID(ctrl.DB, service.EndpointBSubinterfaceNetboxID, "B")
	}
	if service.L2VPNNetboxID != 0 {
		if err := nb.DeleteL2VPN(service.L2VPNNetboxID); err != nil {
			return fmt.Errorf("l2vpn: %w", err)
		}
	}
	return nil
}

// elineAppliedState is one endpoint's last-successfully-pushed live state
// (models.Service's AppliedEndpointX* fields) - a zero DeviceID means this
// side has never been pushed yet.
type elineAppliedState struct {
	DeviceID uint
	Iface    string
	VLAN     int
}

// elineStale is what elineComputeStale found: a subinterface a previous
// push left on DeviceID that the current push no longer wants there.
// Abandoned distinguishes two cases the caller must handle differently:
// DeviceID is still one of this push's two endpoint devices ("merge" -
// fold Sub into that device's own ApplyELINE call, in the same atomic
// session as the rest of its config), or it isn't ("abandoned" - that
// device needs its own teardown-only RemoveELINE call, since neither
// endpoint is on it anymore this push).
type elineStale struct {
	DeviceID  uint
	Sub       drivers.ELINEStaleSubinterface
	Abandoned bool
}

// elineComputeStale compares applied (an endpoint's last-pushed live
// state) against its newly desired device/interface/VLAN, reporting the
// subinterface a previous push left behind if its location changed - nil
// if applied is the zero value (never pushed before) or nothing changed.
// currentDeviceIDs is the set of devices this push is about to configure
// (both endpoints, or one for a same-device service), used to decide
// Abandoned.
func elineComputeStale(applied elineAppliedState, desiredDeviceID uint, desiredIface string, desiredVLAN int, currentDeviceIDs map[uint]bool) *elineStale {
	if applied.DeviceID == 0 {
		return nil
	}
	if applied.DeviceID == desiredDeviceID && applied.Iface == desiredIface && applied.VLAN == desiredVLAN {
		return nil
	}
	return &elineStale{
		DeviceID:  applied.DeviceID,
		Sub:       drivers.ELINEStaleSubinterface{Iface: applied.Iface, VLAN: applied.VLAN},
		Abandoned: !currentDeviceIDs[applied.DeviceID],
	}
}

// ApiServiceElinePush pushes an already-provisioned ELINE service's config
// to its endpoint device(s) - see ApiServiceElineUpdate for the NetBox-only
// step that must run first (it's what populates EndpointADeviceID/
// PseudowireID/etc. below).
func (ctrl *Controller) ApiServiceElinePush(c *echo.Context) error {
	id, err := echo.PathParam[uint](c, "id")
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
	}

	var service models.Service
	if err := ctrl.DB.First(&service, id).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"error": "Record not found"})
	}
	if service.ServiceType != "ELINE" {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service is not an ELINE service"})
	}
	if service.EndpointADeviceID == 0 || service.EndpointBDeviceID == 0 || service.PseudowireID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service has not been provisioned yet - PUT .../eline must run first"})
	}
	var customer models.Customer
	if err := ctrl.DB.First(&customer, service.CustomerID).Error; err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "service has no valid customer"})
	}

	var creds deviceCredentialsRequest
	if err := c.Bind(&creds); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	ctx := c.Request().Context()
	// Also fetch whichever device(s) the last successful push actually
	// landed on (AppliedEndpointX*), even if a since-edited service no
	// longer points at them - that's exactly the device a stale
	// pseudowire/patch/subinterface might need removing from below.
	fetchIDs := []uint{service.EndpointADeviceID, service.EndpointBDeviceID}
	if service.AppliedEndpointADeviceID != 0 {
		fetchIDs = append(fetchIDs, service.AppliedEndpointADeviceID)
	}
	if service.AppliedEndpointBDeviceID != 0 {
		fetchIDs = append(fetchIDs, service.AppliedEndpointBDeviceID)
	}
	devices, err := fetchDevices(ctx, ctrl.DB, fetchIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	devicesByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devicesByID[d.ID] = d
	}

	epA, err := resolveELineEndpoint(devicesByID, service.EndpointADeviceID, service.EndpointAInterfaceID, service.EndpointAVlan, "A")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	epB, err := resolveELineEndpoint(devicesByID, service.EndpointBDeviceID, service.EndpointBInterfaceID, service.EndpointBVlan, "B")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}

	description := elineInterfaceDescription(service.ServiceID, customer.Name)
	sameDevice := service.EndpointADeviceID == service.EndpointBDeviceID

	var intentA, intentB *drivers.ELINEIntent
	if sameDevice {
		intentA = &drivers.ELINEIntent{
			Name:             service.ServiceID,
			Description:      description,
			LocalIface:       epA.iface.Name,
			LocalVLAN:        epA.vlan,
			PeerLocalIface:   epB.iface.Name,
			PeerLocalVLAN:    epB.vlan,
			ServiceNumericID: service.PseudowireID,
		}
	} else {
		loopbackA, err := deviceLoopback0Address(epA.device)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": "endpoint A: " + err.Error()})
		}
		loopbackB, err := deviceLoopback0Address(epB.device)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]any{"error": "endpoint B: " + err.Error()})
		}
		intentA = &drivers.ELINEIntent{
			Name:             service.ServiceID,
			Description:      description,
			LocalIface:       epA.iface.Name,
			LocalVLAN:        epA.vlan,
			ServiceNumericID: service.PseudowireID,
			Remote: &drivers.ELINERemotePeer{
				NeighborIP:   loopbackB,
				PseudowireID: service.PseudowireID,
				MTU:          elineDefaultMTU,
				ControlWord:  elineDefaultControlWord,
				DeviceName:   epB.device.Name,
				RemoteIface:  epB.iface.Name,
				RemoteVLAN:   epB.vlan,
			},
		}
		intentB = &drivers.ELINEIntent{
			Name:             service.ServiceID,
			Description:      description,
			LocalIface:       epB.iface.Name,
			LocalVLAN:        epB.vlan,
			ServiceNumericID: service.PseudowireID,
			Remote: &drivers.ELINERemotePeer{
				NeighborIP:   loopbackA,
				PseudowireID: service.PseudowireID,
				MTU:          elineDefaultMTU,
				ControlWord:  elineDefaultControlWord,
				DeviceName:   epA.device.Name,
				RemoteIface:  epA.iface.Name,
				RemoteVLAN:   epA.vlan,
			},
		}
	}

	// Detect whether either side's device/interface/VLAN moved since the
	// last successful push, and route the stale subinterface it left
	// behind accordingly: merged into the relevant device's own
	// ApplyELINE call below if that device is still one of this push's
	// endpoints, or queued for a separate teardown-only RemoveELINE call
	// if it's been abandoned entirely (see elineComputeStale).
	currentDeviceIDs := map[uint]bool{epA.device.ID: true}
	if !sameDevice {
		currentDeviceIDs[epB.device.ID] = true
	}
	staleA := elineComputeStale(
		elineAppliedState{service.AppliedEndpointADeviceID, service.AppliedEndpointAIface, service.AppliedEndpointAVlan},
		epA.device.ID, epA.iface.Name, epA.vlan, currentDeviceIDs)
	staleB := elineComputeStale(
		elineAppliedState{service.AppliedEndpointBDeviceID, service.AppliedEndpointBIface, service.AppliedEndpointBVlan},
		epB.device.ID, epB.iface.Name, epB.vlan, currentDeviceIDs)

	abandonedSubs := map[uint][]drivers.ELINEStaleSubinterface{}
	attachStale := func(s *elineStale) {
		if s == nil {
			return
		}
		if s.Abandoned {
			abandonedSubs[s.DeviceID] = append(abandonedSubs[s.DeviceID], s.Sub)
			return
		}
		// Not abandoned: s.DeviceID is guaranteed to be epA.device.ID or
		// epB.device.ID (or both, when sameDevice) - fold into that
		// device's own apply so the removal commits atomically with the
		// rest of its config.
		if sameDevice || s.DeviceID == epA.device.ID {
			intentA.StaleSubinterfaces = append(intentA.StaleSubinterfaces, s.Sub)
		} else {
			intentB.StaleSubinterfaces = append(intentB.StaleSubinterfaces, s.Sub)
		}
	}
	attachStale(staleA)
	attachStale(staleB)

	// Push both endpoints every time, not just the one that changed:
	// moving A to a different device means B's pseudowire must start
	// pointing its "neighbor" at A's new loopback, which only happens by
	// re-rendering and re-applying B's intent (its NeighborIP above is
	// always computed from the *current* peer). Skipping the "unchanged"
	// side as an optimization would silently strand its neighbor pointed
	// at the old device.
	resultA := ctrl.applyELINEToDevice(epA.device, creds, settings, intentA)
	results := []ApiServiceElinePushResult{resultA}
	resultB := resultA // sameDevice: A's one call provisions both sides
	if !sameDevice {
		resultB = ctrl.applyELINEToDevice(epB.device, creds, settings, intentB)
		results = append(results, resultB)
	}

	// Abandoned devices (an endpoint moved off them entirely) get their
	// own teardown-only call, since no ApplyELINE above touches them.
	abandonedErr := map[uint]string{}
	for deviceID, subs := range abandonedSubs {
		device, ok := devicesByID[deviceID]
		if !ok {
			abandonedErr[deviceID] = "device not found"
			results = append(results, ApiServiceElinePushResult{
				Device: fmt.Sprintf("device #%d", deviceID),
				Error:  "abandoned ELINE endpoint device no longer exists in factum - stale config was not removed",
			})
			continue
		}
		res := ctrl.removeELINEFromDevice(&device, creds, settings, &drivers.ELINERemoval{
			Name:               service.ServiceID,
			StaleSubinterfaces: subs,
		})
		abandonedErr[deviceID] = res.Error
		results = append(results, res)
	}

	// Only advance Applied* (and so treat this side as reconciled) once
	// its own push succeeded and, if it left a device abandoned, that
	// device's cleanup succeeded too - otherwise the next push retries
	// the same stale cleanup rather than losing track of it.
	sideOK := func(result ApiServiceElinePushResult, stale *elineStale) bool {
		if result.Error != "" {
			return false
		}
		if stale != nil && stale.Abandoned && abandonedErr[stale.DeviceID] != "" {
			return false
		}
		return true
	}
	updates := map[string]any{}
	if sideOK(resultA, staleA) {
		updates["applied_endpoint_a_device_id"] = epA.device.ID
		updates["applied_endpoint_a_iface"] = epA.iface.Name
		updates["applied_endpoint_a_vlan"] = epA.vlan
	}
	if sideOK(resultB, staleB) {
		updates["applied_endpoint_b_device_id"] = epB.device.ID
		updates["applied_endpoint_b_iface"] = epB.iface.Name
		updates["applied_endpoint_b_vlan"] = epB.vlan
	}
	if len(updates) > 0 {
		if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
	}

	return c.JSON(http.StatusOK, map[string]any{"results": results})
}
