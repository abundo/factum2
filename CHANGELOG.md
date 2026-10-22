# Changelog

The major changes in each release. A release's notes on GitHub are its section
here (`dev/release-notes.sh`); the full list is `git log <previous tag>..<tag>`.

## Unreleased

### New

- The Software page expands and collapses a folder from its **+** / **−**
  control. Right-click a folder to create, rename, delete, or upload, and
  a file to rename, delete, or download. Delete asks for confirmation.

### Changed

- Services → DNS is one page. Zones, DNS templates, SOA templates, and
  DNSSEC policies are tabs. The old paths redirect to the matching tab.
- `factum2-dns` writes SOA templates and DNS templates into the zone
  include, next to the DNSSEC policies. dnsmgr2 uses those templates
  when it writes the BIND zone file. A template in the include replaces
  a template of the same name in `dnsmgr2.yaml`. Apex NS records are no
  longer copied into the JSON records file.
- NetBox delta sync always runs the `netbox` worker command with
  `--delta`. A `worker.commands` entry named `netbox-delta` is ignored.
- BECS sync names an interface that sits under another interface the way
  iBOS shows it (`gi3/vid511` for `vid511` under `gigabitethernet3`) and
  sets that interface's NetBox parent to `gigabitethernet3`. A row the
  old sync stored as `vid511` is renamed when it still has the same BECS
  id.
- `factum2-worker start` runs with an empty `worker.commands` map, so a
  host can connect to the primary before any command is configured.
- Admin → Settings → Factum → Software stores a `worker.paths` key
  (`storage_root`) for the repository directory. The path is only in the
  storage host's `factum2-worker.yaml`. A blank field uses `storage_root`.
- Releases ship a linux/amd64 `tar.gz` only. There is no linux/arm64
  build and no `.deb`.

### Fixed

- NetBox rack sync reads `width` when NetBox 4 returns it as
  `{"value": 19, "label": "19 inches"}`. The cabinet size still comes
  from `outer_width` and `outer_depth` (millimetres, or inches when
  `outer_unit` says so). A rack with no outer size stays 600 mm by
  1000 mm.
- BECS sync keeps every IPv4 address on an interface. A second
  `resource-inet` on the same port is added beside the one already in
  NetBox, instead of replacing it.
- NetBox sync can rename two interfaces that swap names. The temporary
  name no longer starts with a NUL byte, which PostgreSQL rejects
  (`invalid byte sequence for encoding "UTF8": 0x00`).
- `factum2-netbox check --update` assigns the `destination` custom field
  to interfaces as well as devices and virtual machines. A LibreNMS id
  stored on `dcim.interface` can then be copied into `destination`.
- Opening a DNS zone's records and leaving without edits no longer asks
  to discard unsaved changes. The blank row at the bottom of the grid
  stays local until you type in it.

### Upgrading

- Delete any `netbox-delta` entry from `worker.commands`. Delta sync uses
  the `netbox` command and `allow_args: ["--delta"]`.
- Install applies goose migration `00037`. It rewrites `storage_root`
  from a filesystem path to the key `storage_root`. On the host that runs
  `factum2-storage`, set `worker.paths.storage_root` to the directory that
  was in the GUI (often `/var/lib/factum2/storage`) before migrating. The
  migration does not keep a custom path.

## v1.3.0

### New

- `factum2-icinga poll` also checks ELINE services on monitored
  devices: IOS-XR xconnects via CISCO-IETF-PW-MIB, Nokia epipes via
  TIMETRA-SERV-MIB `svcOperStatus`, and any platform that implements
  PW-STD-MIB (RFC 5601). Arista EOS uses eAPI `show patch panel`
  with the EOS eAPI account on the Icinga destination. Sync writes one passive `eline <id>`
  service per applied endpoint. Both ends are checked. Only the earlier host notifies while
  that end is Warning or Critical, so one failure does not page twice. Admin-down stays OK.
- Icinga can monitor interfaces. Admin → Destinations → Icinga has
  Monitor interfaces, SNMP communities, and an interfaces file.
  Sync writes one passive service per interface on devices with
  Interface alarms set. `factum2-icinga poll --interval 1m` reads
  status and counters with one SNMPv2c session per device and submits
  passive results, including at about 40000 interfaces. Set
  `icinga_command_pipe` on the Icinga worker so those results go to
  the external command pipe.
- Lime sync emails a report when customers, services, or contacts were
  added, updated, or removed. Set the recipients under Admin → Sources →
  Lime. A sync with no changes does not send mail.
