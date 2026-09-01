package netbox

import "github.com/abundo/netboxtool"

// CableLabelLLDP is the dcim.Cable.label written on cables created from
// LLDP neighbors. device-sync only creates, retargets, or deletes cables
// with this exact label so operator-drawn cables stay untouched.
const CableLabelLLDP = "lldp"

// IsLLDPCable reports whether c is owned by LLDP auto-cabling.
func IsLLDPCable(c *netboxtool.NBCable) bool {
	return c != nil && c.Label == CableLabelLLDP
}

// cableWriter is the netboxtool surface CreateLLDPCable needs.
type cableWriter interface {
	CreateCableWithOptions(aInterfaceID, bInterfaceID uint, extra map[string]any) (*netboxtool.NBCable, error)
}

// CreateLLDPCable creates a cable between two interfaces and marks it as
// LLDP-owned.
func CreateLLDPCable(nb cableWriter, aInterfaceID, bInterfaceID uint) (*netboxtool.NBCable, error) {
	return nb.CreateCableWithOptions(aInterfaceID, bInterfaceID, map[string]any{"label": CableLabelLLDP})
}
