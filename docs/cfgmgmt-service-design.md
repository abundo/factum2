# Designing a Factum service type

How to add a **capacity service** (CN/CI) that Factum can validate, preview,
and push to devices. You design the type, the CLI that implements it, and
the service instance **in the Config GUI tree**. You do **not** add a new
Go package per service. The machinery lives in `internal/cfgmgmt`.

ELINE uses the same **generic endpoints** + **CLI objects** path as every
other capacity type. It still has NetBox L2VPN reconcile on save (and
reverse-import from `factum2-netbox sync`). Do not add
`Service.EndpointA/B*` columns for a new type.

Related code:

| Piece | Where |
| ----- | ----- |
| Models | `models/config.go` (`ServiceType`, `ConfigScope` kinds, `ConfigCLIFeature`, `ServiceEndpoint`) |
| Engine | `internal/cfgmgmt/` |
| HTTP | `web/handler_config.go`, `web/handler_service.go` |
| GUI | Config page (tree + inspector + catalog), service edit dialog (push) |
| Seeded ELINE CLI | `internal/drivers/templates/*.tmpl` (seed source for `_catalog/cli/ELINE/<platform>`) |

---

## What you are designing

Four pieces, all in the Config GUI:

1. **Service type** (catalog) — vendor-agnostic class: name, schema, endpoint
   roles, optional `sync_source` / `netbox_type`.
2. **CLI objects** under `global / _catalog / cli / <Type> / <platform>` —
   per-NOS translation of that intent into command lists.
3. **Parameter objects** — named groups of variable assignments that inherit
   down the tree (MTU, AS number, NTP, …).
4. **Service object** in the tree — a view onto a `models.Service` row
   (create or attach) plus endpoints.

A **service instance** is still a `models.Service` row (tree create, wizard,
Lime sync, or API) with that type set. Endpoints are a separate table
(`service_endpoints`), replaced as a set via `PUT /api/service/:id/endpoints`.
The tree node is not a second inventory.

Wavelength (VL/VI) and dark fiber (LF/LI) are **not** cfgmgmt services.
They have no `ServiceType` and no device CLI. Do not invent CLI objects
for them.

```
  ServiceType catalog (name, schema, endpoint_roles)
       │
       ├─ CLI object  (_catalog/cli/<Type>/<platform>)
       │     features: add / remove command blobs
       │
       ├─ Parameter object  (assignments → .Vars)
       │
       └─ Service object in the tree  (CN/CI + service_type)
              └─ ServiceEndpoint[]  (role, device, interface, fields)
                     │
                     ▼
              Render  →  Preview (docked on the Config tree)
              Push    →  CLI session on each endpoint device
```

Platform packs and baseline templates were the previous composition units.
They have been replaced by CLI objects; the old pack/template HTTP routes
return 410. The **operator path is the tree**: CLI objects and parameter
objects.

---

## Decide this before you write JSON

Work through these in order. Changing roles or field names after instances
exist is a data migration, not a rename.

1. **Topology.** Point-to-point (exactly two sides), hub-and-spoke, or
   unlimited multipoint?
2. **Roles.** Named sides (`a`/`b`, `hub`/`spoke`) vs a single `endpoint`
   role. Use distinct roles when templates or operators must treat sides
   differently; use one role when every termination is the same shape.
3. **Cardinality.** `min` / `max` per role. `max: 0` means unlimited.
4. **Per-endpoint fields.** Anything that varies by termination (VLAN, SAP,
   encapsulation, inner/outer tag). These are validated and shown in the
   service inspector.
5. **Per-service fields.** Anything shared by every endpoint (VRF name,
   RT/RD, numeric service id, MTU, bandwidth). These live on
   `Service.Fields` as `.Fields` in CLI blobs. The create wizard and
   inspector render a form from `schema`. Well-known names are also
   copied to dedicated Service columns for list views and older API
   clients: `bandwidth_mbps` → `Service.BandwidthMbps`,
   `max_mac_addresses` → `Service.MaxMacAddresses`.
6. **What is not a service field.** Device/site-wide knobs (AS number,
   loopback, NTP, default MTU) belong in **parameter objects** on the
   scope tree, not on the service type. CLI blobs see them as `.Vars`.