- `install.py --archive FILE.tar.gz` installs a local release archive.
  The host does not need a git checkout or a compiler. An interim stamp
  such as `0.6.1-rc1` is kept.

### Fixed

- **Packet capture:** a release build of `factum2-web` includes Wiregasm.
  Packet capture works from the installed binary. Development builds still
  use `make wiregasm`.

### Changed

- Icinga passive results sent through the API reuse one HTTP/1.1
  connection per poll worker. Each interface is still its own
  `process-check-result` request. The command pipe remains the path
  while Icinga provides it.
- Admin → Destinations file fields store a `worker.paths` key, such as
  `icinga_hosts_file`. The filesystem path is only in each worker's
  `factum2-worker.yaml`. A field may name any pinned key. A blank field
  uses the key for that field.
- Job overview lists Device sync under Sources. Sync all runs it with the
  other sources, before destinations.
- LibreNMS sync creates a device with its hostname set to the primary
  IPv4 and its display name set to the Factum name plus
  `Settings.DefaultDomain`. Renames and field updates use the LibreNMS
  device id stored in the NetBox `destination` custom field.
- BECS stores a device, interface, and IP origin in the NetBox `source`
  custom field (`becs:3641645`). `source` is also assigned to virtual
  machines. `becs_oid` is no longer created. Until `becs_oid` is copied,
  sync still matches a device that only has the old integer.
- NetBox sync mirrors regions, sites, and locations into Provisioning →
  Config and places each device under its site. Organization sites still
  receive the same rows for the network map and racks.
- NetBox delta sync runs as `factum2-netbox sync --delta` on the existing
  `netbox` worker command. Add `allow_args: ["--delta"]` to that entry.
  A separate `netbox-delta` command is no longer required. A worker that
  still advertises one keeps using `sync-delta`.

### Upgrading

- Install applies goose migration `00033`. It rewrites destination file
  settings from filesystem paths to `worker.paths` keys (`icinga_hosts_file`,
  `dns_dest_file`, `lego_yaml`, and the rest). The paths stay in each
  worker's `worker.paths`. Stop `factum2-web` before migrating by hand.
  After it, a destination sync refuses a settings value that is still a
  path.
- Run `factum2-netbox check --update` so `source` is assigned to devices,
  interfaces, IP addresses, and virtual machines. Copy existing `becs_oid`
  values with `examples/netbox_migrate_source_field.py` (source plus
  source_id, then becs_oid). Copy old LibreNMS ids with
  `examples/netbox_migrate_destination_field.py`. Both use the NetBox REST
  API and read the URL and token from the `settings` row, using `db:` in
  `/etc/factum2/factum2.yaml`. Then delete the old custom fields in the
  NetBox UI.
- Install applies goose migration `00032` (`config_scopes.netbox_kind`
  and `netbox_id`). Stop `factum2-web` before migrating by hand.

## v1.2.0

### New

- **RADIUS:** switches and routers log in through a worker. PAP checks the
  directory bind. MikroTik MS-CHAPv2 checks Active Directory with the
  computer account on the Service tab. A login also needs an LDAP group
  mapped to the device's NetBox role. Accept attributes are edited under
  Admin → Services → RADIUS.
- **Packet capture:** Tools → Packet capture mirrors an Arista Ethernet,
  Port-Channel, or Recirc-Channel and shows the packets in the browser.
- **Running configuration:** DCIM → Configuration edits Arista EOS and
  Cisco IOS-XR from CLI text, and Nokia SR OS MD from the device's
  configuration JSON. A click selects a context. A double click opens or
  closes that context's children.
- **NetBox delta sync:** `factum2-netbox sync-delta` (job `netbox-delta`)
  refetches what changed since the last full sync. Webhook bursts wait one
  quiet period, then run that delta. Device, VM, cable, site, region, and
  location deletes apply immediately.
- **Config bundles:** Config → Export / Import moves a service definition
  with its translation CLI, macros, variables, and parameter objects.
- **DNS:** a template nameserver can carry an address, written as in-zone
  A/AAAA glue. Zone sync writes DNSSEC policies for dnsmgr2 v1.2.2.
- **Devices:** a VM flag on device types and devices. The customer dialog
  lists linked contacts.
- A schedule can name several jobs. Device sync can run several workers at
  once, and ELINE, ELAN, and L3VPN sync can each be turned off.
- A security policy for vulnerability reports (`SECURITY.md`).

### Changed

- NetBox origin is one `source` custom field (`factum:42`). Downstream ids
  are one `destination` field (`librenms:42 zabbix:7`). Each integration
  updates only its own id. `librenms_id` is no longer created.
