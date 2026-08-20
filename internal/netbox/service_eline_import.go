package netbox

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

// l2vpnTypeEVPL is Netbox's L2VPN type for point-to-point ELINEs - the
// only type device-sync and web.ApiServiceElineUpdate create.
const l2vpnTypeEVPL = "evpl"

// subinterfaceVLANRe matches a dotted per-VLAN subinterface name as used
// on EOS/IOS-XR (and mirrored into Netbox by device-sync), e.g.
// "Ethernet3.338" -> parent "Ethernet3", vlan 338. Other platforms'
// SAP-style names (e.g. "1/1/1:100") are resolved via ParentID instead.
var subinterfaceVLANRe = regexp.MustCompile(`^(.+)\.(\d+)$`)

// l2vpnAPI is the Netbox surface syncServiceEndpointsFromL2VPNs needs -
// narrowed so tests can substitute a fake without a live Netbox.
type l2vpnAPI interface {
	GetL2VPNs(l2vpnType string) ([]*netboxtool.NBL2VPN, error)
	GetL2VPNTerminations(l2vpnID uint) ([]*netboxtool.NBL2VPNTermination, error)
}

// resolvedELineEnd is one L2VPN termination mapped onto factum's device/
// interface tables - the physical port the service GUI stores as
// EndpointXInterfaceID, plus the optional subinterface the L2VPN actually
// terminates on.
type resolvedELineEnd struct {
	deviceID             uint
	physicalInterfaceID  uint
	vlan                 int
	subinterfaceNetboxID uint
	terminationNetboxID  uint
	// sortKey is device_id + "\x00" + interface name for stable A/B order.
	sortKey string
}

// syncServiceEndpointsFromL2VPNs walks every Netbox EVPL L2VPN, matches it
// to a factum Service (by L2VPNNetboxID or by ServiceID == L2VPN name),
// resolves each termination onto factum interfaces, and writes the ELINE
// endpoint fields the device interface table / ServiceEditDialog need.
//
// Does not create Service rows - those come from Lime (or manual create).
// L2VPNs with no matching service are skipped. Does not clear endpoints on
// services whose L2VPN has disappeared (same non-delete stance as
// device-sync's ELINE phase).
func syncServiceEndpointsFromL2VPNs(db *gorm.DB, nb l2vpnAPI, reporter jobevent.Reporter) error {
	l2vpns, err := nb.GetL2VPNs(l2vpnTypeEVPL)
	if err != nil {
		return fmt.Errorf("list netbox l2vpns: %w", err)
	}
	if len(l2vpns) == 0 {
		reporter.Emit(jobevent.Info, "L2VPN→service import: no EVPL L2VPNs in netbox")
		return nil
	}

	var services []models.Service
	if err := db.Find(&services).Error; err != nil {
		return err
	}
	byL2VPNID := make(map[uint]*models.Service, len(services))
	byServiceID := make(map[string]*models.Service, len(services))
	for i := range services {
		s := &services[i]
		if s.L2VPNNetboxID != 0 {
			byL2VPNID[s.L2VPNNetboxID] = s
		}
		if s.ServiceID != "" {
			byServiceID[s.ServiceID] = s
		}
	}

	var interfaces []models.Interface
	if err := db.Select("id", "device_id", "netbox_id", "name", "type", "parent_id").Find(&interfaces).Error; err != nil {
		return err
	}
	byNetboxID := make(map[uint]models.Interface, len(interfaces))
	for _, iface := range interfaces {
		if iface.NetboxID != 0 {
			byNetboxID[iface.NetboxID] = iface
		}
	}

	var countLinked, countUpdated, countSkipped, countNoService, countNoEnds int
	for _, l2vpn := range l2vpns {
		if l2vpn == nil || l2vpn.NetboxID == 0 {
			continue
		}

		svc := byL2VPNID[l2vpn.NetboxID]
		if svc == nil {
			svc = byServiceID[l2vpn.Name]
		}
		if svc == nil {
			countNoService++
			continue
		}
		// Don't reclassify a service the operator marked as something else.
		if svc.ServiceType != "" && svc.ServiceType != "ELINE" {
			reporter.Emit(jobevent.Warning, "L2VPN→service: skip %q (service %s has type %q, not ELINE)",
				l2vpn.Name, svc.ServiceID, svc.ServiceType)
			countSkipped++
			continue
		}

		terms, err := nb.GetL2VPNTerminations(l2vpn.NetboxID)
		if err != nil {
			return fmt.Errorf("l2vpn %q terminations: %w", l2vpn.Name, err)
		}
		ends := resolveL2VPNEnds(terms, byNetboxID)
		if len(ends) == 0 {
			countNoEnds++
			continue
		}
		if len(ends) > 2 {
			reporter.Emit(jobevent.Warning, "L2VPN→service: %q has %d terminations, using first 2",
				l2vpn.Name, len(ends))
			ends = ends[:2]
		}

		changed, err := applyL2VPNEndsToService(db, svc, l2vpn, ends)
		if err != nil {
			return err
		}
		if changed {
			countUpdated++
			// Keep indexes consistent if later L2VPNs also match this row.
			svc.L2VPNNetboxID = l2vpn.NetboxID
			byL2VPNID[l2vpn.NetboxID] = svc
		} else {
			countLinked++
		}
	}

	reporter.Emit(jobevent.Info,
		"L2VPN→service import: %d updated, %d already linked, %d no matching service, %d no resolvable ends, %d skipped",
		countUpdated, countLinked, countNoService, countNoEnds, countSkipped)
	return nil
}

