# Changelog

The major changes in each release. A release's notes on GitHub are its section
here (`dev/release-notes.sh`); the full list is `git log <previous tag>..<tag>`.

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