7. **Platforms you will push.** Each NOS needs its own CLI object.
   `sros-md` falls back to a `sros` object if no dedicated row exists.
   Huawei `vrp` can store CLI and preview, but cannot apply a CLI session
   yet.

Built-in types seeded on migrate (`cfgmgmt.Seed`):

| Name | Description | Seeded roles | Sync source → NetBox |
| ---- | ----------- | ------------ | -------------------- |
| `ELINE` | L2VPN point to point | `a` and `b`, each min=1 max=1, required `vlan`; schema `bandwidth_mbps` | `eline` → `evpl` |
| `ELAN` | L2VPN multipoint | `endpoint` min=1 max=0, required `vlan`; schema `bandwidth_mbps`, `max_mac_addresses` | `elan` → `vpls` |
| `L3VPN` | L3 multipoint | `endpoint` min=1 max=0; schema `bandwidth_mbps` | `l3vpn` → `vrf` |
| `POLARIX` | Internet | same as L3VPN | (none — on-device VRFs go through L3VPN) |

The create wizard lists every type from this API as a capacity product
(CN/CI), including extra form fields from `schema`. Built-in types cannot
be renamed or deleted; you can still edit description, schema, roles, and
the device-sync mapping. ELINE has **seeded CLI objects** under
`_catalog/cli/ELINE/{eos,ios-xr,sros}`. ELAN/L3VPN/POLARIX have none until
you add them.

---

## 1. Create the service type (catalog)

**GUI:** Config → Catalog → Service types → add.

**API:** `POST /api/config/service-types`

```json
{
  "name": "ELAN",
  "description": "L2VPN multipoint",
  "schema": [
    { "name": "service_numeric_id", "type": "int", "required": false, "description": "Optional numeric id for NOS that need one" }
  ],
  "endpoint_roles": [
    {
      "name": "endpoint",
      "min": 1,
      "max": 0,
      "fields": [
        { "name": "vlan", "type": "vlan", "required": true, "description": "Customer VLAN / SAP tag" }
      ]
    }
  ],
  "sync_source": "elan",
  "netbox_type": "vpls"
}
```

The type is a catalog row, not a tree node. JSON textareas for schema and
roles are acceptable in the GUI; a form editor is a later follow-up.

### Name

- Unique, stored as `Service.ServiceType`. Match what operators already
  say (`ELAN`, not `l2vpn-mp`).
