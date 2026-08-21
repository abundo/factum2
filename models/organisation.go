package models

import (
	"regexp"

	"gorm.io/gorm"
)

// serviceIDRe matches the <category><5 digits> shape used by factum-assigned
// service IDs. Older/Lime free-text IDs do not match.
var serviceIDRe = regexp.MustCompile(`^([A-Z]{2})(\d{5})$`)

// CategoryFromServiceID derives CN/CI/VL/VI/LF/LI from ServiceID's prefix.
// Returns "" for free-text / Lime IDs that don't match the shape.
func CategoryFromServiceID(serviceID string) string {
	m := serviceIDRe.FindStringSubmatch(serviceID)
	if m == nil {
		return ""
	}
	return m[1]
}

// OpticalServiceCategories are the prefixes that participate in fiber /
// wavelength impact. CN/CI are excluded.
var OpticalServiceCategories = map[string]bool{
	"VL": true, "VI": true, "LF": true, "LI": true,
}

// type Customer = limetool_models.LimeCompany

type Customer struct {
	FactumModel
	LastSync uint `json:"-"`

	Name string `json:"name"`

	Postaladdress1 string `json:"postal_address1"`
	Postaladdress2 string `json:"postal_address2"`
	Postalcity     string `json:"postalcity"`
	Postalzipcode  string `json:"postalzipcode"`
	Country        string `json:"country"`

	OrganizationNumber string `json:"organization_number"`

	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// BeforeCreate defaults a customer's Source to "factum" when nothing set it
// already. The Lime sync job (internal/lime/lime.go) always sets Source to
// "lime" itself before saving, so this only ever fires for customers created
// through the web API - CustomerDTO has no Source field for a caller to set,
// so without this default every manually-created customer would be
// indistinguishable (Source == "") from one whose sync origin just hasn't
// been recorded yet, instead of clearly marked as user-created.
func (c *Customer) BeforeCreate(tx *gorm.DB) error {
	if c.Source == "" {
		c.Source = "factum"
	}
	return nil
}

// CustomerDTO is the create/update request shape for the customer API,
// mirroring the fields the frontend's customer form actually edits
// (web/frontend/src/views/customer/CustomerList.vue). Source/SourceID are
// deliberately excluded: they're populated by the Lime sync job
// (internal/lime/lime.go), not something a caller creating or editing a
// customer through the web UI should be able to set - excluding them from
// the DTO means Update leaves a synced customer's Source/SourceID untouched
// (the JSON merge only overwrites fields present in the body), and Create
// leaves them at their zero value so BeforeCreate can default Source to
// "factum" for a manually-created customer.
type CustomerDTO struct {
	ID                 uint   `json:"id"`
	Name               string `json:"name"`
	Postaladdress1     string `json:"postal_address1"`
	Postaladdress2     string `json:"postal_address2"`
	Postalcity         string `json:"postalcity"`
	Postalzipcode      string `json:"postalzipcode"`
	Country            string `json:"country"`
	OrganizationNumber string `json:"organization_number"`
}

type Contact struct {
	FactumModel
	LastSync          uint   `json:"-"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	NotifyMaintenance bool   `json:"notify_maintenance"`
	Source            string `json:"source"`
	SourceID          string `json:"source_id"`
}

// BeforeCreate defaults a contact's Source to "factum" when nothing set it
// already. The Lime person-sync (internal/lime/lime.go) always sets Source
// to "lime" itself before saving, so this only ever fires for contacts
// created through the web API - ContactDTO has no Source field for a
// caller to set, so without this default every manually-created contact
// would be indistinguishable (Source == "") from one whose sync origin
// just hasn't been recorded yet, instead of clearly marked as user-created.
func (c *Contact) BeforeCreate(tx *gorm.DB) error {
	if c.Source == "" {
		c.Source = "factum"
	}
	return nil
}

// ContactDTO is the create/update request shape for the contact API,
// mirroring the fields the frontend's contact form actually edits
// (web/frontend/src/views/contact/ContactList.vue). Source/SourceID are
// deliberately excluded: they're populated by the Lime person-sync
// (internal/lime/lime.go), not something a caller creating or editing a
// contact through the web UI should be able to set - excluding them from
// the DTO means Update leaves a synced contact's Source/SourceID untouched
// (the JSON merge only overwrites fields present in the body), and Create
// leaves them at their zero value so BeforeCreate can default Source to
// "factum" for a manually-created contact. Name/email/phone on a Lime
// row are also ignored by ApiContactUpdate; only NotifyMaintenance is
// applied, since the next sync would otherwise discard those edits.
type ContactDTO struct {
	ID                uint   `json:"id"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`
	NotifyMaintenance bool   `json:"notify_maintenance"`
}

type Product struct {
	FactumModel
	LastSync uint   `json:"-"`
	Name     string `json:"name"`
}

type Service struct {
	FactumModel
	LastSync   uint   `json:"-"`
	Name       string `json:"name"`
	CustomerID uint   `json:"company"`
	Comment    string `json:"comment"`

	ServiceID       string `json:"service_id"`   // <category><5-digit>, e.g. CI00001 - the 2-letter prefix is the category (CI, VI, LI, ...), so it isn't stored separately
	ServiceType     string `json:"service_type"` // ELINE, ELAN, L3VPN, POLARIX - CI/CN only, else ""
	BandwidthMbps   int    `json:"bandwidth_mbps"`
	MaxMacAddresses int    `json:"max_mac_addresses"` // ELAN only, else 0

	DeliveryPoint1  string `json:"deliverypoint1"`
	DeliveryPoint2  string `json:"deliverypoint2"`
	Product         string `json:"product"`
	Service         string `json:"service"`
	AgreementStatus string `json:"agreement_status"` // Lime agreement_status.text (e.g. "Active"); empty for factum-created rows

	// ELINE endpoint/provisioning data (ServiceType == "ELINE" only, zero
	// otherwise) - set via PUT /service/:id/eline
	// (web/handler_service_eline.go), which also creates the matching
	// Netbox L2VPN + L2VPNTerminations. EndpointXInterfaceID is the
	// physical interface (factum Interface.ID) chosen by the user;
	// EndpointXSubinterfaceNetboxID is the Netbox ID of the per-VLAN
	// subinterface created to carry the L2VPN termination.
	EndpointADeviceID             uint `json:"endpoint_a_device_id"`
	EndpointAInterfaceID          uint `json:"endpoint_a_interface_id"`
	EndpointAVlan                 int  `json:"endpoint_a_vlan"`
	EndpointASubinterfaceNetboxID uint `json:"endpoint_a_subinterface_netbox_id"`
	EndpointATerminationNetboxID  uint `json:"endpoint_a_termination_netbox_id"`

	EndpointBDeviceID             uint `json:"endpoint_b_device_id"`
	EndpointBInterfaceID          uint `json:"endpoint_b_interface_id"`
	EndpointBVlan                 int  `json:"endpoint_b_vlan"`
	EndpointBSubinterfaceNetboxID uint `json:"endpoint_b_subinterface_netbox_id"`
	EndpointBTerminationNetboxID  uint `json:"endpoint_b_termination_netbox_id"`

	// AppliedEndpointX* record what's actually live on the devices as of
	// the last successful PUT .../eline/push (web/handler_service_eline.go,
	// ApiServiceElinePush) - distinct from EndpointX* above, which is the
	// desired state Netbox/the DB already reflects the moment PUT
	// .../eline runs. A zero AppliedEndpointXDeviceID means this side has
	// never been pushed yet. Comparing Applied* against Endpoint* on each
	// push is what lets a re-provision (interface/VLAN/device edit) find
	// and remove the stale subinterface/pseudowire/patch a previous push
	// left behind, on whichever device still has it - see
	// elineComputeStale. Only advanced once the corresponding push (and
	// any stale cleanup it required) actually succeeds, so a partial
	// failure is retried on the next push rather than silently forgotten.
	AppliedEndpointADeviceID uint   `json:"-"`
	AppliedEndpointAIface    string `json:"-"`
	AppliedEndpointAVlan     int    `json:"-"`
	AppliedEndpointBDeviceID uint   `json:"-"`
	AppliedEndpointBIface    string `json:"-"`
	AppliedEndpointBVlan     int    `json:"-"`

	// PseudowireID is derived from ServiceID (pseudowireIDFromServiceID,
	// web/handler_service_eline.go) and stored as the Netbox L2VPN's
	// identifier.
	PseudowireID  int  `json:"pseudowire_id"`
	L2VPNNetboxID uint `json:"l2vpn_netbox_id"`

	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// BeforeCreate defaults a service's Source to "factum" when nothing set it
// already. The Lime sync job (internal/lime/lime.go) always sets Source to
// "lime" itself before saving, so this only ever fires for services created
// through the web API - ServiceDTO has no Source field for a caller to set,
// so without this default every manually-created service would be
// indistinguishable (Source == "") from one whose sync origin just hasn't
// been recorded yet, instead of clearly marked as user-created.
func (s *Service) BeforeCreate(tx *gorm.DB) error {
	if s.Source == "" {
		s.Source = "factum"
	}
	return nil
}

// ServiceDTO is the create/update request shape for the service API,
// mirroring the fields the frontend's service form actually edits
// (web/frontend/src/views/service/ServiceList.vue,
// web/frontend/src/views/service/ServiceCreateWizard.vue) - it shows
// "source" and "agreement_status" as disabled/display-only fields, so, like
// ContactDTO, Source/SourceID/AgreementStatus are excluded here rather than
// merely disabled client-side: the sync-managed values survive an Update
// untouched, and a manually-created service gets the zero value instead of
// a caller being able to fake a Lime origin or agreement status.
//
// On create (ApiServiceCreate), ServiceID is optional: a blank
// value has the backend auto-assign the next <category><5-digit> number for
// Category, rather than the wizard reserving one up front.
type ServiceDTO struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	CustomerID      uint   `json:"company"`
	Comment         string `json:"comment"`
	ServiceID       string `json:"service_id"`
	Category        string `json:"category"`
	ServiceType     string `json:"service_type"`
	BandwidthMbps   int    `json:"bandwidth_mbps"`
	MaxMacAddresses int    `json:"max_mac_addresses"`
	DeliveryPoint1  string `json:"deliverypoint1"`
	DeliveryPoint2  string `json:"deliverypoint2"`
	Product         string `json:"product"`
	Service         string `json:"service"`
}

// -------
type Agreement struct {
	FactumModel
	LastSync    uint          `json:"-"`
	Monthly_fee int           `json:"montly_fee"`
	Onetime_fee int           `json:"onetime_fee"`
	QOS         *AgreementQoS `gorm:"-" json:"qos"`
}
type AgreementQoS struct {
	FactumModel
	LastSync uint   `json:"-"`
	Key      string `json:"key"`
	Text     string `json:"text"`
}