- Remote worker upgrades SSH as `factum` and use passwordless sudo. Root
  SSH is refused when a remote worker is in the run.
- The network map uses OpenFreeMap Liberty. Lines are colored by fiber,
  wavelength, and capacity.
- The cable editor spans the device columns you pick, and each device
  appears in one column.
- Provisioning follows Organization. DNS and Certificates sit under
  Services. Software sits under Tools, next to Packet capture. RADIUS sits
  under Admin → Services.
- Tables search as you type. Delete lives in the detail view and asks
  before it removes the named row. Forms stay wide, and leaving a page
  with unsaved edits asks first.
- Oxidized versions show age in days, or years and days.
- Icinga alert mail names the Factum customers and services on the host.
- Reset links, worker command arguments, and hub config fetches stay
  inside their trust boundary.

### Fixed

- The installer extracts a release archive on Python 3.14 when
  `tarfile.data_filter` is missing.
- A NetBox cable stays when its webhook races the save.
- Changing an address VRF updates that row, so the device's primary IP
  stays.
- The log panel no longer jumps when its backlog arrives.

### Upgrading

- Install applies goose migrations `00023` through `00031`. Stop
  `factum2-web` before migrating by hand.
- A worker upgrade needs a `factum` user, the primary's deploy key in that
  user's `authorized_keys`, and `examples/factum2-install.sudoers`.
- Copy old NetBox origin fields with `examples/netbox_migrate_source_field.py`
  and old LibreNMS ids with `examples/netbox_migrate_destination_field.py`
  before the next sync. `factum2-netbox check --update` creates `source`
  and `destination`.
- Add `netbox-delta` next to `netbox` in `worker.commands`
  (`sync-delta --job`) to schedule the shorter sync. A full `sync` records
  the cursor. With no cursor, delta runs a full sync.
- DNSSEC policy lines need dnsmgr2 v1.2.2 on the DNS host.
- The primary and its workers must be this same build. The hub handshake
  rejects a mismatch.

## v1.1.2

### Changed

- DNS zone export uses dnsmgr2 v1.2.1 include files.
- CLI add and remove commands are edited in the template modal.
- Icinga destination file paths sit before their templates.

### Fixed

- Updating an Icinga worker adds `nagios` to the `factum` group.

## v1.1.1

### New

- **Infrastructure:** racks, floor plans, and connections, including a cable
  editor between devices. Floor plans can be renamed from the list.
- **Header branding:** configurable logo and text beside the Factum wordmark.
- **DHCP:** a lease picker for zone-record MAC addresses. The DNS lab
  container runs ISC Kea DHCPv4.
- **Interface types:** a catalog synced from NetBox; lists show the type name.
- **Software images:** a repository served by `factum2-storage`.
- **Templates:** operator templates render with Jet.

### Changed

- Admin splits sources and destinations into submenus.
- Icinga writes an HTTPS certificate check for each certificate name, and
  destination file fields live in the template editors.
- The interface IP picker lists prefixes in the interface VRF. Extra VRF names
  are unique across namespaces, and NetBox VRFs sync into IPAM with route
  distinguishers and route targets.
- A device's primary IPv4 or IPv6 can be set from a management IP checkbox.
- "Technical service" is called a service instance. Service definition fields
  can have default values.
- Devices that came from NetBox and disappear from a full sync are deleted.
- `install.py --compose` is limited to the primary; destination containers
  restart with `--worker`.

### Fixed

- SSH CLI sessions are reused per device (`factum2-driver` serves the pool).
- NetBox webhook device sync shows in the GUI log again. Bursts for one
  device wait 3 seconds before syncing.
- A single-device NetBox sync skips L2VPN import.
- Hub version matching is skipped when `APP_ENV=development`.

## v1.1.0

### New

- **Local devices:** Factum-local devices, a shared DCIM catalog, device-type
  templates, and interfaces on the device page. A platform on a device type
  is copied onto new devices.
- **Sites:** hierarchical Organization sites. A device's site is a site
  record, and one can be created from the site selector. Sites and devices
  can be given map coordinates.
- **IPAM:** Factum-created addresses under IPAM and on device interfaces, with
  a VRF, prefix, and host picker. An address is linked to the prefix it
  belongs to.
- **Certificates:** ACME via lego (DNS-01 / RFC 2136).
- **Service definitions:** typed fields, a form editor, generic NetBox
  reconcile, and CLI rendered from `.Interfaces` and `.Others`.

