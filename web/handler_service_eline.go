package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/abundo/factum2/internal/cfgmgmt"
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
	if !cfgmgmt.IsPhysicalInterfaceType(iface.Type) {
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

// ApiServiceElineUpdate is a thin adapter around persistELINEEndpoints:
// maps the historical A/B DTO onto generic service_endpoints (roles a/b).
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

	var dto ServiceElineDTO
	if err := c.Bind(&dto); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	eps := []models.ServiceEndpoint{
		{
			Role: "a", DeviceID: dto.EndpointADeviceID, InterfaceID: dto.EndpointAInterfaceID,
			Fields: cfgmgmt.EncodeEndpointFields(dto.EndpointAVlan, 0, 0),
		},
		{
			Role: "b", DeviceID: dto.EndpointBDeviceID, InterfaceID: dto.EndpointBInterfaceID,
			Fields: cfgmgmt.EncodeEndpointFields(dto.EndpointBVlan, 0, 0),
		},
	}
	st, err := cfgmgmt.LookupServiceType(ctrl.DB, "ELINE")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	if err := cfgmgmt.ValidateEndpoints(ctrl.DB, st, eps); err != nil {
		if se := cfgmgmt.AsStatusError(err); se != nil {
			return c.JSON(se.Status, map[string]any{"error": se.Message})
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := cfgmgmt.ValidateELINEShape(ctrl.DB, eps); err != nil {
		if se := cfgmgmt.AsStatusError(err); se != nil {
			return c.JSON(se.Status, map[string]any{"error": se.Message})
		}
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if err := ctrl.persistELINEEndpoints(c.Request().Context(), &service, eps); err != nil {
		return elinePersistError(c, err)
	}
	var updated models.Service
	if err := ctrl.DB.First(&updated, id).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	rows, _ := cfgmgmt.ListEndpoints(ctrl.DB, id)
	return c.JSON(http.StatusOK, ServiceDetailResponse{
		Service:         updated,
		AppliedToDevice: updated.AppliedEndpointADeviceID != 0 || updated.AppliedEndpointBDeviceID != 0,
		Endpoints:       rows,
	})
}

type elineHTTPError struct {
	status int
	msg    string
}

func (e *elineHTTPError) Error() string { return e.msg }

func elinePersistError(c *echo.Context, err error) error {
	if se := cfgmgmt.AsStatusError(err); se != nil {
		return c.JSON(se.Status, map[string]any{"error": se.Message})
	}
	var he *elineHTTPError
	if errors.As(err, &he) {
		return c.JSON(he.status, map[string]any{"error": he.msg})
	}
	return c.JSON(http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// persistELINEEndpoints writes generic service_endpoints for an ELINE,
// optionally reconciling NetBox L2VPN + subinterfaces. Does not write
// Service.EndpointA/B* columns.
func (ctrl *Controller) persistELINEEndpoints(ctx context.Context, svc *models.Service, eps []models.ServiceEndpoint) error {
	var customer models.Customer
	if err := ctrl.DB.First(&customer, svc.CustomerID).Error; err != nil {
		return &elineHTTPError{http.StatusBadRequest, "service has no valid customer"}
	}
	pseudowireID, err := pseudowireIDFromServiceID(svc.ServiceID)
	if err != nil {
		return &elineHTTPError{http.StatusBadRequest, err.Error()}
	}

	ids := make([]uint, 0, len(eps))
	for _, ep := range eps {
		ids = append(ids, ep.DeviceID)
	}
	devices, err := fetchDevices(ctx, ctrl.DB, ids)
	if err != nil {
		return err
	}
	devicesByID := make(map[uint]models.Device, len(devices))
	for _, d := range devices {
		devicesByID[d.ID] = d
	}

	existing, err := cfgmgmt.ListEndpoints(ctrl.DB, svc.ID)
	if err != nil {
		return err
	}
	prevByRole := map[string]models.ServiceEndpoint{}
	for _, e := range existing {
		prevByRole[e.Role] = e
	}

	resolved := make([]*eLineEndpoint, len(eps))
	for i := range eps {
		vlan := cfgmgmt.VLANFromFields(eps[i].Fields)
		ep, err := resolveELineEndpoint(devicesByID, eps[i].DeviceID, eps[i].InterfaceID, vlan, eps[i].Role)
		if err != nil {
			return &elineHTTPError{http.StatusBadRequest, err.Error()}
		}
		resolved[i] = ep
	}

	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return err
	}

	l2vpnID := svc.L2VPNNetboxID
	if netboxConfigured(settings) {
		nb, err := ctrl.newNetboxClient(settings)
		if err != nil {
			slog.Warn("eline: netbox client unavailable, persisting locally", "err", err)
		} else {
			tenant, err := netbox.FindOrCreateTenant(nb, customer)
			if err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to resolve netbox tenant for customer: " + err.Error()}
			}
			description := elineInterfaceDescription(svc.ServiceID, customer.Name)
			for i := range eps {
				prev := prevByRole[eps[i].Role]
				oldSub, oldTerm := cfgmgmt.NetboxIDsFromFields(prev.Fields)
				res, err := reconcileELineSubinterface(ctrl.DB, nb, resolved[i], oldSub, oldTerm, description, eps[i].Role)
				if err != nil {
					return &elineHTTPError{http.StatusBadGateway, err.Error()}
				}
				if l2vpnID == 0 {
					created, err := nb.CreateL2VPN(svc.ServiceID, strings.ToLower(svc.ServiceID), "evpl", pseudowireID)
					if err != nil {
						return &elineHTTPError{http.StatusBadGateway, "failed to create netbox l2vpn: " + err.Error()}
					}
					l2vpnID = created.NetboxID
				}
				term, err := nb.CreateL2VPNTermination(l2vpnID, res.subinterfaceNetboxID)
				if err != nil {
					return &elineHTTPError{http.StatusBadGateway, "failed to create l2vpn termination " + eps[i].Role + ": " + err.Error()}
				}
				eps[i].Fields = cfgmgmt.EncodeEndpointFields(resolved[i].vlan, res.subinterfaceNetboxID, term.NetboxID)
			}
			if err := nb.UpdateL2VPN(l2vpnID, map[string]any{"tenant": tenant.NetboxID}); err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to set netbox l2vpn tenant: " + err.Error()}
			}
		}
	}

	if err := cfgmgmt.ReplaceEndpoints(ctrl.DB, svc.ID, eps); err != nil {
		return err
	}
	updates := map[string]any{"pseudowire_id": pseudowireID}
	if l2vpnID != 0 {
		updates["l2_vpn_netbox_id"] = l2vpnID
	}
	if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", svc.ID).Updates(updates).Error; err != nil {
		return err
	}
	svc.PseudowireID = pseudowireID
	svc.L2VPNNetboxID = l2vpnID
	return nil
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
func netboxConfigured(settings *models.Settings) bool {
	if settings == nil {
		return false
	}
	if settings.NetboxEnabled != nil && !*settings.NetboxEnabled {
		return false
	}
	return strings.TrimSpace(settings.NetboxApiURL) != "" && strings.TrimSpace(settings.NetboxApiToken) != ""
}

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
	if err := ctrl.applyELINECmds(drv, device, intent); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (ctrl *Controller) applyELINECmds(drv drivers.DriverClient, device *models.Device, intent *drivers.ELINEIntent) error {
	cliObj, err := cfgmgmt.LookupCLIObject(ctrl.DB, "ELINE", device.Platform)
	if err != nil {
		return err
	}
	if cliObj != nil {
		if err := cfgmgmt.RequireCLIObject(cliObj); err != nil {
			return err
		}
		if prep, ok := drv.(drivers.ELINEPrepareChecker); ok {
			if err := prep.PrepareELINEApply(intent); err != nil {
				return err
			}
		}
		data, err := drivers.ELINETemplateData(intent, strings.ToLower(device.Platform))
		if err != nil {
			return err
		}
		cmds, err := cfgmgmt.RenderCLIObject(ctrl.DB, cliObj, data)
		if err != nil {
			return err
		}
		applier, ok := drv.(drivers.CLISessionApplier)
		if !ok {
			return fmt.Errorf("CLI object exists but this platform cannot apply CLI sessions yet")
		}
		return applier.ApplyCLISession(intent.Name, cmds)
	}
	applier, ok := drv.(drivers.ELINEApplier)
	if !ok {
		return fmt.Errorf("ELINE provisioning is not yet supported for platform %s", device.Platform)
	}
	return applier.ApplyELINE(intent)
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
	if err := ctrl.removeELINECmds(drv, device, removal); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (ctrl *Controller) removeELINECmds(drv drivers.DriverClient, device *models.Device, removal *drivers.ELINERemoval) error {
	cliObj, err := cfgmgmt.LookupCLIObject(ctrl.DB, "ELINE", device.Platform)
	if err != nil {
		return err
	}
	if cliObj != nil {
		if err := cfgmgmt.RequireCLIObject(cliObj); err != nil {
			return err
		}
		cmds, err := cfgmgmt.RenderCLIObjectRemove(ctrl.DB, cliObj, removal)
		if err != nil {
			return err
		}
		applier, ok := drv.(drivers.CLISessionApplier)
		if !ok {
			return fmt.Errorf("CLI object exists but this platform cannot apply CLI sessions yet")
		}
		return applier.ApplyCLISession(removal.Name, cmds)
	}
	remover, ok := drv.(drivers.ELINERemover)
	if !ok {
		return fmt.Errorf("ELINE provisioning is not yet supported for platform %s", device.Platform)
	}
	return remover.RemoveELINE(removal)
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

	eps, err := cfgmgmt.ListEndpoints(ctrl.DB, service.ID)
	if err != nil {
		return err
	}
	for _, ep := range eps {
		sub, term := cfgmgmt.NetboxIDsFromFields(ep.Fields)
		if term != 0 {
			if err := nb.DeleteL2VPNTermination(term); err != nil {
				return fmt.Errorf("endpoint %s termination: %w", ep.Role, err)
			}
		}
		if sub != 0 {
			if err := nb.InterfaceDelete(int(sub)); err != nil {
				return fmt.Errorf("endpoint %s subinterface: %w", ep.Role, err)
			}
			deleteFactumInterfaceByNetboxID(ctrl.DB, sub, ep.Role)
		}
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

// ApiServiceElinePush is kept as a dedicated route; it delegates to the
// generic pack-based push used by POST /service/:id/push.
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
	return ctrl.apiServiceGenericPush(c, &service)
}
