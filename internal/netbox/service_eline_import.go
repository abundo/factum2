package netbox

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/abundo/factum2/internal/cfgmgmt"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/models"
	"github.com/abundo/netboxtool"
	"gorm.io/gorm"
)

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

// syncServiceEndpointsFromL2VPNs walks Netbox L2VPNs whose type matches a
// cfgmgmt service type's NetboxType (evpl→ELINE, vpls→ELAN, …), matches
// each to a factum Service (by L2VPNNetboxID or by ServiceID == L2VPN name),
// resolves terminations onto factum interfaces, and writes service_endpoints.
//
// Does not create Service rows - those come from Lime (or manual create).
// L2VPNs with no matching service are skipped. Does not clear endpoints on
// services whose L2VPN has disappeared (same non-delete stance as
// device-sync's L2 phase).
func syncServiceEndpointsFromL2VPNs(db *gorm.DB, nb l2vpnAPI, reporter jobevent.Reporter) error {
	types, err := cfgmgmt.ListServiceTypes(db)
	if err != nil {
		return err
	}
	typeByNetbox := map[string]*models.ServiceType{}
	for i := range types {
		st := &types[i]
		if st.NetboxType == "" || st.NetboxType == models.NetboxTypeVRF {
			continue
		}
		if _, ok := typeByNetbox[st.NetboxType]; !ok {
			typeByNetbox[st.NetboxType] = st
		}
	}
	if len(typeByNetbox) == 0 {
		reporter.Emit(jobevent.Info, "L2VPN→service import: no service types with a NetBox L2VPN mapping")
		return nil
	}

	l2vpns, err := nb.GetL2VPNs("")
	if err != nil {
		return fmt.Errorf("list netbox l2vpns: %w", err)
	}
	if len(l2vpns) == 0 {
		reporter.Emit(jobevent.Info, "L2VPN→service import: no L2VPNs in netbox")
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

	var countLinked, countUpdated, countSkipped, countNoService, countNoEnds, countNoType int
	for _, l2vpn := range l2vpns {
		if l2vpn == nil || l2vpn.NetboxID == 0 {
			continue
		}
		st := typeByNetbox[l2vpn.Type]
		if st == nil {
			countNoType++
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
		if svc.ServiceType != "" && svc.ServiceType != st.Name {
			reporter.Emit(jobevent.Warning, "L2VPN→service: skip %q (service %s has type %q, not %s)",
				l2vpn.Name, svc.ServiceID, svc.ServiceType, st.Name)
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
		roles := cfgmgmt.EndpointRolesForCount(st, len(ends))
		if len(roles) == 0 {
			reporter.Emit(jobevent.Warning, "L2VPN→service: skip %q (type %s has no endpoint roles for %d ends)",
				l2vpn.Name, st.Name, len(ends))
			countSkipped++
			continue
		}
		if len(ends) > len(roles) {
			reporter.Emit(jobevent.Warning, "L2VPN→service: %q has %d terminations, using first %d",
				l2vpn.Name, len(ends), len(roles))
			ends = ends[:len(roles)]
		}

		changed, err := applyL2VPNEndsToService(db, svc, st, l2vpn, ends, roles)
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
		"L2VPN→service import: %d updated, %d already linked, %d no matching service, %d no resolvable ends, %d skipped, %d unmapped type",
		countUpdated, countLinked, countNoService, countNoEnds, countSkipped, countNoType)
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

// applyL2VPNEndsToService writes generic service_endpoints for ends onto svc
// using roles from the matching service type. Returns whether anything
// actually changed.
func applyL2VPNEndsToService(db *gorm.DB, svc *models.Service, st *models.ServiceType, l2vpn *netboxtool.NBL2VPN, ends []resolvedELineEnd, roles []string) (bool, error) {
	want := make([]models.ServiceEndpoint, 0, len(ends))
	for i, end := range ends {
		if end.physicalInterfaceID == 0 {
			continue
		}
		if i >= len(roles) {
			break
		}
		want = append(want, models.ServiceEndpoint{
			Role:        roles[i],
			DeviceID:    end.deviceID,
			InterfaceID: end.physicalInterfaceID,
			Fields:      cfgmgmt.EncodeEndpointFields(end.vlan, end.subinterfaceNetboxID, end.terminationNetboxID),
		})
	}

	pseudowireID := l2vpn.Identifier
	if pseudowireID == 0 {
		pseudowireID = svc.PseudowireID
	}

	existing, err := cfgmgmt.ListEndpoints(db, svc.ID)
	if err != nil {
		return false, err
	}
	if !serviceEndpointsDiffer(svc, st.Name, existing, want, pseudowireID, l2vpn.NetboxID) {
		return false, nil
	}

	updates := map[string]any{
		"service_type":     st.Name,
		"l2_vpn_netbox_id": l2vpn.NetboxID,
		"pseudowire_id":    pseudowireID,
	}
	if err := db.Model(&models.Service{}).Where("id = ?", svc.ID).Updates(updates).Error; err != nil {
		return false, fmt.Errorf("update service %s from l2vpn %q: %w", svc.ServiceID, l2vpn.Name, err)
	}
	if err := cfgmgmt.ReplaceEndpoints(db, svc.ID, want); err != nil {
		return false, fmt.Errorf("update service %s endpoints from l2vpn %q: %w", svc.ServiceID, l2vpn.Name, err)
	}

	svc.ServiceType = st.Name
	svc.L2VPNNetboxID = l2vpn.NetboxID
	svc.PseudowireID = pseudowireID
	return true, nil
}

func endpointKey(ep models.ServiceEndpoint) string {
	sub, term := cfgmgmt.NetboxIDsFromFields(ep.Fields)
	return fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%d\x00%d",
		ep.Role, ep.DeviceID, ep.InterfaceID, cfgmgmt.VLANFromFields(ep.Fields), sub, term)
}

func serviceEndpointsDiffer(svc *models.Service, typeName string, existing, want []models.ServiceEndpoint, pseudowireID int, l2vpnID uint) bool {
	if svc.ServiceType != typeName {
		return true
	}
	if svc.L2VPNNetboxID != l2vpnID || svc.PseudowireID != pseudowireID {
		return true
	}
	if len(existing) != len(want) {
		return true
	}
	counts := map[string]int{}
	for _, e := range existing {
		counts[endpointKey(e)]++
	}
	for _, w := range want {
		k := endpointKey(w)
		if counts[k] == 0 {
			return true
		}
		counts[k]--
	}
	return false
}
