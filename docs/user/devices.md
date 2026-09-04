---
title: Devices
order: 20
---

# Devices

Devices are synced from NetBox (and, when enabled, BECS via NetBox). You
do not create chassis in Factum; you work with what the last source sync
brought in.

## Device list

**Devices** in the sidebar lists name, site, role, status, manufacturer,
model, and primary IPv4. Open a row to:

- Inspect interfaces and addresses
- Refresh interface state from the device (needs write permission and
  device credentials)
- Edit VLANs on an interface
- Attach or open a [service](services.md)
- Open the Oxidized backup for that node, when Oxidized is enabled

Credentials used to talk to a device are prompted in the GUI and can be
remembered for the browser session. They are not stored in Factum.

## Network map

**Network map** draws sites and links from Factum's topology (cables
synced from NetBox). Use it to see how devices connect, not to edit
cabling — change cables in NetBox and sync.

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
