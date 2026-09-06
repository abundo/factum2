package models

import (
	"time"
)

type FactumModel struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// User represents an application user
type User struct {
	FactumModel
	Username     string  `gorm:"uniqueIndex;not null"  json:"username"`
	PasswordHash string  `gorm:"not null" json:"-"`
	Name         string  `gorm:"not null" json:"name"`
	Email        string  `json:"email"`
	Mobile       string  `json:"mobile"`
	Roles        []*Role `gorm:"many2many:user_roles;;constraint:OnDelete:CASCADE;" json:"-"`
}

// PasswordResetToken backs the forgot-password flow (web.ApiForgotPassword/
// ApiResetPassword). One row per outstanding request: TokenHash matches the
// long random token embedded in the emailed link, CodeHash matches the
// short human-typeable code shown alongside it - both derive from the same
// crypto/rand-generated secrets, so a fast deterministic hash is fine for
// both (unlike a user-chosen password, there's no dictionary smaller than
// the keyspace to attack). Either one redeems the request; redeeming
// consumes it.
type PasswordResetToken struct {
	FactumModel
	UserID    uint      `gorm:"index;not null" json:"-"`
	TokenHash string    `gorm:"uniqueIndex;not null" json:"-"`
	CodeHash  string    `gorm:"not null" json:"-"`
	ExpiresAt time.Time `json:"-"`
	// Attempts counts failed code redemption tries against this row - the
	// code is only 8 digits (looked up by email, not by hash), so this
	// caps brute-forcing it before ExpiresAt would otherwise.
	Attempts   int        `json:"-"`
	ConsumedAt *time.Time `json:"-"`
}

type UserDTO struct {
	ID       uint     `json:"id"`
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Mobile   string   `json:"mobile"`
	Roles    []string `json:"roles"`
	RoleIDs  []uint   `json:"role_ids"`

	// Password is only used as input on create, and as an optional reset on update.
	// It is never populated on output (the model's PasswordHash is json:"-").
	Password string `json:"password,omitempty"`
}

// Role defines user roles (e.g., admin, user)
type Role struct {
	FactumModel
	Name        string  `gorm:"uniqueIndex;not null" json:"name"`
	Description string  `json:"description"`
	Users       []*User `gorm:"many2many:user_roles;"  json:"-"`
}

type RoleDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LdapRoleMapping maps one LDAP/AD group DN to one Factum Role. Re-evaluated
// on every successful LDAP-backed login (see web/auth.go's syncLDAPRoles),
// so role changes in the directory propagate automatically without local
// admin action.
type LdapRoleMapping struct {
	FactumModel
	GroupDN string `gorm:"uniqueIndex;not null" json:"group_dn"`
	RoleID  uint   `gorm:"not null" json:"role_id"`
}

type LdapRoleMappingDTO struct {
	ID      uint   `json:"id"`
	GroupDN string `json:"group_dn"`
	RoleID  uint   `json:"role_id"`
}

// WorkerNode is a remote factum2-worker host the primary dials out to over a
// WSS connection (internal/worker.RemoteManager) - the reverse of a
// worker dialing in, so the firewall hole is a single narrow rule on the
// worker host (source = primary) rather than any inbound rule on the
// primary. Admin-editable list, not local YAML, since it's just "who to
// dial", not a security boundary tied to the primary host the way an
// agent's own worker.commands allowlist is.
type WorkerNode struct {
	FactumModel
	Name    string `gorm:"uniqueIndex;not null" json:"name"`
	Address string `json:"address"` // host:port, dialed as wss://<Address><worker.HubPath>
	Token   string `json:"-"`       // shared secret sent as "Authorization: Bearer <Token>"; never serialized
	Enabled bool   `json:"enabled"`
	// TLSSkipVerify disables hub certificate verification for this node.
	// The channel is still encrypted, but a MITM with any cert is accepted;
	// prefer TLSCA. Intended for lab/self-signed certs whose SAN does not
	// match Address.
	TLSSkipVerify bool `json:"tls_skip_verify" gorm:"column:tls_skip_verify"`
	// TLSCA is optional PEM used as the TLS trust root when verifying this
	// node's hub certificate. Empty uses the system CA pool. Typically the
	// worker's self-signed hub.crt, or an internal CA. Not a secret.
	TLSCA string `json:"tls_ca" gorm:"column:tls_ca;type:text"`
}

