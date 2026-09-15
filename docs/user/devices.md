---
title: Devices
order: 20
---

# Devices

Devices can be synced from NetBox (and, when enabled, BECS via NetBox) or
created locally in Factum. Local devices are independent of NetBox: they
are not pushed there, and a NetBox sync will not overwrite or delete them.

Create a local device from **DCIM → Devices → New**. Manufacturers, device
types, and platforms are a shared catalog: NetBox sync fills the same
tables (source `netbox`) that you can also create locally (source
`factum`). Pick an existing type from either source. Platform is optional;
its slug should match a driver such as `eos` or `sros` if you want
interface refresh.

Local devices can be edited and deleted from the device detail dialog.
NetBox-synced catalog rows and devices are read-only here.

## Device list

**Devices** in the sidebar lists name, site, role, status, manufacturer,
model, and primary IPv4. Open a row to:

- Inspect interfaces and addresses
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
synced from NetBox). Use it to see how devices connect, not to edit
cabling — change cables in NetBox and sync. Sites without GPS are omitted.

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

When Oxidized is enabled under Admin → Settings → Destinations, an
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
