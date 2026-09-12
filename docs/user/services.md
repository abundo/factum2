---
title: Services
order: 30
---

# Services

**Services** is the commercial/operational inventory: a service ID, a
customer, delivery points, bandwidth, and — when realized — a catalog
definition with endpoints on devices.

Capacity CN/CI rows also appear as **service objects** on the
[Config](config.md) tree (under `_services` after migrate, or wherever you
create/attach them) once they have a **definition**. The tree node is the
same row, not a second inventory. Factum ships **no** built-in ELINE/ELAN
products — definitions live in Config → Catalog → Service types.

## List and edit

Filter by customer when you arrived from the customer page. Operators can
open a service to change details, endpoints, and (when the type supports
it) push config to devices. The Config tree inspector edits type and
endpoints the same way.

Rows that were synced from Lime cannot be edited or deleted here. Lime
owns company, delivery points, product, service, comment, service ID, and
agreement status; the next Lime sync would overwrite those fields. You
can still **Realize** a Lime-sourced row (set a definition and endpoints)
or **Unrealize** it (drop device/NetBox state, keep the CN). Lime delete
is refused; unrealize is allowed. A Lime sync also **removes**
Factum rows for deliveries that Lime no longer returns (and, on a full
sync with no company filter, companies Lime no longer returns). Inactive
Lime persons stay as contact rows but are unlinked so they are not
mailed.

## Create

**New** opens a two-step wizard:

1. **Product** — Capacity (CN/CI), Wavelength, or Fiber. Capacity here
   is a commercial row; it does **not** require a cfgmgmt definition.
2. **Details** — customer, delivery points, product text.

You can also **create from the Config tree** (right-click a folder): pick
a definition, then fill that definition’s form. That creates the
inventory row and the canonical tree node together. Lime create is not
offered from the tree. Use **Realize** on an existing CN to attach a
definition without a second row.

Capacity products use categories **CN** (external) or **CI** (internal).
Wavelength uses **VL** / **VI**; fiber uses **LF** / **LI**. If you leave
service ID blank, Factum assigns the next `<type><5-digit>` value (for
example `CN00042`).

Wavelength and fiber have no cfgmgmt definition; they are inventory rows
and are not offered in the config tree. Realized capacity services get
endpoints and can be pushed.

## Endpoints and push

On a realized capacity service, each **interface** is a port plus the
fields the definition asks for (VLAN, ServiceID, …). There are no named
A/B roles. Save endpoints on the service (dialog or Config inspector),
then **Push** to render the CLI object for each device's NOS and apply
it. Drag a tree ref to another port to rebind; Factum removes the old
CLI when a previous push snapshot exists.

**Show configuration** on the edit dialog renders that CLI for the
devices and interfaces currently selected (including unsaved picks). It
does not contact the devices. Push still needs write permission. Devices
are logged into with the credentials under **Admin → Device sync** (a
per-device override, or the `default` row). On Arista EOS the configure-session description records who pushed; on
Nokia SR OS and Cisco IOS-XR it is the commit comment, for example
`factum push CN00042 by Alice Andersson`. Preview from the
[Config](config.md) page before you rely on a new CLI object in
production.

NetBox L2VPN import fills endpoints on matching Factum services after
device-sync and `factum2-netbox sync`; it does not create service rows.
Lime or the wizard (or tree create) still owns creation.

## Maintenance

When optical modeling is on, **Maintenance** windows compute impact from
wavelength/fiber paths. Packet-only deployments will not see that menu.