type WorkerNodeDTO struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	// Token is omitempty so the frontend can leave it blank on Update to
	// keep the existing value - handle_crud.go's Update loads the row
	// before merging the DTO's JSON on top, so an absent key never
	// overwrites the stored token (same pattern as UserDTO.Password).
	Token         string `json:"token,omitempty"`
	Enabled       bool   `json:"enabled"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
	TLSCA         string `json:"tls_ca"`
}

// DeviceSyncAuth is one set of device-login credentials internal/device-sync
// uses to connect directly to a device (SSH/NETCONF/eAPI) - distinct from
// Netbox's own API credentials (Settings.NetboxApi*). Name is either a
// device name (an override for that one device) or the literal "default",
// used as the fallback for any device without its own row. Admin-editable
// list, same shape as WorkerNode (Password is json:"-" for the same reason
// WorkerNode.Token is - never leak it via GetAll/GetOne).
type DeviceSyncAuth struct {
	FactumModel
	Name     string `gorm:"uniqueIndex;not null" json:"name"`
	Username string `json:"username"`
	Password string `json:"-"`
}

type DeviceSyncAuthDTO struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	// Password is omitempty so the frontend can leave it blank on Update to
	// keep the existing value - same pattern as WorkerNodeDTO.Token.
	Password string `json:"password,omitempty"`
}

// Link is one admin-managed shortcut shown on the dashboard (Admin ->
// Settings -> Dashboard), grouped under Group (a free-text section
// heading) and ordered within the whole list by Position - see
// web/handle_links.go's ApiLinkCreate/ApiLinksReorder for how the two are
// maintained. Readable by every logged-in user (GET /api/links), editable
// by admins only (/api/admin/links).
type Link struct {
	FactumModel
	Group        string `gorm:"index;not null" json:"group"`
	Name         string `gorm:"not null" json:"name"`
	URL          string `gorm:"not null" json:"url"`
	OpenInNewTab bool   `json:"open_in_new_tab"`
	// Icon is an optional "data:<mime>;base64,..." URI, uploaded client-side
	// and stored inline rather than as a file on disk - a release build's
	// static assets are compiled into the binary (web/fs_release.go) and
	// read-only at runtime, so there's nowhere to write an uploaded file to.
	Icon     string `gorm:"type:text" json:"icon,omitempty"`
	Position int    `json:"position"`
}

type LinkDTO struct {
	ID           uint   `json:"id"`
	Group        string `json:"group"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	OpenInNewTab bool   `json:"open_in_new_tab"`
	Icon         string `json:"icon,omitempty"`
	// Position is omitempty so the generic Update handler's DTO<->model
	// round trip (web/handle_crud.go) leaves the stored value alone when a
	// regular edit omits it - only ApiLinkCreate (append) and
	// ApiLinksReorder (drag-and-drop) ever set it, same convention as
	// WorkerNodeDTO.Token/DeviceSyncAuthDTO.Password leaving those fields
	// unchanged when blank.
	Position int `json:"position,omitempty"`
}

// Job is one triggered unit of work (e.g. one click of "Sync DNS", or one
// "Sync all"), made up of one or more JobTasks - one per target. Type lets
// future non-sync job kinds reuse this table without another schema
// change; "sync" is the only value produced today.
type Job struct {
	FactumModel
	Type        string     `gorm:"index;not null;default:sync" json:"type"`
	TriggeredBy string     `json:"triggered_by"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	// ExpectedTasks is the total number of JobTasks this job will end up
	// with, set once at creation. A sequential batch (StartJob's
	// multi-target path) creates its JobTask rows one at a time as each
	// target's turn comes up rather than all at once, so
	// RemoteManager.resolveTask can't tell "done" from "no rows created
	// yet" by counting unfinished rows alone - it also checks the finished
	// row count against ExpectedTasks. Internal bookkeeping, not part of
	// the REST API surface (json:"-"), like JobTask.TaskID.
	ExpectedTasks int       `json:"-"`
	Tasks         []JobTask `gorm:"foreignKey:JobID" json:"tasks,omitempty"`
}

// JobTask is one dispatched run of a single target (web.ApiSyncTrigger/
// ApiSyncTriggerAll) within a Job - the direct successor of the old
// SyncJob row, minus TriggeredBy (now only on the parent Job). TaskID is
// the same ID used for wire-protocol correlation over the hub transport
// (internal/worker/hub.go's StartCommand/RunAndWait), not a separate ID
// space - it's an internal transport detail, not part of the REST API
// surface (json:"-"), which addresses tasks by their ordinary numeric ID
// like every other resource. FinishedAt is set by the same code path that
// already detects command completion (internal/worker.LogMsg's StreamExit
// line) - see RemoteManager.resolveTask. ErrorCount/WarningCount are
// denormalized, incremented alongside each JobTaskEvent insert
// (RemoteManager's EnvelopeEvent handler) so the job list can show counts
// without a live COUNT(*) over events on every poll.
type JobTask struct {
	FactumModel
	JobID        uint       `gorm:"index;not null" json:"job_id"`
	TaskID       string     `gorm:"uniqueIndex;not null" json:"-"`
	Target       string     `gorm:"index" json:"target"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ExitCode     int        `json:"exit_code"`
	Err          string     `json:"err,omitempty"`
	ErrorCount   int        `gorm:"not null;default:0" json:"error_count"`
	WarningCount int        `gorm:"not null;default:0" json:"warning_count"`
}

