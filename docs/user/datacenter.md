---
title: Datacenter
order: 25
---

# Datacenter

Factum can draw racks, rooms, and direct cables. Inventory that came from
NetBox stays owned by NetBox: you can look at imported racks and mounts,
but you cannot move them here. Factum-created racks and devices can be
placed locally. Floor-plan drawings and graph node positions are always
local — they survive a NetBox sync.

## Racks

**Infrastructure → Racks** lists every rack. Open a row for the elevation: numbered
rails, front and rear faces, and occupancy. Full-depth devices occupy both
faces. Devices whose type has no height show as unknown rather than as 1U.

Place a local device with the form (offset from the bottom of the rack, in
U). Unmount removes the mount only; the device stays in inventory. Imported
placements are visible and read-only — change them in NetBox, then sync.

Zero-U accessories sit in a separate list. Virtual machines cannot be
racked.

## Floor plans

**Infrastructure → Floor plans** are drawings of a site. Create a plan, then Edit to
drag racks onto the room, snap to the grid, and rotate in 90° steps.
Save/Cancel keep a draft; undo is in-session. A rejected or stale save
keeps your draft so you can reload or reapply. Dragging a rack on the plan
does not change which site it belongs to.

From a rack on the plan, open its elevation.

## Connections

**Infrastructure → Connections** shows **direct interface-to-interface cables**
synced from NetBox. It is not end-to-end tracing through patch panels,
power, or services. Filter by site or rack. Cables that leave the selected
scope appear as labelled stubs. Rearranging nodes only stores your layout;
it never creates or edits cables.

## Device types

Local device types can set height in U and whether the chassis is full
depth. Those values drive occupancy. Leave height blank when you do not
know it — Factum will not assume 1U.
