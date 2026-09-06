---
title: Config
order: 40
---

# Config

**Config** is the tree where capacity services, parameter objects, and
device CLI live. It is not the YAML file on disk
(`/etc/factum2/factum2.yaml`); that file only has database, web, worker,
and similar process settings.

## Tree (primary)

The page is a **tree** with an inspector and a **Preview** dock. Right-click
to add folders, sites, locations, parameter objects, CLI objects, and
services, or to attach a device. Drag to move a node (the API rejects
illegal parents).

| Kind | What it is |
| --- | --- |
| Folder / site / location | Organizational. Site and location are folder variants. |
| Device | An existing DCIM device **attached** here. Detach does not delete inventory. Interfaces are managed children. |
| Parameter object | Named assignments (MTU, AS number, …). They apply to the parent and its descendants. |
| CLI object | Per-NOS command templates (features with add/remove). Baseline objects sit on the ancestor chain; service translation lives under `_catalog/cli`. |
| Service | A capacity `Service` row (create or attach). Endpoints are edited in the inspector. Virtual refs appear under involved devices. |

Reserved folders (`global`, `_catalog`, `_services`) are system folders.
Put service-translation CLI under `_catalog/cli/<type>/<platform>`. Put
global baseline CLI as a **direct child of `global`**, not under
`_catalog`.

**Matrix** is an audit view: one variable across interfaces under the
selected folder or device (or the nearest such ancestor).

## Catalog

**Catalog** (button on the Config page) holds definitions that are not
tree nodes:

| Panel | What it is |
| --- | --- |
| Variables | Typed knobs. Assign values on a parameter object. |
| Service types | ELINE, ELAN, … — roles, schema, NetBox mapping |
| Macros | Reusable CLI fragments (`include "name"`) |

Operators with write permission add variable defs, types, and macros
here. CLI objects are added from the tree (translation under
`_catalog/cli/<type>/<platform>`). Adding a capacity product is a
database change, not a new Go package.

## How a push uses this

1. The [service](services.md) has a **service type** and endpoints
   (created or attached in the tree, or from the Services page).
2. Each endpoint's device has a platform. Factum picks the **CLI object**
   for that type + NOS (under `_catalog/cli`).
3. Feature blobs are rendered with the service, endpoints, macros, and
   resolved parameter values.
4. The driver applies the CLI to the device.

Use **Preview** on this page against a device before pushing a new CLI
object from the service dialog. Push is still service-only; baseline CLI
is preview-only.

## Inheritance

A parameter object applies to its parent and that parent's descendants: a
variable set on a site-level object applies to devices under that site
unless a closer object overrides it. Attach a device under the site so it
picks up those values.
