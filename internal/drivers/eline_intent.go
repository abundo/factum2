package drivers

// ELINEIntent is one device's local half of a point-to-point ELINE service
// to provision - the input to an ELINEApplier. Vendor-agnostic: any future
// driver (SR OS, IOS-XR, ...) renders this through its own template rather
// than needing a new intent shape.
type ELINEIntent struct {
	Name        string // ServiceID, used as the pseudowire/patch name
	Description string // "ID=<ServiceID> <customer name>" - set on every subinterface this intent touches
	LocalIface  string // this device's physical interface, e.g. "Ethernet1"
	LocalVLAN   int

	// ServiceNumericID is the same numeric ID as Remote.PseudowireID
	// (web/handler_service_eline.go's pseudowireIDFromServiceID), but set
	// unconditionally - including for a same-device ELINE, where Remote is
	// nil. EOS has no use for it (a same-device patch needs no numeric ID
	// at all), but SR OS does: every epipe needs a numeric service-id
	// regardless of whether it carries a spoke-sdp, so this is the only
	// place a same-device apply on that platform can get one from.
	ServiceNumericID int

	// Exactly one of PeerLocalIface or Remote is set: PeerLocalIface when
	// the ELINE's other endpoint is on this same device (no pseudowire
	// needed, the patch cross-connects two local subinterfaces directly),
	// Remote when it's on a different device.
	PeerLocalIface string
	PeerLocalVLAN  int
	Remote         *ELINERemotePeer

	// StaleSubinterfaces lists "<iface>.<vlan>" subinterfaces a previous
	// apply of this same service (by Name) created on this device and the
	// current one no longer wants - e.g. after the service's interface or
	// VLAN was edited. ELINEApplier removes them in the same atomic
	// session as the rest of this apply, so a re-provision never leaves an
	// orphaned subinterface behind. See ELINERemoval for the case where a
	// whole device stops being an endpoint of this service.
	StaleSubinterfaces []ELINEStaleSubinterface
}

// ELINEStaleSubinterface identifies one "<iface>.<vlan>" subinterface to
// remove as part of an ELINE apply or removal.
type ELINEStaleSubinterface struct {
	Iface string
	VLAN  int
}

// ELINERemotePeer describes the ELINE's other endpoint when it's on a
// different device, reachable over an MPLS LDP pseudowire.
type ELINERemotePeer struct {
	NeighborIP   string
	PseudowireID int

	// MTU/ControlWord are EOS-only: its pseudowire config sets both
	// explicitly per service. SR OS doesn't use either - its spoke-sdp's
	// MTU and control-word are inherited from its apply-group instead (see
	// driver_nokia_sros_eline.go), so there's nowhere for a per-service
	// value to go on that platform even if a service needed a different
	// one.
	MTU         int
	ControlWord bool

	// DeviceName/RemoteIface/RemoteVLAN describe the far end for SR OS's
	// human-readable "PEER=<name> interface=<iface>.<vlan>" description
	// convention (driver_nokia_sros_eline.go, confirmed against a real
	// device) - EOS doesn't use them. RemoteIface/RemoteVLAN are the
	// remote device's own LocalIface/LocalVLAN, i.e. the other side's
	// intent - not to be confused with PeerLocalIface/PeerLocalVLAN above,
	// which is this same device's other local port in the same-device
	// case.
	DeviceName  string
	RemoteIface string
	RemoteVLAN  int
}

// ELINEApplier is implemented by drivers that can provision an ELINE.
// Deliberately not part of DriverClient: adding ELINE support to one
// driver should never force every other driver to grow a stub method.
// Callers type-assert for it (drv.(ELINEApplier)) and report a clean
// "not supported on this platform" error when the assertion fails.
type ELINEApplier interface {
	ApplyELINE(intent *ELINEIntent) error
}

// ELINERemoval is passed to ELINERemover.RemoveELINE to fully tear down a
// service's config on a device that no longer participates in it at all
// (its endpoint moved to a different device) - unlike ApplyELINE, nothing
// new is configured afterward.
type ELINERemoval struct {
	Name               string // ServiceID, matches the pseudowire/patch name ApplyELINE used
	StaleSubinterfaces []ELINEStaleSubinterface
}

// ELINERemover is implemented by drivers that can tear down an abandoned
// ELINE endpoint. Separate from ELINEApplier for the same reason
// ELINEApplier is separate from DriverClient: a driver can gain ELINE
// support incrementally. Callers type-assert for it the same way as
// ELINEApplier.
type ELINERemover interface {
	RemoveELINE(removal *ELINERemoval) error
}