// resolveL2VPNEnds maps Netbox terminations onto factum physical ports,
// ordered stably so A/B assignment does not flip between runs.
func resolveL2VPNEnds(terms []*netboxtool.NBL2VPNTermination, byNetboxID map[uint]models.Interface) []resolvedELineEnd {
	var ends []resolvedELineEnd
	for _, t := range terms {
		if t == nil || t.InterfaceID == 0 {
			continue
		}
		iface, ok := byNetboxID[t.InterfaceID]
		if !ok {
			continue
		}
		end, ok := resolveTerminationInterface(iface, byNetboxID)
		if !ok {
			continue
		}
		end.terminationNetboxID = t.NetboxID
		ends = append(ends, end)
	}
	sort.Slice(ends, func(i, j int) bool { return ends[i].sortKey < ends[j].sortKey })
	return ends
}

// resolveTerminationInterface turns the interface an L2VPN terminates on
// into the physical port + VLAN the service model stores. Prefer ParentID
// (Netbox parent of a virtual subinterface); fall back to parsing
// "<parent>.<vlan>" from the name for older rows missing ParentID.
func resolveTerminationInterface(iface models.Interface, byNetboxID map[uint]models.Interface) (resolvedELineEnd, bool) {
	end := resolvedELineEnd{
		deviceID: iface.DeviceID,
		sortKey:  fmt.Sprintf("%d\x00%s", iface.DeviceID, iface.Name),
	}

	// Terminated on a subinterface with an explicit Netbox parent.
	if iface.ParentID != 0 {
		parent, ok := byNetboxID[iface.ParentID]
		if !ok {
			return resolvedELineEnd{}, false
		}
		end.physicalInterfaceID = parent.ID
		end.deviceID = parent.DeviceID
		end.subinterfaceNetboxID = iface.NetboxID
		end.vlan = vlanFromSubinterfaceName(iface.Name)
		end.sortKey = fmt.Sprintf("%d\x00%s", end.deviceID, parent.Name)
		if end.vlan > 0 {
			end.sortKey += fmt.Sprintf(".%d", end.vlan)
		}
		return end, true
	}

	// Name-based subinterface: "Ethernet3.338".
	if parentName, vlan, ok := splitSubinterfaceName(iface.Name); ok {
		// Prefer same-device parent match by name when ParentID wasn't set.
		var parent models.Interface
		found := false
		for _, cand := range byNetboxID {
			if cand.DeviceID == iface.DeviceID && cand.Name == parentName {
				parent = cand
				found = true
				break
			}
		}
		if !found {
			// Still record the subinterface; physical port unknown so the
			// Services button can hang on the subinterface row via
			// EndpointXSubinterfaceNetboxID once physical is 0... but
			// handle_dcim requires physicalID != 0. Skip until parent is
			// synced.
			return resolvedELineEnd{}, false
		}
		end.physicalInterfaceID = parent.ID
		end.deviceID = parent.DeviceID
		end.subinterfaceNetboxID = iface.NetboxID
		end.vlan = vlan
		end.sortKey = fmt.Sprintf("%d\x00%s.%d", end.deviceID, parent.Name, vlan)
		return end, true
	}

	// Bare physical port (untagged AC).
	end.physicalInterfaceID = iface.ID
	end.subinterfaceNetboxID = 0
	end.vlan = 0
	return end, true
}

func splitSubinterfaceName(name string) (parent string, vlan int, ok bool) {
	m := subinterfaceVLANRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	// Accept any positive unit number - device-sync may use values outside
	// the classic 1..4094 VLAN range (e.g. Ethernet3.5711 for CN00570-11).
	// The service GUI still enforces 1..4094 when *provisioning* new ELINEs.
	vlan, err := strconv.Atoi(m[2])
	if err != nil || vlan < 1 {
		return "", 0, false
	}
	return m[1], vlan, true
}

