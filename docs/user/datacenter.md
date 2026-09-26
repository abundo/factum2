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

**Infrastructure → Floor plans** are drawings of a site. Create a plan or
rename one from the list, then Edit to drag racks onto the room, snap to
the grid, and rotate in 90° steps.
Save/Cancel keep a draft; undo is in-session. A rejected or stale save
keeps your draft so you can reload or reapply. Dragging a rack on the plan
does not change which site it belongs to.

From a rack on the plan, open its elevation.

## Connections

**Infrastructure → Connections** has two views of **direct
interface-to-interface cables**. It is not end-to-end tracing through
patch panels, power, or services.

**Between devices** is the editor. It opens on one device column and lists
that device's physical interfaces (virtual ports and LAGs are hidden).
**Add device** appends another column. Each device sits in its own box.
Cables between neighbouring columns are drawn as lines. Point at a port
circle to see the device and interface it connects to. Click a circle that
already has a cable to open that device in the next column. Drag from a
free port circle to a port on the neighbouring device to create a cable, or
click one free port then the other. The list scrolls if you drag near the
top or bottom. Click a cable line to change the label or endpoints, or use
the trash control to remove the cable. If both ports already exist in
NetBox, Factum creates (and later changes or removes) the cable there too.
Factum-only devices keep the cable local. A port that is already cabled
cannot take a second cable.

**Graph** is the neighbourhood map. Filter by site or rack. Cables that
leave the selected scope appear as labelled stubs. Rearranging nodes only
stores your layout; it never creates or edits cables.

## Device types

Local device types can set height in U, whether the chassis is full
depth, and whether the type is a VM. Height and depth drive occupancy.
Leave height blank when you do not know it — Factum will not assume 1U.
A VM device is not placed in a rack.