- The create wizard lists every type from this API as a capacity product
  (CN/CI). A new name appears there automatically. Extra create/edit
  fields come from `schema` (for example `bandwidth_mbps` on every built-in
  type, and ELAN's `max_mac_addresses`). Bandwidth is not hardcoded in the
  wizard; add or omit `bandwidth_mbps` on the type to show or hide it.
- **Device-sync mapping** — `sync_source` (`eline` / `elan` / `l3vpn`)
  names the parsed `DeviceConfig` collection; `netbox_type` (`evpl` /
  `vpls` / `vrf`) is what `factum2-device-sync` upserts. Leave both empty
  for GUI-only types.

### Schema (`FieldSchema[]`)

Each entry:

| Field | Meaning |
| ----- | ------- |
| `name` | Key in `Service.Fields` and `.Fields.<name>` in templates |
| `type` | A variable type (table below) |
| `required` | Not enforced on write today; treat as documentation until it is |
| `description` | Operator hint |

`service_numeric_id` is special: if present and an integer, it is also
copied to `.ServiceNumericID` for templates that want a bare int (SR OS
`service-id`, and similar).

### Endpoint roles (`EndpointRole[]`)

| Field | Meaning |
| ----- | ------- |
| `name` | Stored on `ServiceEndpoint.Role`. Unknown names are rejected. |
| `min` | Count of endpoints with this role must be ≥ min (`0` = optional) |
| `max` | Count must be ≤ max. **`0` means no upper bound.** |
| `fields` | Typed map on that endpoint. Validated on save. |

Point-to-point example:

```json
[
  {
    "name": "a",
    "min": 1,
    "max": 1,
    "fields": [{ "name": "vlan", "type": "vlan", "required": true }]
  },
  {
    "name": "b",
    "min": 1,
    "max": 1,
    "fields": [{ "name": "vlan", "type": "vlan", "required": true }]
  }
]
```

Hub-and-spoke example: `hub` min=1 max=1, `spoke` min=1 max=0.

Validation on `PUT /api/service/:id/endpoints` (`cfgmgmt.ValidateEndpoints`):

- Every endpoint has a known `role`, a live `device_id`, and an
  `interface_id` that belongs to that device.
- Required role fields are present and type-check.
- Role counts satisfy min/max.

ELINE uses this API too (`PUT .../eline` remains as an A/B DTO adapter).
ELINE extra checks: physical interfaces, A and B not the same
device+interface. When NetBox is configured, save also reconciles
subinterfaces and an EVPL L2VPN.

### Field types

Same names as config variables (`models.VarType*`). Endpoint fields are
type-checked with the same engine (`cfgmgmt.TypeCheck`).

| Type | JSON value | Notes |
| ---- | ---------- | ----- |
| `string` | string | Optional `constraints.regex` is **not** on `FieldSchema` (only on config variables). |
| `int` | number or numeric string | |
| `bool` | JSON boolean | The endpoint form is a text input; prefer `string`/`enum` until the GUI has a switch. |
| `enum` | string | Allowed values live on config-variable constraints, not `FieldSchema`. Use `string` on roles unless you validate out of band. |
| `ip` | string | Parsed with `net.ParseIP`. |
| `prefix` | string | CIDR (`netip.ParsePrefix`). |
| `vlan` | integer 1–4094 | Preferred for tags. Also copied to `.LocalVLAN` when the field is named `vlan`. |
| `interface_ref` | integer | Factum interface id; not a picker in the endpoint form. |
| `secret` | string | Redaction applies to config variables, not endpoint fields. |
| `list` / `map` | JSON array / object | Awkward in the text form. Keep role fields scalar. |

**Name the VLAN field `vlan`.** `GenericData` only promotes that key to
`.LocalVLAN`. Other tag names stay in `.Endpoint.Fields`.

---

## 2. Add CLI objects under `_catalog/cli`

Service translation is looked up **globally** by `(service_type, platform)`.
Tree location is ignored for that lookup, so put translation objects under
the reserved folder:

`global / _catalog / cli / <ServiceType.Name> / <platform>`

**GUI:** Config tree → expand `_catalog` → `cli` → add a folder named as
the type if missing → right-click → **Add CLI object**. Set **Service
type** in the inspector (empty = baseline/golden CLI, not translation).
Add features; each **add** / **remove** blob is one Go `text/template`.
The update editor is hidden in v1 (missing update ⇒ remove then add).

**API:** `POST /api/config/scopes` then `POST /api/config/scopes/:id/features`

```json
{
  "parent_id": 12,
  "kind": "cli",
  "name": "eos",
  "platform": "eos",
  "payload_kind": "cli",
  "service_type_id": 2
}
```

| Field | Rule |
| ----- | ---- |
| `platform` | Lower-cased NetBox platform: `eos`, `ios-xr`, `sros`, `sros-md`, `vrp`. Unique per type for translation objects. |
| `payload_kind` | Default `cli`. `netconf` / `restconf` can be stored and previewed; **push requires `cli`**. |
| `service_type_id` | Set for translation. Empty/zero = baseline CLI (applies when the object's **parent** is on the device ancestor chain). |
| Context | Empty = no wrap. When `enter` is set, render wraps add with enter/exit. |

Do **not** put golden/baseline CLI under `_catalog`. `_catalog` is a child
of `global` but is **not** an ancestor of a PE under a site, so baseline
objects placed there would never be collected. Global baseline CLI objects
are **direct children of `global`**.

Seeded ELINE CLI objects are refreshed from embed files on migrate **only**
when the stored features + context still match `seed_checksum`. Operator
edits are left alone. Your new objects are never overwritten by Seed.

### Template language

Each feature blob is parsed with `missingkey=error`: a reference to an
absent field fails the preview/push. Guard optional data with `{{if}}`.
`{{range}}` / `{{define}}` are legal inside a blob. Output is split on
newlines; blank and whitespace-only lines are dropped. Write one CLI
command (or MD-CLI line) per line.

Allowed functions (no file, HTTP, or shell):

| Func | Use |
| ---- | --- |
| `join` | `strings.Join` |
| `include` | `{{include "macro-name"}}` — body of a `ConfigMacro`. Nested at most 8 deep. |
| `eq` / `ne` | Equality via `fmt.Sprint`, so `1` and `"1"` compare equal. |

**Cleanup contract** (generic push, several endpoints on one device):

1. Remove (cleanup) is rendered **once** for the first endpoint, then each
   endpoint's add body.
2. Put shared teardown in the feature **remove** blob, keyed by `.Name`
   (the service ID). Assume the object may not exist yet.
3. Empty context: remove and add are emitted as-is. Non-empty `enter`:
   one enter / remove-or-add / exit wrap (`RemoveAtRoot` true: remove
   unwrapped, then wrapped add).

Migrated ELINE objects are one feature with empty context; the add blob is
the old apply template with the cleanup invoke stripped, and the remove
blob is the old cleanup define.

### Generic template context

Service-translation CLI executes against `cfgmgmt.GenericRenderData`:

```
.Name               string            Service.ServiceID (e.g. CN00012)
.Description        string            Service.Comment (ELINE: "ID=<ServiceID> <customer>")
.ServiceNumericID   int               Service.PseudowireID, else Fields["service_numeric_id"]
.Fields             map[string]any    Service.Fields
.Endpoint.Role      string
.Endpoint.DeviceID  uint
.Endpoint.InterfaceID uint
.Endpoint.Fields    map[string]any    this termination's fields
.Vars               map[string]any    resolved config variables at the interface
.Device             DCIMDevice        id, name, platform, site, role, model, …
.Interface          DCIMInterface     id, name, description, enabled, type, vlans, addresses
.LocalIface         string            Interface.Name
.LocalVLAN          int               Endpoint.Fields["vlan"], else 0
.Role               string            same as Endpoint.Role
.PeerLocalIface     string            other endpoint on the same device (ELINE)
.PeerLocalVLAN      int
.Remote             *ELINERemote      other endpoint on a different device (ELINE)
.SDPID              int               SR OS shared SDP id from neighbor last octet
.StaleSubinterfaces []ELINEStale      previous apply's leftover subinterfaces
```

`.Vars` is a `map`; use `{{index .Vars "mtu"}}` (not `.Vars.mtu`). Missing
keys on a map index yield nil rather than `missingkey=error`.

DCIM is read-only inventory. Do not try to write it from a template.

Non-ELINE objects leave Peer/Remote/SDPID/Stale zero; do not reference them.

Baseline CLI objects see `.Name`, `.Device`, `.Vars` (and `.Interface` /
`.LocalIface` when parented under an interface). They do **not** see
service endpoints.

### Example: EOS ELAN (VPLS-shaped) add blob

Sketch only — match your own lab's CLI. Cleanup keyed by service ID in
the **remove** blob:

```
no router bgp vpls {{.Name}}
```

Add:

```
interface {{.LocalIface}}
no switchport
interface {{.LocalIface}}.{{.LocalVLAN}}
description {{.Description}}
encapsulation vlan
client dot1q {{.LocalVLAN}}
exit
exit
router bgp
vpls {{.Name}}
vlan {{.LocalVLAN}}
interface {{.LocalIface}}.{{.LocalVLAN}}
exit
exit
```

SR OS objects should be MD-CLI block-paste (see
`internal/drivers/templates/sros_eline.tmpl`): one `/configure … { }`
block, `delete` in remove, apply-groups for invariants.

IOS-XR objects should return to `root` the way
`internal/drivers/templates/iosxr_eline.tmpl` does.

### Shared snippets

Config → Catalog → Macros. A CLI blob can `{{include "eline-defaults"}}`.
Macros see the **same data** as the caller. Use them for repeated banners
or apply-group names, not for per-platform whole services (that is what
CLI objects are).

---

## 3. Parameter objects

Variable **definitions** (name, type, default, constraints) live in the
Variables catalog. Values live on **parameter objects** in the tree, not
on arbitrary folders.

**GUI:** right-click a folder / site / location / device / interface /
service → **Add parameter object**. Select it and **Assign**. Scalar types
get typed inputs; list/map stay JSON.

A parameter object applies to its **parent and the parent's descendants**
(closest ancestor wins; at one parent, higher `sort_order` wins). The
reserved child named `parameters` is what migrate copies folder
assignments onto. Extra named objects (`ntp`, `qos-core`) are first-class
nodes you can move.

`PUT /api/config/assignments` with a non-parameter `scope_id` still
**remaps** onto the reserved `parameters` child (folder Assign remains as
compatibility). Prefer assigning on the parameter node itself.

Secrets are redacted on read (`***`). A PUT that sends `***` or omits the
value leaves the stored secret unchanged.

---

## 4. Preview before you push

**GUI:** Config tree, Preview dock (device from the current selection or
the picker).

**API:**

```http
POST /api/config/render
{ "service_id": 123 }
```

or `{ "device_id": 45 }` for that device's baseline CLI objects **plus**
every terminating service.

Each source is `{source, kind, platform, payload_kind, commands[], error}`.
`kind` is `cli` for baseline objects (`source` = `"cli:<name>"`) and
`service` for translation (`source` still `"device / iface (role)"`). Fix
`error` (unknown field, missing CLI object, template parse) before
pushing.

Baseline CLI on the ancestor chain all **preview** (ancestor first, then
device, then per-interface). **Apply** is still
`POST /api/service/:id/push` (cleanup then apply for that service only).
Baseline is not sent in a service push.

---

## 5. Instantiate a service in the tree

1. **Create** from the tree: right-click a folder (or a device — the
   canonical node is parented at the device's folder / `_services`, not
   under the interface). Category CN/CI, type, company. The node is
   created with **zero endpoints**; complete the set in the inspector.
   Lime create is not offered.

   Or **attach** an existing typed CN/CI (Lime rows included). Commercial
   Lime fields stay read-only; you can still set type and endpoints.

   The wizard (`POST /api/service`) still works; attach the row afterwards
   if you want it in the tree. New Lime-synced rows are **not**
   auto-placed; migrate placed existing typed rows under `_services`.

2. **Endpoints** in the inspector (same `PUT /api/service/:id/endpoints`
   as the service dialog):

   ```http
   PUT /api/service/:id/endpoints
   {
     "fields": { "service_numeric_id": 12001 },
     "endpoints": [
       { "role": "endpoint", "device_id": 10, "interface_id": 44, "fields": { "vlan": 100 } },
       { "role": "endpoint", "device_id": 11, "interface_id": 80, "fields": { "vlan": 100 } }
     ]
   }
   ```

   The whole set is replaced. Endpoint children of the service node are
   projected from that table. Virtual **service refs** appear under each
   involved device/interface (not stored rows). Drag a ref onto another
   interface to rebind. ELINE also derives `PseudowireID` and, when
   NetBox is configured, reconciles L2VPN + subinterfaces.

3. **Push** `POST /api/service/:id/push` with device credentials
   (`username`, `password`) from the service dialog. Per endpoint device:

   - look up the translation CLI object for `service_type` + device
     platform (pack fallback until those tables drop)
   - require `payload_kind=cli` and a `CLISessionApplier` driver
     (`eos`, `ios-xr`, `sros` / `sros-md`)
   - render cleanup once, then each endpoint body, one CLI session
   - ELINE: `PrepareELINEApply` (SR OS SDP guard), stamp `AppliedEndpoint*`,
     teardown abandoned devices

   EOS uses the session name as `configure session`; other platforms ignore
   it. Failures are returned per device; there is no automatic rollback of
   siblings that already succeeded.

Default tree delete of a service node **detaches** it (inventory remains).
Devices are attach-only; Detach never deletes the DCIM row.

---

## Config variables vs service fields vs endpoint fields

| Data | Lives on | Template | When to use |
| ---- | -------- | -------- | ----------- |
| Config variable | Parameter object assignment | `.Vars["name"]` | Inherited (global → site → device → interface). Platform-filterable. |
| Service field | `Service.Fields` / type `schema` | `.Fields.name` | One value for the whole instance (VRF, RT, numeric id). |
| Endpoint field | `ServiceEndpoint.Fields` / role `fields` | `.Endpoint.Fields.name` and, for `vlan`, `.LocalVLAN` | Varies per termination. |
| Inventory | Device / interface row | `.Device` / `.Interface` / `.LocalIface` | Already in DCIM; do not duplicate as a field. |

Resolution walks the interface's parameter children and ancestor chain
(closest wins), then the variable's default. Required variables with no
value fail resolve; render skips failed vars in `.Vars` rather than
aborting the whole device.

---

## ELINE extras

ELINE is a normal cfgmgmt type with extra NetBox and peer-render behaviour:

- Endpoints: `PUT /api/service/:id/endpoints` (roles `a`/`b`).
  `PUT /api/service/:id/eline` is an A/B DTO adapter onto the same helper.
- Push: `POST /api/service/:id/push` (and `/eline/push`, same path).
- Render context: `GenericRenderData` with `.Remote`, `.PeerLocal*`,
  `.SDPID`, `.StaleSubinterfaces` filled from sibling endpoints.
- CLI objects: seeded from `internal/drivers/templates/{eos,iosxr,sros}_eline.tmpl`
  under `_catalog/cli/ELINE/`.
- Edit the CLI feature in the tree to tweak CLI; leave `seed_checksum`
  mismatched so migrate will not clobber it. Change the embed file + Seed
  when the default for **new** databases should move.
- Reverse-import: `factum2-netbox sync` writes `service_endpoints` for every
  L2VPN whose type matches a service type's `netbox_type` (evpl→ELINE,
  vpls→ELAN), using that type's endpoint roles. It does not create Service
  rows. Endpoint children are projected if a canonical tree node already
  exists.

Do not add `EndpointA*` columns for a new type.

---

## Checklist for a new type

- [ ] Topology and roles written down (names, min, max).
- [ ] Per-endpoint fields scalar (`vlan`, `string`, `int`, `ip`). VLAN
      field named `vlan` if templates need `.LocalVLAN`.
- [ ] Per-service data either in `schema` / `Service.Fields` or in
      parameter objects — not both for the same key.
- [ ] Type created in Catalog → Service types. Confirm it appears in the
      create wizard.
- [ ] One CLI object per platform you will push, under
      `_catalog/cli/<Type>/`, `payload_kind=cli`, service type set.
- [ ] Feature remove blob keyed by `.Name`, safe if the object is absent.
- [ ] Add blob uses `{{if}}` around optional maps; `missingkey=error`.
- [ ] Preview from the Config tree for each platform with a lab device +
      two endpoints.
- [ ] Push to a lab device; confirm cleanup + re-apply is idempotent.
- [ ] `sros-md` either has its own CLI object or can inherit `sros`.
- [ ] No new Go types, no new `cmd/`, no ELINE columns.

When the CLI cannot express the service, that is a **driver** gap
(`internal/drivers/README-DRIVERS.md`), not a cfgmgmt one. CLI objects
cannot reach NETCONF/OpenConfig until `payload_kind` other than `cli` is
applied.

---

## API map

| Method | Path | Purpose |
| ------ | ---- | ------- |
| GET/POST | `/api/config/scopes` | List / create tree nodes (`kind`: folder, site, location, device, parameter, cli, service, …) |
| PUT/DELETE | `/api/config/scopes/:id` | Update / delete (service default = detach; device uses `/detach`) |
| POST | `/api/config/scopes/:id/move` | Reparent (`parent_id`, optional `sort_order`) |
| POST | `/api/config/scopes/:id/detach` | Device only: drop config children, keep DCIM row |
| GET/POST | `/api/config/scopes/:id/features` | List / create CLI features |
| PUT/DELETE | `/api/config/features/:id` | Update / delete a feature |
| GET/POST | `/api/config/service-types` | Catalog: list / create type (`sync_source`, `netbox_type`) |
| PUT/DELETE | `/api/config/service-types/:id` | Update / delete (not built-in delete) |
| GET/POST | `/api/config/variables` | Variable definition catalog |
| GET/PUT | `/api/config/assignments` | Values on a parameter node (non-parameter `scope_id` remaps) |
| GET/POST | `/api/config/macros` | Named `{{include}}` snippets |
| POST | `/api/config/render` | Preview device or service |
| PUT | `/api/service/:id/type` | Set type on an instance (incl. Lime) |
| GET/PUT | `/api/service/:id/endpoints` | Endpoints (including ELINE) |
| POST | `/api/service/:id/push` | Apply CLI (service translation only) |
| * | `/api/config/platform-packs`, `/api/config/templates` | Gone (410); use CLI objects |

Write routes need `RequireWrite`. Config seed runs from
`util.MigrateDatabase` → `cfgmgmt.Seed`.