// JobTaskEvent is one info/warning/error line reported by a sync tool via
// internal/jobevent.Reporter, relayed over the hub transport's "event"
// envelope and persisted by internal/worker.RemoteManager. JobTaskID is
// nullable - a --job-flagged command run outside a tracked job (e.g. ad
// hoc "factum2-worker run") still produces rows here with no owning
// JobTask, they're just never queried since nothing links to them from
// the UI.
type JobTaskEvent struct {
	FactumModel
	JobTaskID *uint     `gorm:"index" json:"job_task_id,omitempty"`
	TaskID    string    `gorm:"index;not null" json:"-"`
	Target    string    `json:"target"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

// JobSchedule is one user-defined periodic trigger for a job, the in-app
// replacement for a crontab entry on the primary. Target is either one
// worker.IsValidJobTarget name (a single-target StartJob, same as clicking
// one tile on Job overview - sync targets plus "housekeeping"), or the
// sentinel "all" (SequencedSyncAllTargets + StartJob, same as "Sync all";
// housekeeping is not included). Cron is a 5-field expression evaluated
// in Europe/Stockholm; NextRunAt is computed at create/update and advanced
// by the scheduler when a run is claimed so a restart only ever catch-up
// fires once, not once per missed tick.
type JobSchedule struct {
	FactumModel
	Name    string `gorm:"not null" json:"name"`
	Enabled bool   `json:"enabled"`
	Target  string `gorm:"not null;index" json:"target"`
	Cron    string `gorm:"not null" json:"cron"`
	// LastRunAt is when the scheduler last claimed a due run - it is
	// stamped even if StartJob then fails (LastError), so a busy target
	// doesn't get retried every tick.
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	NextRunAt *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	CreatedBy string     `json:"created_by"`
}

// JobScheduleDTO is the writable subset of JobSchedule - LastRunAt/
// NextRunAt/LastError/CreatedBy are server-owned, same reason LinkDTO
// omits Position from ordinary edits.
type JobScheduleDTO struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
	Cron    string `json:"cron"`
}

// Settings is stored in database as a single row (id=1)
type Settings struct {
	FactumModel

	// features activated
	BecsEnabled   *bool `gorm:"column:becs_enabled" form:"becs_enabled" json:"becs_enabled"`
	DnsEnabled    *bool `gorm:"column:dns_enabled" form:"dns_enabled" json:"dns_enabled"`
	IcingaEnabled *bool `gorm:"column:icinga_enabled" form:"icinga_enabled" json:"icinga_enabled"`
	// OpticalEnabled gates WDM / ROADM / wavelength-path UI and APIs.
	// Off (nil/false) is the default. Same *bool shape as the other flags.
	OpticalEnabled *bool `gorm:"column:optical_enabled" form:"optical_enabled" json:"optical_enabled"`
	// IpamEnabled gates the standalone IPAM UI and /api/ipam/* routes.
	// Off (nil/false) is the default. Turning it off only hides the
	// feature — namespaces, VRFs and prefixes stay in the database.
	IpamEnabled *bool `gorm:"column:ipam_enabled" form:"ipam_enabled" json:"ipam_enabled"`
	// OrganizationEnabled gates the Organization menu (Customers, Contacts)
	// in the web GUI. Off (nil/false) is the default. Turning it off only
	// hides the menu — customer and contact rows stay in the database, and
	// services may still reference them.
	OrganizationEnabled *bool `gorm:"column:organization_enabled" form:"organization_enabled" json:"organization_enabled"`
	LibrenmsEnabled     *bool `gorm:"column:librenms_enabled" form:"librenms_enabled" json:"librenms_enabled"`
	LimeEnabled         *bool `gorm:"column:lime_enabled" form:"lime_enabled" json:"lime_enabled"`
	NetboxEnabled       *bool `gorm:"column:netbox_enabled" form:"netbox_enabled" json:"netbox_enabled"`
	OxidizedEnabled     *bool `gorm:"column:oxidized_enabled" form:"oxidized_enabled" json:"oxidized_enabled"`
	PrometheusEnabled   *bool `gorm:"column:prometheus_enabled" form:"prometheus_enabled" json:"prometheus_enabled"`
	DeviceSyncEnabled   *bool `gorm:"column:device_sync_enabled" form:"device_sync_enabled" json:"device_sync_enabled"`

	// factum
	FactumApiToken string `gorm:"column:factum_api_token" form:"factum_api_token" json:"factum_api_token"`
	// PublicBaseURL is the externally-reachable origin (e.g.
	// "https://factum.example.com") used to build absolute links in
	// outgoing email, currently just the password-reset link
	// (web.ApiForgotPassword). If left blank, the reset handler falls back
	// to deriving an origin from the incoming request instead - fine for
	// simple setups, but a deployment behind a reverse proxy should set
	// this explicitly rather than trusting Host/X-Forwarded-* headers for
	// something that ends up in an email.
	PublicBaseURL string `gorm:"column:public_base_url" form:"public_base_url" json:"public_base_url"`
	// DefaultDomain is shared by every downstream sync target
	// (DNS/Icinga/LibreNMS/Oxidized/Prometheus), not just DNS - it's the
	// $ORIGIN of the generated DNS zone file, and also used to match factum
	// device names against fully-qualified DNS names during Icinga/LibreNMS/
	// Oxidized/Prometheus sync. It lives here rather than in local YAML
	// config so each CLI tool can fetch it over REST (via util.CommonConfig)
	// like the rest of its settings, since they typically run on a different
	// host than the primary.
	DefaultDomain string `gorm:"column:default_domain" form:"default_domain" json:"default_domain"`
	// JobHistoryKeep is how many newest finished Jobs (and their
	// JobTask/JobTaskEvent rows) the housekeeping target keeps. Values < 1
	// are treated as 50 by housekeeping.Trim (the same cap GET /api/jobs
	// lists), so an unset column cannot wipe history on the first run.
	// Unfinished jobs are never deleted. The task does not run on its
	// own - schedule or trigger "housekeeping" like any other job target.
	JobHistoryKeep int `gorm:"column:job_history_keep" form:"job_history_keep" json:"job_history_keep"`

	// BECS
	BecsEapiURL  string `gorm:"column:becs_eapi_url" form:"becs_eapi_url" json:"becs_eapi_url"`
	BecsEapiUser string `gorm:"column:becs_eapi_user" form:"becs_eapi_user" json:"becs_eapi_user"`
	BecsEapiPass string `gorm:"column:becs_eapi_pass" form:"becs_eapi_pass" json:"becs_eapi_pass"`
	BecsEapiOID  uint   `gorm:"column:becs_eapi_oid" form:"becs_eapi_oid" json:"becs_eapi_oid"`

	// DNS
	DnsDestFile string `gorm:"column:dns_dest_file" form:"dns_dest_file" json:"dns_dest_file"`
	// IgnoreModels/IgnorePlatforms are newline-separated lists (one model or
	// platform name per line), edited as a multiline box in the admin UI -
	// internal/dns.filterDevices skips a device matching either.
	DnsIgnoreModels    string `gorm:"column:dns_ignore_models;type:text" form:"dns_ignore_models" json:"dns_ignore_models"`
	DnsIgnorePlatforms string `gorm:"column:dns_ignore_platforms;type:text" form:"dns_ignore_platforms" json:"dns_ignore_platforms"`

	// Email / SMTP - a general-purpose outbound mail relay, not tied to
	// Icinga specifically (factum2-icinga-notifications is the first
	// consumer, via util.CommonConfig, but any tool that needs to send
	// mail can fetch the same settings). Edited on the admin UI's
	// Factum > Email tab.
	SmtpHost string `gorm:"column:smtp_host" form:"smtp_host" json:"smtp_host"`
	SmtpPort uint16 `gorm:"column:smtp_port" form:"smtp_port" json:"smtp_port"`
	SmtpUser string `gorm:"column:smtp_user" form:"smtp_user" json:"smtp_user"`
	SmtpPass string `gorm:"column:smtp_pass" form:"smtp_pass" json:"smtp_pass"`
	// SmtpTLSMode is "none" | "starttls" | "tls", same convention as LdapTLSMode.
	SmtpTLSMode string `gorm:"column:smtp_tls_mode" form:"smtp_tls_mode" json:"smtp_tls_mode"`
	// EmailSender is the default From address for outgoing notification email.
	EmailSender string `gorm:"column:email_sender" form:"email_sender" json:"email_sender"`

	// Icinga
	IcingaApiURL    string `gorm:"column:icinga_api_url" form:"icinga_api_url" json:"icinga_api_url"`
	IcingaApiUser   string `gorm:"column:icinga_api_user" form:"icinga_api_user" json:"icinga_api_user"`
	IcingaApiPass   string `gorm:"column:icinga_api_pass" form:"icinga_api_pass" json:"icinga_api_pass"`
	IcingaHostsFile string `gorm:"column:icinga_hosts_file" form:"icinga_hosts_file" json:"icinga_hosts_file"`
	IcingaUsersFile string `gorm:"column:icinga_users_file" form:"icinga_users_file" json:"icinga_users_file"`
	// IcingaIgnoreDevices is a newline-separated list of device names (one
	// per line) that factum2-icinga's Update() skips entirely.
	IcingaIgnoreDevices string `gorm:"column:icinga_ignore_devices;type:text" form:"icinga_ignore_devices" json:"icinga_ignore_devices"`
	// IcingaDefaultNotification is Go text/template executed with .Device
	// for a host that has no cf_alarm_destination, e.g.
	// `  vars.pe_notify_default = true`. Literal Icinga with no {{ }} is
	// fine. The rendered lines are inserted into the host object (see
	// internal/icinga/factum2-icinga.go).
	IcingaDefaultNotification string `gorm:"column:icinga_default_notification;type:text" form:"icinga_default_notification" json:"icinga_default_notification"`
	// IcingaHostTemplate/IcingaDependencyTemplate/IcingaUserTemplate are
	// Go text/template (see internal/icinga/factum2-icinga.go for the data
	// each is executed with).
	IcingaHostTemplate       string `gorm:"column:icinga_host_template;type:text" form:"icinga_host_template" json:"icinga_host_template"`
	IcingaDependencyTemplate string `gorm:"column:icinga_dependency_template;type:text" form:"icinga_dependency_template" json:"icinga_dependency_template"`
	IcingaUserTemplate       string `gorm:"column:icinga_user_template;type:text" form:"icinga_user_template" json:"icinga_user_template"`

	// Librenms
	LibrenmsApiURL   string `gorm:"column:librenms_api_url" form:"librenms_api_url" json:"librenms_api_url"`
	LibrenmsApiToken string `gorm:"column:librenms_api_token" form:"librenms_api_token" json:"librenms_api_token"`
	// LibrenmsPersistentDevices is a newline-separated list, edited as a
	// multiline box in the admin UI, same convention as the Ignore* fields
	// above. FactumLibrenmsClient.Sync never quarantines or deletes a
	// LibreNMS device whose hostname or display name matches a line here.
	LibrenmsPersistentDevices string `gorm:"column:librenms_persistent_devices;type:text" form:"librenms_persistent_devices" json:"librenms_persistent_devices"`
	// LibrenmsDelayedDeleteEnabled gates the LibreNMS delete path. Off
	// (nil/false, the default) means sync never removes a device. On means
	// a delete candidate is quarantined (polling and alerts off, display
	// name stamped with the scheduled date) and actually deleted on a later
	// sync once ScheduledAt has passed, or sooner if the user sets
	// LibrenmsPendingDelete.ForceDelete. Same *bool shape as the other flags.
	LibrenmsDelayedDeleteEnabled *bool `gorm:"column:librenms_delayed_delete_enabled" form:"librenms_delayed_delete_enabled" json:"librenms_delayed_delete_enabled"`
	// LibrenmsDelayedDeleteDays is how long a quarantined device stays in
	// LibreNMS before the next sync deletes it. Values < 1 are treated as
	// 30 by sync (the documented default), so an unset column cannot
	// accidentally delete on the following run.
	LibrenmsDelayedDeleteDays int `gorm:"column:librenms_delayed_delete_days" form:"librenms_delayed_delete_days" json:"librenms_delayed_delete_days"`
	// LibrenmsRolesEnabled/InterfacesDisabled are newline-separated lists of
	// regexes (one per line) - FactumLibrenmsClient.syncInterfaces compiles
	// each line and matches it against an interface's factum role
	// (RolesEnabled) or name (InterfacesDisabled) to force alerting on/off
	// respectively, overriding the device's CfAlarmInterfaces default.
	LibrenmsRolesEnabled       string `gorm:"column:librenms_roles_enabled;type:text" form:"librenms_roles_enabled" json:"librenms_roles_enabled"`
	LibrenmsInterfacesDisabled string `gorm:"column:librenms_interfaces_disabled;type:text" form:"librenms_interfaces_disabled" json:"librenms_interfaces_disabled"`
	// LibrenmsSNMPVersion is the SNMP version (e.g. "v1", "v2c", "v3") used
	// when creating devices in LibreNMS.
	LibrenmsSNMPVersion string `gorm:"column:librenms_snmp_version" form:"librenms_snmp_version" json:"librenms_snmp_version"`
	// LibrenmsSNMPCommunities is a newline-separated list of SNMP
	// communities (one per line, tried in order), same convention as the
	// Ignore* fields above - FactumLibrenmsClient.Sync tries each in turn
	// when creating a device in LibreNMS, force-adding with the first one
	// if none succeed.
	LibrenmsSNMPCommunities string `gorm:"column:librenms_snmp_communities;type:text" form:"librenms_snmp_communities" json:"librenms_snmp_communities"`

	// Lime
	LimeApiURL   string `gorm:"column:lime_api_url" form:"lime_api_url" json:"lime_api_url"`
	LimeApiToken string `gorm:"column:lime_api_token" form:"lime_api_token" json:"lime_api_token"`

	// Netbox
	NetboxApiURL   string `gorm:"column:netbox_api_url" form:"netbox_api_url" json:"netbox_api_url"`
	NetboxApiToken string `gorm:"column:netbox_api_token" form:"netbox_api_token" json:"netbox_api_token"`
	// NetboxWebhookSecret is the shared secret Netbox signs its webhook
	// request bodies with (HMAC-SHA512, "X-Hook-Signature" header) - see
	// web.ApiNetboxWebhook. Configured the same way on the Netbox side, as
	// the webhook's "secret" field.
	NetboxWebhookSecret string `gorm:"column:netbox_webhook_secret" form:"netbox_webhook_secret" json:"netbox_webhook_secret"`
	// NetboxSyncCustomersEnabled, when set, makes FactumSyncNetbox also
	// push factum customers to Netbox as tenants (custom fields
	// source/source_id identify which customer a tenant came from).
	NetboxSyncCustomersEnabled *bool `gorm:"column:netbox_sync_customers_enabled" form:"netbox_sync_customers_enabled" json:"netbox_sync_customers_enabled"`
	// NetboxSyncContactsEnabled, when set, makes FactumSyncNetbox also
	// push factum contacts to Netbox as contacts (same source/source_id
	// custom fields as tenants). CustomerContact links become contact
	// assignments on the matching tenant when that tenant exists.
	NetboxSyncContactsEnabled *bool `gorm:"column:netbox_sync_contacts_enabled" form:"netbox_sync_contacts_enabled" json:"netbox_sync_contacts_enabled"`

	// Oxidized
	OxidizedApiURL  string `gorm:"column:oxidized_api_url" form:"oxidized_api_url" json:"oxidized_api_url"`
	OxidizedApiUser string `gorm:"column:oxidized_api_user" form:"oxidized_api_user" json:"oxidized_api_user"`
	OxidizedApiPass string `gorm:"column:oxidized_api_pass" form:"oxidized_api_pass" json:"oxidized_api_pass"`
	// OxidizedDestFile is oxidized's own router.db - internal/oxidized
	// writes the filtered device list here (name:ip:model per line).
	// Oxidized's CSV source must map name: 0, ip: 1, model: 2.
	OxidizedDestFile string `gorm:"column:oxidized_dest_file" form:"oxidized_dest_file" json:"oxidized_dest_file"`
	// OxidizedIgnoreDevices/Manufacturers/Models/Platforms are
	// newline-separated lists, edited as multiline boxes in the admin UI -
	// a device matching any one of them is skipped by
	// FactumOxidizedClient.Sync.
	OxidizedIgnoreDevices       string `gorm:"column:oxidized_ignore_devices;type:text" form:"oxidized_ignore_devices" json:"oxidized_ignore_devices"`
	OxidizedIgnoreManufacturers string `gorm:"column:oxidized_ignore_manufacturers;type:text" form:"oxidized_ignore_manufacturers" json:"oxidized_ignore_manufacturers"`
	OxidizedIgnoreModels        string `gorm:"column:oxidized_ignore_models;type:text" form:"oxidized_ignore_models" json:"oxidized_ignore_models"`
	OxidizedIgnorePlatforms     string `gorm:"column:oxidized_ignore_platforms;type:text" form:"oxidized_ignore_platforms" json:"oxidized_ignore_platforms"`

	// Prometheus + snmp_exporter. factum2-prometheus writes a Prometheus
	// file_sd JSON of SNMP targets (Settings.PrometheusDestFile) from
	// devices flagged CfMonitorGrafana, then POSTs PrometheusReloadURL
	// if the file changed and a URL is set. Module/Auth are snmp_exporter
	// names (snmp.yml), not community strings.
	PrometheusDestFile string `gorm:"column:prometheus_dest_file" form:"prometheus_dest_file" json:"prometheus_dest_file"`
	// PrometheusReloadURL is the full URL POSTed after a file change
	// (typically http://127.0.0.1:9090/-/reload, which needs Prometheus
	// --web.enable-lifecycle). Empty skips the reload - file_sd still
	// re-reads the dest file on its own refresh interval.
	PrometheusReloadURL string `gorm:"column:prometheus_reload_url" form:"prometheus_reload_url" json:"prometheus_reload_url"`
	// PrometheusModule is the snmp_exporter module name stamped on every
	// target (e.g. "if_mib"). Empty is treated as "if_mib" at write time.
	PrometheusModule string `gorm:"column:prometheus_module" form:"prometheus_module" json:"prometheus_module"`
	// PrometheusAuth is the snmp_exporter auth name stamped on every
	// target (an auths: key in snmp.yml, e.g. "public_v2"). Empty is
	// treated as "public_v2" at write time.
	PrometheusAuth string `gorm:"column:prometheus_auth" form:"prometheus_auth" json:"prometheus_auth"`
	// PrometheusIgnoreDevices/Manufacturers/Models/Platforms are
	// newline-separated lists, same convention as Oxidized's Ignore* -
	// a device matching any one of them is skipped by
	// FactumPrometheusClient.Sync.
	PrometheusIgnoreDevices       string `gorm:"column:prometheus_ignore_devices;type:text" form:"prometheus_ignore_devices" json:"prometheus_ignore_devices"`
	PrometheusIgnoreManufacturers string `gorm:"column:prometheus_ignore_manufacturers;type:text" form:"prometheus_ignore_manufacturers" json:"prometheus_ignore_manufacturers"`
	PrometheusIgnoreModels        string `gorm:"column:prometheus_ignore_models;type:text" form:"prometheus_ignore_models" json:"prometheus_ignore_models"`
	PrometheusIgnorePlatforms     string `gorm:"column:prometheus_ignore_platforms;type:text" form:"prometheus_ignore_platforms" json:"prometheus_ignore_platforms"`

	// Device Sync (internal/device-sync) - VRFInGlobal/DeviceStates/
	// DeviceIgnore are newline-separated lists, edited as multiline boxes in
	// the admin UI, same convention as Oxidized's Ignore* fields above.
	// Per-device login credentials aren't here - see DeviceSyncAuth.
	//
	// DeviceSyncVRFInGlobal lists VRF names whose addresses are allocated in
	// the global routing table on these devices (to avoid duplicate-address
	// rejections in Netbox) - device-sync compares/creates addresses in
	// those VRFs as if they had no VRF at all.
	DeviceSyncVRFInGlobal string `gorm:"column:device_sync_vrf_in_global;type:text" form:"device_sync_vrf_in_global" json:"device_sync_vrf_in_global"`
	// DeviceSyncDeviceStates lists the Netbox device status values (e.g.
	// "Active") eligible for sync - a device in any other state is skipped.
	DeviceSyncDeviceStates string `gorm:"column:device_sync_device_states;type:text" form:"device_sync_device_states" json:"device_sync_device_states"`
	// DeviceSyncDeviceIgnore lists device names to always skip.
	DeviceSyncDeviceIgnore string `gorm:"column:device_sync_device_ignore;type:text" form:"device_sync_device_ignore" json:"device_sync_device_ignore"`
	// DeviceSyncVlanGroupName is the Netbox VLAN Group (global, not scoped
	// to a site) that every VLAN discovered across all synced devices is
	// created in, along with each interface's untagged/tagged VLAN
	// assignment. Empty disables VLAN/interface-VLAN sync entirely.
	DeviceSyncVlanGroupName string `gorm:"column:device_sync_vlan_group_name;type:varchar(255)" form:"device_sync_vlan_group_name" json:"device_sync_vlan_group_name"`

	// LDAP / Active Directory authentication + authorization. Connection
	// fields are edited from the admin "Authentication" page,
	// LdapDefaultRoleID from the "Authorization" page - both just load/save
	// this same Settings singleton, like every other integration here.
	LdapEnabled *bool `gorm:"column:ldap_enabled" form:"ldap_enabled" json:"ldap_enabled"`
	// LdapServerType is "ad" | "generic" (OpenLDAP-compatible). Drives
	// server-specific behavior that differs between the two (e.g. how a
	// password change is performed) rather than being purely cosmetic.
	LdapServerType string `gorm:"column:ldap_server_type" form:"ldap_server_type" json:"ldap_server_type"`
	LdapHost       string `gorm:"column:ldap_host" form:"ldap_host" json:"ldap_host"`
	LdapPort       uint16 `gorm:"column:ldap_port" form:"ldap_port" json:"ldap_port"`
	// LdapHost2/LdapPort2 are an optional second LDAP/AD server for
	// redundancy. Directory operations try LdapHost first and fall back to
	// LdapHost2 if it is unreachable. LdapPort2 of 0 means "same as
	// LdapPort". TLS, bind DN, base DN and filters are shared - both
	// servers are assumed to be replicas of the same directory.
	LdapHost2 string `gorm:"column:ldap_host2" form:"ldap_host2" json:"ldap_host2"`
	LdapPort2 uint16 `gorm:"column:ldap_port2" form:"ldap_port2" json:"ldap_port2"`
	// LdapTLSMode is "none" | "starttls" | "ldaps".
	LdapTLSMode       string `gorm:"column:ldap_tls_mode" form:"ldap_tls_mode" json:"ldap_tls_mode"`
	LdapSkipTLSVerify *bool  `gorm:"column:ldap_skip_tls_verify" form:"ldap_skip_tls_verify" json:"ldap_skip_tls_verify"`
	// LdapBindDN/LdapBindPassword are the search identity used for the
	// search+bind flow (see internal/ldapauth.Authenticate) - stored in
	// plaintext like every other integration credential in this struct.
	// Both may be blank for an anonymous bind, which requires the
	// directory to allow anonymous search under LdapBaseDN.
	LdapBindDN       string `gorm:"column:ldap_bind_dn" form:"ldap_bind_dn" json:"ldap_bind_dn"`
	LdapBindPassword string `gorm:"column:ldap_bind_password" form:"ldap_bind_password" json:"ldap_bind_password"`
	LdapBaseDN       string `gorm:"column:ldap_base_dn" form:"ldap_base_dn" json:"ldap_base_dn"`
	// LdapUserFilter is a fmt.Sprintf template with one %s for the escaped
	// username, e.g. "(sAMAccountName=%s)" for AD or "(uid=%s)" for
	// OpenLDAP.
	LdapUserFilter string `gorm:"column:ldap_user_filter" form:"ldap_user_filter" json:"ldap_user_filter"`
	// LdapAttrUsername is the attribute holding the login-username value
	// (matched against LdapUserFilter's %s) - "sAMAccountName" for AD,
	// "uid" for OpenLDAP by default. Only used by the forgot-password
	// flow's LDAP-by-email lookup to auto-provision a not-yet-provisioned
	// LDAP user's local row; ordinary login already knows the username
	// from what was typed into the form.
	LdapAttrUsername    string `gorm:"column:ldap_attr_username" form:"ldap_attr_username" json:"ldap_attr_username"`
	LdapAttrEmail       string `gorm:"column:ldap_attr_email" form:"ldap_attr_email" json:"ldap_attr_email"`
	LdapAttrDisplayName string `gorm:"column:ldap_attr_display_name" form:"ldap_attr_display_name" json:"ldap_attr_display_name"`
	// LdapAttrMobile is the attribute holding the user's phone/mobile
	// number, e.g. "mobile" for both AD and OpenLDAP.
	LdapAttrMobile string `gorm:"column:ldap_attr_mobile" form:"ldap_attr_mobile" json:"ldap_attr_mobile"`
	// LdapAttrGroups is the attribute holding group DNs on the user entry,
	// e.g. "memberOf" for both AD and OpenLDAP (with the memberOf overlay).
	LdapAttrGroups string `gorm:"column:ldap_attr_groups" form:"ldap_attr_groups" json:"ldap_attr_groups"`
	// LdapDefaultRoleID is the fallback Role granted to an LDAP-authenticated
	// user whose group DNs match no LdapRoleMapping row. Nil = no fallback.
	LdapDefaultRoleID *uint `gorm:"column:ldap_default_role_id" form:"ldap_default_role_id" json:"ldap_default_role_id"`
	// LdapAllowPasswordChange opts into writing a password reset back to the
	// directory for LDAP-backed users (self-service "Change password" on
	// the User Settings page, and the forgot-password flow) instead of
	// refusing it outright. The elevated credentials that actually perform
	// the directory write are NOT here - they're config-file-only
	// (util.ConfigRoot.LdapWriteback), deliberately never exposed through
	// this Settings row or its admin API, since they're far more powerful
	// than the read-only LdapBindDN/LdapBindPassword service account above.
	LdapAllowPasswordChange *bool `gorm:"column:ldap_allow_password_change" form:"ldap_allow_password_change" json:"ldap_allow_password_change"`
}

// LibrenmsPendingDelete is one LibreNMS device that sync has quarantined
// rather than deleted outright. DeviceID is LibreNMS's device_id (not a
// factum device id) - the row can outlive the factum device, which is the
// usual reason it was queued. Display is the original LibreNMS display
// name without the "(scheduled for deletion …)" suffix sync stamps while
// the device is quarantined.
type LibrenmsPendingDelete struct {
	FactumModel
	DeviceID    int       `gorm:"uniqueIndex;not null;column:device_id" json:"device_id"`
	Hostname    string    `json:"hostname"`
	Display     string    `json:"display"`
	Reason      string    `json:"reason"`
	ScheduledAt time.Time `json:"scheduled_at"`
	ForceDelete bool      `json:"force_delete"`
}

type Control struct {
	FactumModel
	// sync_name = models.CharField(max_length=255, blank=True, default="")
	// timestamp = models.DateTimeField(default=timezone.now)
}

// Loosely based on RFC5424
type LogEntry struct {
	FactumModel
	// timestamp = models.DateTimeField(default=timezone.now)
	// facility = models.IntegerField(null=True)
	// severity = models.IntegerField(null=True)
	// hostname = models.CharField(max_length=255, blank=True, default="")
	// appname = models.CharField(max_length=48, blank=True, default="")
	// procid = models.CharField(max_length=128, blank=True, default="")
	// msgid = models.TextField(max_length=64, blank=True, default="")
	// msg = models.CharField(max_length=255, blank=True, default="")
}

// HasRole checks if the user has a specific role by name
func (u *User) HasRole(roleName string) bool {
	for _, r := range u.Roles {
		if r.Name == roleName {
			return true
		}
	}
	return false
}
