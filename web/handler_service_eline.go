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
func reconcileELineSubinterface(db *gorm.DB, nb serviceNetboxAPI, ep *eLineEndpoint, prevSubinterfaceNetboxID, prevTerminationNetboxID uint, description, label string) (*eLineReconcileResult, error) {
	if prevTerminationNetboxID != 0 {
		if err := nb.DeleteL2VPNTermination(prevTerminationNetboxID); err != nil {
			slog.Warn("service netbox: failed to delete old l2vpn termination", "endpoint", label, "err", err)
		}
	}
	if prevSubinterfaceNetboxID != 0 {
		if err := nb.InterfaceDelete(int(prevSubinterfaceNetboxID)); err != nil {
			slog.Warn("service netbox: failed to delete old subinterface", "endpoint", label, "err", err)
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

func apiServiceElineGone(c *echo.Context) error {
	return c.JSON(http.StatusGone, map[string]any{
		"error": "use PUT /api/service/:id/endpoints and POST /api/service/:id/push",
	})
}

// ApiServiceElineUpdate is gone; use PUT /api/service/:id/endpoints.
func (ctrl *Controller) ApiServiceElineUpdate(c *echo.Context) error {
	return apiServiceElineGone(c)
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

// serviceNetboxAPI is the NetBox surface generic service reconcile/teardown uses.
type serviceNetboxAPI interface {
	CreateInterfaceWithOptions(deviceID uint, name string, extra map[string]any) (*netboxtool.NetboxInterfaceREST, error)
	InterfaceDelete(interfaceID int) error
	GetL2VPNByName(name string) (*netboxtool.NBL2VPN, error)
	GetL2VPNByIdentifier(identifier int) (*netboxtool.NBL2VPN, error)
	CreateL2VPN(name, slug, l2vpnType string, identifier int) (*netboxtool.NBL2VPN, error)
	UpdateL2VPN(l2vpnID uint, changes map[string]any) error
	DeleteL2VPN(l2vpnID uint) error
	CreateL2VPNTermination(l2vpnID, interfaceID uint) (*netboxtool.NBL2VPNTermination, error)
	DeleteL2VPNTermination(terminationID uint) error
	GetVRFByName(name string) (*netboxtool.NBVRF, error)
	CreateVRF(name, rd, description string) (*netboxtool.NBVRF, error)
	DeleteVRF(id uint) error
}

type liveNetbox struct {
	*netboxtool.NetboxClient
}

func (l liveNetbox) DeleteVRF(id uint) error {
	if id == 0 {
		return nil
	}
	endpoint := "/api/ipam/vrfs/" + strconv.FormatUint(uint64(id), 10) + "/"
	base := strings.TrimRight(l.P.URL, "/")
	req, err := http.NewRequest(http.MethodDelete, base+endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+l.P.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("netbox DELETE %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("netbox DELETE %s failed: %s", endpoint, resp.Status)
	}
	return nil
}

func (ctrl *Controller) serviceNetbox(settings *models.Settings) (serviceNetboxAPI, error) {
	if ctrl.netboxFn != nil {
		return ctrl.netboxFn(settings)
	}
	nb, err := ctrl.newNetboxClient(settings)
	if err != nil {
		return nil, err
	}
	return liveNetbox{nb}, nil
}

func netboxObjectName(svc *models.Service) string {
	if svc == nil {
		return ""
	}
	return svc.ServiceID
}

func netboxSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 100 {
		out = strings.Trim(out[:100], "-")
	}
	if out == "" {
		return "l2vpn"
	}
	return out
}

func serviceHasNetboxMapping(st *models.ServiceType) bool {
	return st != nil && (st.SyncSource != "" || st.NetboxType != "")
}

func matchPrevEndpoint(prev []models.ServiceEndpoint, ep models.ServiceEndpoint) models.ServiceEndpoint {
	for _, p := range prev {
		if p.DeviceID == ep.DeviceID && p.InterfaceID == ep.InterfaceID {
			return p
		}
	}
	vlan := cfgmgmt.VLANFromFields(ep.Fields)
	found := -1
	for i := range prev {
		if cfgmgmt.VLANFromFields(prev[i].Fields) != vlan {
			continue
		}
		if found != -1 {
			return models.ServiceEndpoint{}
		}
		found = i
	}
	if found >= 0 {
		return prev[found]
	}
	return models.ServiceEndpoint{}
}

func ifaceOnDevice(device *models.Device, id uint) *models.Interface {
	if device == nil {
		return nil
	}
	for i := range device.Interfaces {
		if device.Interfaces[i].ID == id {
			return &device.Interfaces[i]
		}
	}
	return nil
}

func ifaceByName(device *models.Device, name string) *models.Interface {
	if device == nil || name == "" {
		return nil
	}
	for i := range device.Interfaces {
		if device.Interfaces[i].Name == name {
			return &device.Interfaces[i]
		}
	}
	return nil
}

// reconcileServiceNetbox upserts L2VPN/VRF + terminations when the definition
// has a NetBox mapping and integration is active. Mutates eps fields in place
// with stored netbox ids. Does not ReplaceEndpoints.
func (ctrl *Controller) reconcileServiceNetbox(ctx context.Context, svc *models.Service, st *models.ServiceType, eps []models.ServiceEndpoint) error {
	if svc == nil || !serviceHasNetboxMapping(st) {
		return nil
	}
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return err
	}
	if !netboxConfigured(settings) {
		return nil
	}
	if st.NetboxType == "" {
		return nil
	}
	nb, err := ctrl.serviceNetbox(settings)
	if err != nil {
		return &elineHTTPError{http.StatusBadGateway, "netbox client unavailable: " + err.Error()}
	}

	existing, err := cfgmgmt.ListEndpoints(ctrl.DB, svc.ID)
	if err != nil {
		return err
	}

	name := netboxObjectName(svc)
	var customer models.Customer
	hasCustomer := svc.CustomerID != 0 && ctrl.DB.First(&customer, svc.CustomerID).Error == nil
	description := name
	if hasCustomer {
		description = elineInterfaceDescription(name, customer.Name)
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

	usedPrev := map[uint]bool{}
	objectID := svc.L2VPNNetboxID

	switch st.NetboxType {
	case models.NetboxTypeEVPL, models.NetboxTypeVPLS:
		l2vpnType := st.NetboxType
		pw := 0
		if l2vpnType == models.NetboxTypeEVPL {
			pw, err = pseudowireIDFromServiceID(name)
			if err != nil {
				return &elineHTTPError{http.StatusBadRequest, err.Error()}
			}
		}
		if objectID == 0 {
			existingL2, err := nb.GetL2VPNByName(name)
			if err != nil {
				return &elineHTTPError{http.StatusBadGateway, "lookup netbox l2vpn: " + err.Error()}
			}
			if existingL2 == nil && pw > 0 {
				existingL2, err = nb.GetL2VPNByIdentifier(pw)
				if err != nil {
					return &elineHTTPError{http.StatusBadGateway, "lookup netbox l2vpn identifier: " + err.Error()}
				}
			}
			if existingL2 != nil {
				objectID = existingL2.NetboxID
			} else {
				created, err := nb.CreateL2VPN(name, netboxSlug(name), l2vpnType, pw)
				if err != nil {
					return &elineHTTPError{http.StatusBadGateway, "failed to create netbox l2vpn: " + err.Error()}
				}
				objectID = created.NetboxID
			}
		}
		if l2vpnType == models.NetboxTypeEVPL && pw > 0 {
			if err := nb.UpdateL2VPN(objectID, map[string]any{"identifier": pw}); err != nil {
				slog.Warn("service netbox: failed to set l2vpn identifier", "err", err)
			}
			svc.PseudowireID = pw
		}
		for i := range eps {
			device, ok := devicesByID[eps[i].DeviceID]
			if !ok {
				return &elineHTTPError{http.StatusBadRequest, "endpoint device not found"}
			}
			ifc := ifaceOnDevice(&device, eps[i].InterfaceID)
			if ifc == nil {
				return &elineHTTPError{http.StatusBadRequest, "endpoint interface not found"}
			}
			prev := matchPrevEndpoint(existing, eps[i])
			if prev.ID != 0 {
				usedPrev[prev.ID] = true
			}
			oldSub, oldTerm := cfgmgmt.NetboxIDsFromFields(prev.Fields)
			vlan := cfgmgmt.VLANFromFields(eps[i].Fields)
			termOn := ifc.NetboxID
			subID := uint(0)
			if vlan > 0 {
				resolved := &eLineEndpoint{device: &device, iface: *ifc, vlan: vlan}
				res, err := reconcileELineSubinterface(ctrl.DB, nb, resolved, oldSub, oldTerm, description, eps[i].Role)
				if err != nil {
					return &elineHTTPError{http.StatusBadGateway, err.Error()}
				}
				subID = res.subinterfaceNetboxID
				termOn = subID
			} else {
				if oldTerm != 0 {
					if err := nb.DeleteL2VPNTermination(oldTerm); err != nil {
						slog.Warn("service netbox: failed to delete old l2vpn termination", "err", err)
					}
				}
				if oldSub != 0 {
					if err := nb.InterfaceDelete(int(oldSub)); err != nil {
						slog.Warn("service netbox: failed to delete old subinterface", "err", err)
					}
					deleteFactumInterfaceByNetboxID(ctrl.DB, oldSub, eps[i].Role)
				}
			}
			term, err := nb.CreateL2VPNTermination(objectID, termOn)
			if err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to create l2vpn termination: " + err.Error()}
			}
			eps[i].Fields = cfgmgmt.MergeEndpointNetboxIDs(eps[i].Fields, subID, term.NetboxID)
		}
		if live, ok := unwrapNetbox(nb); ok && hasCustomer && live != nil {
			tenant, err := netbox.FindOrCreateTenant(live, customer)
			if err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to resolve netbox tenant for customer: " + err.Error()}
			}
			if err := nb.UpdateL2VPN(objectID, map[string]any{"tenant": tenant.NetboxID}); err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to set netbox l2vpn tenant: " + err.Error()}
			}
		}
	case models.NetboxTypeVRF:
		existingVRF, err := nb.GetVRFByName(name)
		if err != nil {
			return &elineHTTPError{http.StatusBadGateway, "lookup netbox vrf: " + err.Error()}
		}
		if existingVRF != nil {
			objectID = existingVRF.NetboxID
		} else {
			created, err := nb.CreateVRF(name, "", description)
			if err != nil {
				return &elineHTTPError{http.StatusBadGateway, "failed to create netbox vrf: " + err.Error()}
			}
			objectID = created.NetboxID
		}
	}

	for _, prev := range existing {
		if usedPrev[prev.ID] {
			continue
		}
		sub, term := cfgmgmt.NetboxIDsFromFields(prev.Fields)
		if term != 0 {
			if err := nb.DeleteL2VPNTermination(term); err != nil {
				slog.Warn("service netbox: failed to delete removed endpoint termination", "err", err)
			}
		}
		if sub != 0 {
			if err := nb.InterfaceDelete(int(sub)); err != nil {
				slog.Warn("service netbox: failed to delete removed endpoint subinterface", "err", err)
			}
			deleteFactumInterfaceByNetboxID(ctrl.DB, sub, prev.Role)
		}
	}

	updates := map[string]any{}
	if objectID != 0 {
		updates["l2_vpn_netbox_id"] = objectID
		svc.L2VPNNetboxID = objectID
	}
	if svc.PseudowireID != 0 && st.NetboxType == models.NetboxTypeEVPL {
		updates["pseudowire_id"] = svc.PseudowireID
	}
	if len(updates) > 0 {
		if err := ctrl.DB.Model(&models.Service{}).Where("id = ?", svc.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func unwrapNetbox(nb serviceNetboxAPI) (*netboxtool.NetboxClient, bool) {
	if v, ok := nb.(liveNetbox); ok && v.NetboxClient != nil {
		return v.NetboxClient, true
	}
	return nil, false
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
	if err := ctrl.applyELINECmds(drv, device, intent, ""); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (ctrl *Controller) applyELINECmds(drv drivers.DriverClient, device *models.Device, intent *drivers.ELINEIntent, comment string) error {
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
		return applier.ApplyCLISession(intent.Name, cmds, comment)
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
func (ctrl *Controller) removeELINEFromDevice(device *models.Device, creds deviceCredentialsRequest, settings *models.Settings, removal *drivers.ELINERemoval, comment string) ApiServiceElinePushResult {
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
	if err := ctrl.removeELINECmds(drv, device, removal, comment); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (ctrl *Controller) removeELINECmds(drv drivers.DriverClient, device *models.Device, removal *drivers.ELINERemoval, comment string) error {
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
		return applier.ApplyCLISession(removal.Name, cmds, comment)
	}
	remover, ok := drv.(drivers.ELINERemover)
	if !ok {
		return fmt.Errorf("ELINE provisioning is not yet supported for platform %s", device.Platform)
	}
	return remover.RemoveELINE(removal)
}

func appliedSnapshot(ep models.ServiceEndpoint) models.ServiceEndpoint {
	out := ep
	if ep.AppliedDeviceID != 0 {
		out.DeviceID = ep.AppliedDeviceID
		if ep.AppliedIface != "" {
			out.AppliedIface = ep.AppliedIface
		}
		if len(ep.AppliedFields) > 0 && string(ep.AppliedFields) != "null" {
			out.Fields = ep.AppliedFields
		}
	}
	return out
}

func bindingUnchanged(old, neu models.ServiceEndpoint) bool {
	return old.DeviceID == neu.DeviceID && old.InterfaceID == neu.InterfaceID
}

func endpointNeedsAddPush(ep models.ServiceEndpoint, ifaceName string) bool {
	if ep.AppliedDeviceID == 0 {
		return true
	}
	if ep.AppliedDeviceID != ep.DeviceID {
		return true
	}
	if ep.AppliedIface != "" && ifaceName != "" && ep.AppliedIface != ifaceName {
		return true
	}
	return false
}

func endpointsNeedingTeardown(existing, next []models.ServiceEndpoint) []models.ServiceEndpoint {
	var out []models.ServiceEndpoint
	for _, old := range existing {
		if old.AppliedDeviceID == 0 {
			continue
		}
		kept := false
		for _, neu := range next {
			if bindingUnchanged(old, neu) {
				kept = true
				break
			}
		}
		if !kept {
			out = append(out, old)
		}
	}
	return out
}

func stampAppliedOnDevice(db *gorm.DB, device *models.Device, eps []models.ServiceEndpoint) error {
	if device == nil {
		return nil
	}
	plat := cfgmgmt.NormalizePlatform(device.Platform)
	for i := range eps {
		ifc := ifaceOnDevice(device, eps[i].InterfaceID)
		name := ""
		if ifc != nil {
			name = ifc.Name
		}
		if name == "" {
			name = eps[i].AppliedIface
		}
		updates := map[string]any{
			"applied_device_id": device.ID,
			"applied_iface":     name,
			"applied_platform":  plat,
			"applied_fields":    eps[i].Fields,
		}
		if err := db.Model(&models.ServiceEndpoint{}).Where("id = ?", eps[i].ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func (ctrl *Controller) applyServiceCLIToDevice(svc *models.Service, device *models.Device, epsOnDevice, siblings []models.ServiceEndpoint, creds deviceCredentialsRequest, settings *models.Settings, comment string) ApiServiceElinePushResult {
	result := ApiServiceElinePushResult{Device: device.Name}
	if !isSupportedDriverPlatform(device) {
		result.Error = "CLI object exists but this platform cannot apply CLI sessions yet"
		return result
	}
	cliObj, err := cfgmgmt.LookupCLIObject(ctrl.DB, svc.ServiceType, device.Platform)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if cliObj == nil {
		result.Error = cfgmgmt.MissingCLIObjectMessage(svc.ServiceType, device.Platform)
		return result
	}
	if err := cfgmgmt.RequireCLIObject(cliObj); err != nil {
		result.Error = err.Error()
		return result
	}
	drv, err := ctrl.newDriverForDevice(device, creds, settings)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	applier, ok := drv.(drivers.CLISessionApplier)
	if !ok {
		result.Error = "CLI object exists but this platform cannot apply CLI sessions yet"
		return result
	}
	var cmds []string
	cleanupDone := false
	label := device.Name
	for i := range epsOnDevice {
		ep := &epsOnDevice[i]
		ifc := ifaceOnDevice(device, ep.InterfaceID)
		if ifc != nil {
			label = device.Name + " " + ifc.Name
		}
		data, err := cfgmgmt.GenericDataWithSiblings(ctrl.DB, svc, ep, device, ifc, siblings)
		if err != nil {
			result.Device = label
			result.Error = err.Error()
			return result
		}
		part, err := cfgmgmt.RenderCLITranslation(ctrl.DB, cliObj, data, !cleanupDone)
		if err != nil {
			result.Device = label
			result.Error = err.Error()
			return result
		}
		cmds = append(cmds, part...)
		cleanupDone = true
	}
	result.Device = label
	if err := applier.ApplyCLISession(svc.ServiceID, cmds, comment); err != nil {
		result.Error = err.Error()
		return result
	}
	if err := stampAppliedOnDevice(ctrl.DB, device, epsOnDevice); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (ctrl *Controller) removeServiceCLIFromDevice(svc *models.Service, device *models.Device, snapshots, siblings []models.ServiceEndpoint, creds deviceCredentialsRequest, settings *models.Settings, comment string) ApiServiceElinePushResult {
	result := ApiServiceElinePushResult{Device: device.Name}
	if !isSupportedDriverPlatform(device) {
		result.Error = "CLI object exists but this platform cannot apply CLI sessions yet"
		return result
	}
	platform := device.Platform
	if len(snapshots) > 0 && snapshots[0].AppliedPlatform != "" {
		platform = snapshots[0].AppliedPlatform
	}
	cliObj, err := cfgmgmt.LookupCLIObject(ctrl.DB, svc.ServiceType, platform)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if cliObj == nil {
		result.Error = cfgmgmt.MissingCLIObjectMessage(svc.ServiceType, platform)
		return result
	}
	if err := cfgmgmt.RequireCLIObject(cliObj); err != nil {
		result.Error = err.Error()
		return result
	}
	drv, err := ctrl.newDriverForDevice(device, creds, settings)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	applier, ok := drv.(drivers.CLISessionApplier)
	if !ok {
		result.Error = "CLI object exists but this platform cannot apply CLI sessions yet"
		return result
	}
	var cmds []string
	label := device.Name
	for i := range snapshots {
		ep := snapshots[i]
		snap := appliedSnapshot(ep)
		ifc := ifaceByName(device, ep.AppliedIface)
		if ifc == nil {
			ifc = ifaceOnDevice(device, snap.InterfaceID)
		}
		if ifc != nil {
			label = device.Name + " " + ifc.Name
			snap.InterfaceID = ifc.ID
		} else {
			// AppliedIface is the snapshot; do not load a stale InterfaceID.
			snap.InterfaceID = 0
			if ep.AppliedIface != "" {
				label = device.Name + " " + ep.AppliedIface
			}
		}
		data, err := cfgmgmt.GenericDataWithSiblings(ctrl.DB, svc, &snap, device, ifc, siblings)
		if err != nil {
			result.Device = label
			result.Error = err.Error()
			return result
		}
		part, err := cfgmgmt.RenderCLIObjectRemove(ctrl.DB, cliObj, data)
		if err != nil {
			result.Device = label
			result.Error = err.Error()
			return result
		}
		cmds = append(cmds, part...)
	}
	result.Device = label
	if err := applier.ApplyCLISession(svc.ServiceID, cmds, comment); err != nil {
		result.Error = err.Error()
	}
	return result
}

// removeServiceFromDevices tears down translation CLI on every device the
// service was pushed to (Applied* if set, else current endpoints).
func (ctrl *Controller) removeServiceFromDevices(c *echo.Context, service *models.Service) ([]ApiServiceElinePushResult, error) {
	eps, err := cfgmgmt.ListEndpoints(ctrl.DB, service.ID)
	if err != nil {
		return nil, err
	}
	return ctrl.removeEndpointSnapshotsFromDevices(c, service, eps, eps)
}

func (ctrl *Controller) removeEndpointSnapshotsFromDevices(c *echo.Context, service *models.Service, snapshots, siblings []models.ServiceEndpoint) ([]ApiServiceElinePushResult, error) {
	order := []uint{}
	byDev := map[uint][]models.ServiceEndpoint{}
	for _, ep := range snapshots {
		snap := appliedSnapshot(ep)
		devID := snap.DeviceID
		if ep.AppliedDeviceID != 0 {
			devID = ep.AppliedDeviceID
		}
		if devID == 0 {
			continue
		}
		if _, ok := byDev[devID]; !ok {
			order = append(order, devID)
		}
		byDev[devID] = append(byDev[devID], ep)
	}
	if len(order) == 0 {
		return nil, nil
	}

	devices, err := fetchDevices(c.Request().Context(), ctrl.DB, order)
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

	results := make([]ApiServiceElinePushResult, 0, len(order))
	for _, deviceID := range order {
		device, ok := devicesByID[deviceID]
		if !ok {
			results = append(results, ApiServiceElinePushResult{
				Device: fmt.Sprintf("device #%d", deviceID),
				Error:  "device no longer exists in factum - stale config was not removed",
			})
			continue
		}
		creds, credErr := ctrl.deviceSyncCredentials(device.Name)
		if credErr != nil {
			results = append(results, ApiServiceElinePushResult{Device: device.Name, Error: credErr.Error()})
			continue
		}
		comment := serviceCommitComment(c, service.ServiceID, "remove")
		results = append(results, ctrl.removeServiceCLIFromDevice(service, &device, byDev[deviceID], siblings, creds, settings, comment))
	}
	return results, nil
}

// removeServiceFromNetbox deletes stored L2VPN/VRF terminations, subinterfaces,
// and the parent object. Fails fast so delete/unrealize can abort.
func (ctrl *Controller) removeServiceFromNetbox(service *models.Service) error {
	settings, err := util.GetOrCreateSettings(ctrl.DB)
	if err != nil {
		return err
	}
	if !netboxConfigured(settings) {
		return fmt.Errorf("netbox is not configured")
	}
	nb, err := ctrl.serviceNetbox(settings)
	if err != nil {
		return err
	}

	eps, err := cfgmgmt.ListEndpoints(ctrl.DB, service.ID)
	if err != nil {
		return err
	}
	st, _ := cfgmgmt.LookupServiceType(ctrl.DB, service.ServiceType)
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
	if service.L2VPNNetboxID == 0 {
		return nil
	}
	if st != nil && st.NetboxType == models.NetboxTypeVRF {
		if err := nb.DeleteVRF(service.L2VPNNetboxID); err != nil {
			return fmt.Errorf("vrf: %w", err)
		}
		return nil
	}
	if err := nb.DeleteL2VPN(service.L2VPNNetboxID); err != nil {
		return fmt.Errorf("l2vpn: %w", err)
	}
	return nil
}

// elineAppliedState is one endpoint's last-successfully-pushed live state
// (service_endpoints.applied_*). A zero DeviceID means this side has never
// been pushed yet.
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

// ApiServiceElinePush is gone; use POST /api/service/:id/push.
func (ctrl *Controller) ApiServiceElinePush(c *echo.Context) error {
	return apiServiceElineGone(c)
}
