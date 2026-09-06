---
title: Services
order: 30
---

# Services

**Services** is the commercial/operational inventory: a service ID, a
customer, delivery points, bandwidth, and — for capacity products — a
service type with endpoints on devices.

Capacity CN/CI rows also appear as **service objects** on the
[Config](config.md) tree (under `_services` after migrate, or wherever you
create/attach them). The tree node is the same row, not a second
inventory.

## List and edit

Filter by customer when you arrived from the customer page. Operators can
open a service to change details, endpoints, and (when the type supports
it) push config to devices. The Config tree inspector edits type and
endpoints the same way.

Rows that were synced from Lime cannot be edited or deleted here. Lime
owns company, delivery points, product, service, comment, service ID, and
agreement status; the next Lime sync would overwrite those fields. You
can still set the Factum service type and endpoints on a Lime-sourced
row, and attach it to the config tree.

## Create

**New** opens a two-step wizard:

1. **Product** — a capacity type from [Config](config.md) (ELINE, ELAN,
   …), or Wavelength / Fiber.
2. **Details** — customer, delivery points, and any fields the type's
   schema defines.

You can also **create from the Config tree** (right-click a folder):
category, type, and company. That creates the inventory row and the
canonical tree node together, with zero endpoints — complete the set in
the inspector. Lime create is not offered from the tree.

Capacity products use categories **CN** (external) or **CI** (internal).
Wavelength uses **VL** / **VI**; fiber uses **LF** / **LI**. If you leave
service ID blank, Factum assigns the next `<type><5-digit>` value (for
example `CN00042`).

Wavelength and fiber have no cfgmgmt service type; they are inventory
rows and are not offered in the config tree. Capacity types get endpoints
and can be pushed.

## Endpoints and push

On a capacity service, each **endpoint role** defined by the type (A/B
for ELINE, and so on) is a physical port plus VLAN or subinterface. Save
endpoints on the service (dialog or Config inspector), then **Push** to
render the CLI object for each device's NOS and apply it.

**Show configuration** on the edit dialog renders that CLI for the
devices and interfaces currently selected (including unsaved picks). It
does not contact the devices. Push still needs write permission and
device credentials. Preview from the [Config](config.md) page before you
rely on a new CLI object in production.

NetBox L2VPN import fills endpoints on matching Factum services after
device-sync and `factum2-netbox sync`; it does not create service rows.
Lime or the wizard (or tree create) still owns creation.

## Maintenance

When optical modeling is on, **Maintenance** windows compute impact from
wavelength/fiber paths. Packet-only deployments will not see that menu.
