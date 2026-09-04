---
title: Services
order: 30
---

# Services

**Services** is the commercial/operational inventory: a service ID, a
customer, delivery points, bandwidth, and — for capacity products — a
service type with endpoints on devices.

## List and edit

Filter by customer when you arrived from the customer page. Operators can
open a service to change details, endpoints, and (when the type supports
it) push config to devices.

Rows that were synced from Lime cannot be edited or deleted here. Lime
owns company, delivery points, product, service, comment, service ID, and
agreement status; the next Lime sync would overwrite those fields. You
can still set the Factum service type and endpoints on a Lime-sourced
row.

## Create

**New** opens a two-step wizard:

1. **Product** — a capacity type from [Config](config.md) (ELINE, ELAN,
   …), or Wavelength / Fiber.
2. **Details** — customer, delivery points, and any fields the type's
   schema defines.

Capacity products use categories **CN** (external) or **CI** (internal).
Wavelength uses **VL** / **VI**; fiber uses **LF** / **LI**. If you leave
service ID blank, Factum assigns the next `<type><5-digit>` value (for
example `CN00042`).

Wavelength and fiber have no cfgmgmt service type; they are inventory
rows. Capacity types get endpoints and can be pushed.

## Endpoints and push

On a capacity service, each **endpoint role** defined by the type (A/B
for ELINE, and so on) is a physical port plus VLAN or subinterface. Save
endpoints on the service, then **Push** to render the platform pack for
each device's NOS and apply it.

Push needs write permission and device credentials. Preview the CLI on
the [Config](config.md) page before you rely on a new pack in production.

NetBox L2VPN import fills endpoints on matching Factum services after
device-sync and `factum2-netbox sync`; it does not create service rows.
Lime or the wizard still owns creation.

## Maintenance

When optical modeling is on, **Maintenance** windows compute impact from
wavelength/fiber paths. Packet-only deployments will not see that menu.