func vlanFromSubinterfaceName(name string) int {
	_, vlan, ok := splitSubinterfaceName(name)
	if !ok {
		return 0
	}
	return vlan
}

// applyL2VPNEndsToService writes endpoint fields for ends (1 or 2) onto
// svc. Returns whether anything actually changed.
func applyL2VPNEndsToService(db *gorm.DB, svc *models.Service, l2vpn *netboxtool.NBL2VPN, ends []resolvedELineEnd) (bool, error) {
	var a, b resolvedELineEnd
	a = ends[0]
	if len(ends) > 1 {
		b = ends[1]
	}

	pseudowireID := l2vpn.Identifier
	if pseudowireID == 0 {
		// Keep any previously stored value if Netbox has no identifier
		// (same-device patches often omit it).
		pseudowireID = svc.PseudowireID
	}

	updates := map[string]any{
		"service_type":                      "ELINE",
		"l2_vpn_netbox_id":                  l2vpn.NetboxID,
		"pseudowire_id":                     pseudowireID,
		"endpoint_a_device_id":              a.deviceID,
		"endpoint_a_interface_id":           a.physicalInterfaceID,
		"endpoint_a_vlan":                   a.vlan,
		"endpoint_a_subinterface_netbox_id": a.subinterfaceNetboxID,
		"endpoint_a_termination_netbox_id":  a.terminationNetboxID,
		"endpoint_b_device_id":              b.deviceID,
		"endpoint_b_interface_id":           b.physicalInterfaceID,
		"endpoint_b_vlan":                   b.vlan,
		"endpoint_b_subinterface_netbox_id": b.subinterfaceNetboxID,
		"endpoint_b_termination_netbox_id":  b.terminationNetboxID,
	}

	if !serviceEndpointsDiffer(svc, updates, pseudowireID, l2vpn.NetboxID) {
		return false, nil
	}

	if err := db.Model(&models.Service{}).Where("id = ?", svc.ID).Updates(updates).Error; err != nil {
		return false, fmt.Errorf("update service %s from l2vpn %q: %w", svc.ServiceID, l2vpn.Name, err)
	}

	// Reflect in the in-memory copy for subsequent index lookups.
	svc.ServiceType = "ELINE"
	svc.L2VPNNetboxID = l2vpn.NetboxID
	svc.PseudowireID = pseudowireID
	svc.EndpointADeviceID = a.deviceID
	svc.EndpointAInterfaceID = a.physicalInterfaceID
	svc.EndpointAVlan = a.vlan
	svc.EndpointASubinterfaceNetboxID = a.subinterfaceNetboxID
	svc.EndpointATerminationNetboxID = a.terminationNetboxID
	svc.EndpointBDeviceID = b.deviceID
	svc.EndpointBInterfaceID = b.physicalInterfaceID
	svc.EndpointBVlan = b.vlan
	svc.EndpointBSubinterfaceNetboxID = b.subinterfaceNetboxID
	svc.EndpointBTerminationNetboxID = b.terminationNetboxID

	return true, nil
}

// serviceEndpointsDiffer reports whether updates would change anything on
// svc worth writing.
func serviceEndpointsDiffer(svc *models.Service, updates map[string]any, pseudowireID int, l2vpnID uint) bool {
	if svc.ServiceType != "ELINE" {
		return true
	}
	if svc.L2VPNNetboxID != l2vpnID || svc.PseudowireID != pseudowireID {
		return true
	}
	if svc.EndpointADeviceID != updates["endpoint_a_device_id"].(uint) ||
		svc.EndpointAInterfaceID != updates["endpoint_a_interface_id"].(uint) ||
		svc.EndpointAVlan != updates["endpoint_a_vlan"].(int) ||
		svc.EndpointASubinterfaceNetboxID != updates["endpoint_a_subinterface_netbox_id"].(uint) ||
		svc.EndpointATerminationNetboxID != updates["endpoint_a_termination_netbox_id"].(uint) {
		return true
	}
	if svc.EndpointBDeviceID != updates["endpoint_b_device_id"].(uint) ||
		svc.EndpointBInterfaceID != updates["endpoint_b_interface_id"].(uint) ||
		svc.EndpointBVlan != updates["endpoint_b_vlan"].(int) ||
		svc.EndpointBSubinterfaceNetboxID != updates["endpoint_b_subinterface_netbox_id"].(uint) ||
		svc.EndpointBTerminationNetboxID != updates["endpoint_b_termination_netbox_id"].(uint) {
		return true
	}
	return false
}