### Changed

- The DNS zone editor is split into info, SOA, and records, with Save+sync.
  NetBox IP `dns_name` values sync into DNS records.
- Service push uses device-sync credentials.
- Home is a direct dashboard link. The device detail tab bar stretches to
  the full width.

## v1.0.9

### Fixed

- Adopted databases get goose migration `00003` for `settings.dns_db_file`.

## v1.0.8

### Fixed

- Adopted databases get goose migration `00002` for the DNS zone editor.

## v1.0.7

### Fixed

- The installer treats a GoReleaser `1.0.6` version stamp and git tag
  `v1.0.6` as the same release.

## v1.0.6

### New

- DNS zones and records in the GUI. Apply writes JSON records and DHCP config
  for dnsmgr2 v1.2.0.
- IPAM prefixes edit in a split details pane, sorted by longest prefix.
- Cisco SMB and Huawei VRP SSH CLI drivers.

### Changed

- Schema changes are goose SQL files. DNS apply goes through the dnsmgr2
  library.
- DHCP settings move under Destinations.
- The primary and a worker must run the same version on the hub handshake.
  An unstamped `go run` build skips that check.
- `install.py --source` updates remote workers and checks their version.
- Sidebar headings collapse, and the open group follows the current route.

## v1.0.5

### New

- Config tree resize and rename, with CLI preview in a modal. Packs, macros,
  and templates are edited as Go templates. Parameters, CLI, and services are
  tree objects.
- The network map can assign devices to NetBox sites, pin a device that has
  no site, and show a larger connection popup.
- A job's detail dialog keeps polling while the job runs. Job status refresh
  is a switch.
- An About page with version, license, and GitHub links.
- Operator documentation in the GUI and on GitHub Pages, including the
  release software bill of materials.
- A compose lab for NetBox, LibreNMS, and the other sync targets.
- An LDAP directory picker. Bind is anonymous when the service account DN is
  blank.
- `factum2-becs sync --dry-run` prints the planned NetBox writes.

### Changed

- The Icinga default notification is a Go template.
- Factum contacts sync to NetBox.
- Maintenance windows can attach several fibers, wavelengths, and devices.

## v1.0.4

### New

- The worker hub is served over WSS. The installer issues its certificates.
- A housekeeping job trims old job history.
- Open ROADM inventory: a read-only NETCONF driver, stored on ports and
  cross-connects.
- The network map can switch between light and dark basemaps.

### Changed

- Worker nodes and device sync live under Settings.
- Oxidized stays hidden unless that destination is enabled.
- `web.jwtsecret` is required in every environment. Outside development the
  auth cookie is `Secure`.
- Lime-synced contacts default to `notify=false`.

### Fixed

- Installer self-update uses the checksum-verified release tarball.
- The database password stays off remote `ps` arguments, and tar extraction
  blocks path traversal on Python older than 3.12.
- Systemd start-rate limiting is disabled for `factum2-web` and
  `factum2-worker`.

## v1.0.3

### New

- Binaries are named `factum2-*`.
- Prometheus writes snmp_exporter `file_sd` targets.
- Oxidized has a device browser with config diffs. `router.db` uses FQDNs.
- ELINE is provisioned through cfgmgmt templates. Bandwidth comes from the
  service type schema.

### Changed

- The installer runs the `install.py` from the selected GitHub release.
- Organization is optional in the GUI. Email settings live under Factum.
- LibreNMS deletions are labeled Device deletions.
- NetBox owns the LLDP cable label.

## v1.0.2

### New

- Config scopes, service types, and platform packs. A generic service pushes
  rendered CLI through a config session.
- Schema migrations run from a dedicated migrate command.
- LDAP can use a secondary server.
- The build version shows on the login page and in the sidebar.
- The log panel docks in the layout.

### Changed

- Remote CLIs reach the primary through the hub socket.
- LibreNMS sync stops when a device hostname is not an IP address.

## v1.0.1

### New

- `install.py` installs a GitHub release or this source tree, and can update
  its own copy from GitHub. It replaces `install_prod.sh`.
- A production quickstart installs from GitHub releases.

### Changed

- The `factum2-gui` systemd unit is `factum2-web`.
- The installer asks before overwriting a systemd unit that was modified.
- The Services button shows only for customers that have services.
- Lime agreement status shows on the service list and on the details.

## v1.0.0

### New

- First tagged release. Factum tracks devices, customers, and services, syncs
  NetBox and Lime into its database, and syncs DNS, Icinga, and LibreNMS out.
  The web GUI is included.
