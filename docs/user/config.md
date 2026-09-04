---
title: Config
order: 40
---

# Config

**Config** is where capacity service types and device CLI templates live.
It is not the YAML file on disk (`/etc/factum2/factum2.yaml`); that file
only has database, web, worker, and similar process settings.

## Tabs

| Tab | What it is |
| --- | --- |
| Tree | Nested **scopes** (regions, sites, devices, …) with variable assignments |
| Matrix | One variable across scopes |
| Variables | Typed knobs used in templates |
| Service types | ELINE, ELAN, … — roles, schema, NetBox mapping |
| Platform packs | Per-NOS template + macros for a service type |
| Macros / Templates | Reusable CLI fragments and the templates packs point at |

Operators with write permission can add types and packs here. Adding a
capacity product is a database change, not a new Go package.

## How a push uses this

1. The [service](services.md) has a **service type** and endpoints.
2. Each endpoint's device has a platform. Factum picks the **platform
   pack** for that type + NOS.
3. The pack's template is rendered with the service, endpoints, macros,
   and resolved scope variables.
4. The driver applies the CLI (or NETCONF equivalent) to the device.

Use **Render** / preview on this page against a device before pushing a
new pack from the service dialog.

## Scopes

Assignments inherit down the tree: a variable set on a site applies to
devices under it unless a child scope overrides it. Attach a device to a
scope from the tree when the device should pick up that site's values.
