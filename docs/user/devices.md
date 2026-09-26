---
title: Devices
order: 20
---

# Devices

Devices can be synced from NetBox (and, when enabled, BECS via NetBox) or
created locally in Factum. Local devices are independent of NetBox: they
are not pushed there, and a NetBox sync will not overwrite or delete them.

Create a local device from **DCIM → Devices → New**. Manufacturers, device
types, platforms, and interface types are a shared catalog: NetBox sync
fills the same tables (source `netbox`) that you can also create locally
(source `factum`). Pick an existing type from either source. Platform is
optional; its slug should match a driver such as `eos` or `sros` if you
want interface refresh. Check **VM** when the new device is a virtual
machine. Picking a device type marked VM checks that for you; you can
still change it before creating. NetBox sync sets the same flag:
virtual machines are VMs, physical devices are not.

**DCIM → Interface types** is the list used by the Type picker on
interfaces and device-type templates. A full NetBox sync loads NetBox's
port types (1000BASE-T, SFP+, virtual, …). You can add Factum-only types
here; NetBox-synced rows are read-only. Deleting a type that is still
used on a port is refused. Renaming a Factum type's value updates
existing ports and templates.

**DCIM → Interfaces** lists every device interface, whether it came from
NetBox or was created in Factum. Add ports on a local device from that
page or from the device's Interfaces dialog. When creating, a name like
`Ethernet[1-48]` adds Ethernet1 through Ethernet48 in one step
(`Ethernet1/[1-4]` and `Ethernet[1,3,5]` work the same way). NetBox-synced
interfaces are read-only here.

**DCIM → Device types** opens the same tabbed detail dialog as a device
(Overview and Interfaces). Mark a type **VM** when devices of that type
are virtual machines. NetBox sync sets that flag on the type from the
device it imports. VLAN and Oxidized tabs are device-only. The
Interfaces tab holds port templates for that type (synced from NetBox, or
created locally). Range names such as `Ethernet[1-48]` create one template
per expanded name. Creating a local device copies those templates onto it.
Adding a template also adds the port to existing local devices of that type
that do not already have that name.

Local devices can be edited and deleted from the device detail dialog.
Every field on that form is writable for a Factum-created device, including
addresses, location, comments, enabled, monitoring flags, and optical kind.
NetBox-synced catalog rows and devices are read-only here.

## Device list

**Devices** in the sidebar lists name, site, role, status, manufacturer,
model, and primary IPv4. Open a row to:

- Inspect interfaces and addresses. Add a Factum IP on any interface
  (NetBox-synced addresses stay read-only). The same inventory is under
  **IPAM → IP addresses** when IPAM is enabled.
- Refresh interfaces from the device (needs write permission): descriptions
  are reloaded, and interfaces that no longer exist on the device are
  removed from Factum and NetBox (ports defined by the device type's
  template are kept)
- Edit VLANs on an interface
- Attach or open a [service](services.md)
- Open the Oxidized backup for that node, when Oxidized is enabled

Device login uses the credentials under **Admin → Device sync** (a
per-device override, or the `default` row) — the same store as service
push and device-sync. `factum2-driver-cli` still takes `--username` /
`--password`.

## Network map

**Network map** draws sites and links from Factum's topology (cables
synced from NetBox, plus cables created in Factum). Sites without GPS
are omitted.

**Infrastructure → Racks**, **Floor plans**, and **Connections** are the room
and rack drawings. On **Connections → Between devices** you can create,
change, and remove cables. When both ports came from NetBox, the cable is
stored in NetBox as well. See [Datacenter](datacenter.md).

**Organization → Sites** (when Organization is enabled) is the hierarchical
site tree. NetBox regions, sites, and locations all import as sites, nested
the same way they nest in NetBox. You can add Factum sites under any node;
those stay in Factum and are not pushed to NetBox.

**Assign locations** pins a device on the map and writes the coordinates
back to NetBox. A site is optional: leave it blank to set GPS on that
device only (typical for a lone chassis). Fill in a site when several
devices share the same place — Factum creates or updates the NetBox site
and assigns the device to it.

## Oxidized

When Oxidized is enabled under Admin → Destinations → Oxidized, an
**Oxidized** menu entry lists nodes, last backup time, and status. Open a
node to view the current config, older versions, and diffs.

Deleted devices are not in that list: `/oxidized` is the current source
list. Version and diff lookups require the node to still be present.

The destination file Oxidized reads (`router.db`) is written by the
Oxidized [job](jobs.md), not by this page. This page talks to oxidized-web
using **Oxidized API URL** on the Destinations tab — that URL must be
reachable from the Factum primary.

## Optical devices

If **Optical / WDM modeling** is enabled, device detail also exposes
optical ports, cross-connects, and impact. Wavelength and dark-fiber
services get a path on the service itself; packet-only deployments can
leave optical off.
