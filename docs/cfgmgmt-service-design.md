# Designing a capacity service in the GUI

How to define a **capacity service** (CN/CI) that Factum can validate, preview,
and push. You build the definition, the CLI that implements it, and the
technical instance **in the Config GUI**. You do **not** add a Go package per
product.

Factum ships **no built-in service types** and **no seeded translation CLI**.
ELINE, ELAN, POLARIX, and similar names are only examples of definitions you
create in Catalog. Import/export of definition packages and an L3VPN product
are follow-ups.

Related:

| Piece | Where |
| ----- | ----- |
| Operator GUI | [user/config.md](user/config.md), [user/services.md](user/services.md) |
| Tree architecture | [cfgmgmt-tree-objects.md](cfgmgmt-tree-objects.md) |
| Product design | [cfgmgmt-service-definitions.md](cfgmgmt-service-definitions.md) |
| Engine | `internal/cfgmgmt/` |
| HTTP | `web/handler_config.go`, `web/handler_service.go` |

---

## Breaking migrate (goose 00004)

`internal/dbmigrate/sql/00004_service_definitions.sql` is a **one-shot wipe**.
It runs once via `factum2-web migrate` (`dbmigrate.Up`). It is **not**
`cfgmgmt.Seed`.

The migrate:

1. Clears `maintenance_notifications.service_ids` (not `maintenance_windows`).
2. Deletes optical hops/paths for every `services.id`.
3. Deletes all `service_endpoints`.
4. Deletes translation CLI (`kind=cli` with `service_type_id` set) and
   canonical service trees — **features and assignments first, then scopes**
   (there is no SQL `ON DELETE CASCADE` on those tables). Baseline CLI
   (`service_type_id` IS NULL) is kept.
5. **`DELETE FROM services`** including Lime rows. Local-only commercial rows
   are gone. Lime-owned CNs come back on the next `factum2-lime` sync, without
   a service type.
6. Deletes `service_connection_types` and `service_types`.
7. Drops `endpoint_roles`; adds `interfaces`, connection types, and endpoint
   `applied_*` columns.

`cfgmgmt.Seed` (every start, including after 00004) still creates `global`,
`_catalog`, `_catalog/cli`, `_services`, leftover pack/template copy, and
assignment COPY/MOVE. It does **not** insert types or translation CLI and
does **not** delete operator data.

Rollback is a Postgres restore, not the goose Down. Redeploying an old binary
after 00004 cannot run (`endpoint_roles` is gone).

---

## What you are designing

Four pieces, all in the Config GUI:

1. **Definition** (Catalog → Service types) — form-built schema, one
   homogeneous interfaces spec (min/max, no named roles), optional connection
   types with PNG/WebP images, optional `sync_source` / `netbox_type`.
2. **CLI objects** under `global / _catalog / cli / <Name> / <platform>` —
   per-NOS translation. Saving a definition creates the type folder, not
   per-platform CLI objects.
3. **Parameter objects** and **resource objects** in the tree — inherited
   knobs (`.Vars`) and named CIDR lists for prefix fields.
4. **Technical service** — a `models.Service` row with `service_type` =
   definition name, plus homogeneous `service_endpoints` (`role` is always
   `"interface"`).

Wavelength (VL/VI) and dark fiber (LF/LI) are **not** cfgmgmt services. They
have no definition and no device CLI.

```
  ServiceType catalog (schema, interfaces, connection_types)
       │
       ├─ CLI object  (_catalog/cli/<Name>/<platform>)
       │     features: add / remove command blobs
       │
       ├─ Parameter object  (assignments → .Vars)
       ├─ Resource object   (CIDR list → Allocate)
       │
       └─ Service object in the tree  (CN/CI + service_type)
              └─ ServiceEndpoint[]  (role=interface, device, interface, fields)
                     │
                     ▼
              Render  →  Preview (docked on the Config tree)
              Push    →  CLI session on each endpoint device
```

---

## Decide this before you save a definition

Changing field names after instances exist is a data migration, not a rename
of the type.

1. **Topology.** Point-to-point (exactly two UNIs), or unlimited multipoint?
   Named roles (`a`/`b`, hub/spoke) are gone. Homogeneous interfaces only.
2. **Cardinality.** `interfaces.min` / `interfaces.max`. **`max: 0` means
   unlimited.** `unique` rejects the same `device_id` + `interface_id` twice
   (two VLANs on one port: leave `unique` false).
3. **Per-interface fields.** Anything that varies by UNI (VLAN, ServiceID,
   optional bandwidth). Same field **name** as a service-level field can stay
   empty on the UNI; render/push copies the service value (not
   `default_bandwidth`).
4. **Per-service fields.** Shared knobs (MTU, bandwidth, prefix lists). Live
   on `Service.Fields` as `.Fields` in CLI blobs. Well-known names
   `bandwidth_mbps` and `max_mac_addresses` are also copied to Service columns
   for list views.
5. **Not a service field.** Device/site-wide knobs belong on **parameter
   objects** (`.Vars`). Prefix pools belong on **resource** nodes.
6. **Connection types.** Optional named choices with images. One pick per
   instance (`.ConnectionType` is the name string). Same CLI object for every
   choice; branch with `{{if eq .ConnectionType "nni-vlan"}}`.
7. **Platforms.** Each NOS needs its own CLI object. `sros-md` falls back to
   `sros`. Huawei `vrp` applies CLI sessions like EOS / IOS-XR / SR OS.

Example shapes (you create these; they are not shipped):

| Product | `interfaces` | Typical fields |
| ------- | ------------ | -------------- |
| Point-to-point L2 | min=2 max=2 unique | service `bandwidth_mbps`; UNI `vlan` |
| Multipoint L2 | min=0 max=0 | service `bandwidth_mbps`, `max_mac_addresses`; UNI `vlan`, `service_id` |
| Internet / peering | min=0 max=0 | service prefix **lists** with `items.resource`; UNI `service_id` |

---

## 1. Create the definition (Catalog)

**GUI:** Config → Catalog → Service types → add. Form builder for service
fields, interfaces spec, connection types (PNG/WebP, 512KiB), and NetBox
mapping. There are no schema/roles JSON textareas.

**API:** `POST /api/config/service-types` (`RequireWrite`). Posted
`endpoint_roles` is **400**.

```json
{
  "name": "ELAN",
  "description": "L2VPN multipoint",
  "schema": [
    { "name": "bandwidth_mbps", "type": "int", "unit": "Mbps" },
    { "name": "max_mac_addresses", "type": "int" }
  ],
  "interfaces": {
    "min": 0,
    "max": 0,
    "unique": false,
    "fields": [
      { "name": "vlan", "type": "vlan", "required": true },
      { "name": "service_id", "type": "service_id" },
      { "name": "bandwidth_mbps", "type": "int", "unit": "Mbps" }
    ]
  },
  "connection_types": [
    { "name": "nni-vlan", "sort_order": 0 }
  ],
  "sync_source": "elan",
  "netbox_type": "vpls"
}
```

The type is a catalog row, not a tree node. Creating it ensures
`global/_catalog/cli/<Name>` exists. It does **not** create per-platform CLI
objects.

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET/POST | `/api/config/service-types` | POST may include `connection_types` without ids. |
| GET/PUT/DELETE | `/api/config/service-types/:id` | Any type may be renamed or deleted (`builtin` is unused). Delete **409** if any `services.service_type` equals the **current** name. Rename updates those rows and the `_catalog/cli/<old>` folder in the same transaction. |
| PUT/GET | `/api/config/service-types/:id/connection-types/:ctid/image` | Write: 512KiB, PNG/WebP. Empty body clears. Read: `Content-Type` from the row + `X-Content-Type-Options: nosniff`. |

### FieldSchema

Same struct on `schema[]` and `interfaces.fields[]`. Nested `items` is the
same shape; list items are **nameless** (name uniqueness does not apply).

Top-level `name`: required, `[a-z][a-z0-9_]*`, unique within `schema` and
separately unique within `interfaces.fields`. `snpa` is accepted as an alias
of `mac` and stored as `mac`. **No `regex` on definition strings** (regex
stays on config-variable constraints). Nesting depth ≤ 8.

| Field | Meaning |
| ----- | ------- |
| `name` | Key in `Service.Fields` / endpoint `Fields` and templates |
| `type` | See table below |
| `required` | Enforced on write (`ValidateServiceFields` / `ValidateEndpoints`). Empty interface fields still inherit a same-name service value at **render/push** |
| `description` | Operator hint |
| `min` / `max` | Inclusive. **int**: no bound if nil. **vlan**: default 1–4094. **list**: length bounds |
| `unit` | int only; templates see `(index .FieldMeta "bandwidth_mbps").Unit` — not stored in the value |
| `bool_true_label` / `bool_false_label` | Display; empty → Yes / No |
| `enum` | `{label, value}[]`; required and unique `value`s when `type=enum` |
| `items` | List element schema (`type=list` only; `items.type` must not be `list`) |
| `resource` | Name of a `kind=resource` pool. Allowed on `prefix` / `ipv4_prefix` / `ipv6_prefix`, **including `items`**. Not on `type=list` itself. No existence check at definition save |

| Type | JSON | Notes |
| ---- | ---- | ----- |
| `string` | string | No regex |
| `int` | number | Unit is metadata only |
| `bool` | boolean | `false` is a value, not empty |
| `enum` | string | Must match an `enum[].value` |
| `vlan` | number | Bounds from min/max or 1–4094 |
| `mac` | string | Stored canonical `aabb.ccdd.eeff`. Accepts colon/hyphen/Cisco/bare on write |
| `ipv4` / `ipv6` / `ip` | string | `netip.ParseAddr`; family enforced for ipv4/ipv6 |
| `ipv4_prefix` / `ipv6_prefix` / `prefix` | string | Stored `netip.ParsePrefix` then `Masked().String()`. Occupancy compares this form |
| `service_id` | number (uint) | Commercial `services.id`. **`0` / omit / null = empty** for fill and required |
| `list` | array | Each element checked against `items`. `[]` is a value, not empty |

`service_numeric_id` (int) is copied to `.ServiceNumericID` when
`PseudowireID` is unset.

### Interfaces spec

```json
{ "min": 2, "max": 2, "unique": true, "fields": [ { "name": "vlan", "type": "vlan", "required": true } ] }
```

`Min >= 0`. If `Max > 0` then `Max >= Min`. Endpoint `Role` is always
`"interface"` (empty on write is defaulted). `PUT /api/service/:id/endpoints`
validates count, uniqueness, live device/interface, and field types. There is
no physical-port filter and no platform allowlist on the picker.

### Connection types

Child rows with bytea images. List/get omits image bytes; `has_image` +
`image_url` =
`/api/config/service-types/:id/connection-types/:ctid/image`.

PUT of the parent type **replaces by id** in one transaction: DELETE omitted
ids (409 if a service still references them) → UPDATE remaining (name /
sort_order; **leave image**) → INSERT `id == 0`. If the definition has ≥1
connection type, the instance must pick one.

### NetBox mapping

`sync_source`: `eline` / `elan` / `l3vpn`. `netbox_type`: `evpl` / `vpls` /
`vrf`. Empty = no NetBox, no device-sync collection. Operators who define
an L2 point-to-point set `sync_source=eline` and `netbox_type=evpl`
themselves.

Pseudowire IDs are assigned on reconcile **only when** `netbox_type=evpl`
**and** NetBox integration is active. Push does not require a pseudowire.

---

## 2. Same-name defaults, ServiceID, realize / unrealize

**Fill** (`cfgmgmt.FillEndpointDefaults`): for each interfaces field, if the
endpoint value is empty and a same-named service field is not empty, copy it
into the **render** map. Does not `Save` endpoints.

Empty: missing, JSON null, `""`; for `service_id` also numeric `0`. **Not**
`false`, `0` (other types), or `[]`.

**ServiceID picker:** `GET /api/service?q=<substr>&category=CN,CI,freetext`.
Omit `category` → **all** rows (Services page, including VL/VI/LF/LI). The
picker always sends that category list. Stored value is the commercial
**pk**, not the CN string.

`service_id` means three different things — do not overload them:

| Key | Where | Meaning |
| --- | ----- | ------- |
| `service_id` | `models.Service` / list DTO | Commercial CN/CI **string** (`CN00012`) |
| `service_id` | `ConfigScopeDTO` | Attach **pk** (`services.id`) |
| `service_id` | `Fields` JSON, type `service_id` | Commercial **pk** (uint) |
| `attach` | `ConfigScopeDTO.Attach` | Insert a **new** technical row |

**Same-row realize** (typical point-to-point on an existing CN):

1. `PUT /api/service/:id/type` with `service_type`, `fields`,
   `connection_type_id` (allowed on Lime; not `ApiServiceUpdate`).
2. `POST /api/config/scopes` with `service_id` = that pk.
3. `PUT /api/service/:id/endpoints`.

**Two-row realize** (typical multipoint): tree create
(`ConfigScopeDTO.Attach` / `CreateServiceFromTree`) inserts a new
Factum-sourced row that already has `service_type`. Each UNI `fields.service_id`
points at a commercial CN. Lime rows do not get endpoints in this pattern.

**Unrealize:** `POST /api/service/:id/unrealize` with
`{remove_from_netbox, remove_from_device}`. Allowed on Lime. Tears down
device/NetBox like delete, then detaches the tree node, deletes endpoints,
clears `service_type`, cfgmgmt `fields`, `connection_type_id`,
`pseudowire_id`. The commercial row remains. `DELETE /api/service/:id` still
**403**s Lime.

Legacy `PUT /api/service/:id/eline` and `POST /api/service/:id/eline/push`
return **410**
(`{"error":"use PUT /api/service/:id/endpoints and POST /api/service/:id/push"}`).

---

## 3. Resource objects (prefix pools)

**GUI:** right-click folder / site / location / device / **interface** →
**Add resource**. Inspector: name, description, CIDR list (not JSON).

**Not** under `service`, parameter, CLI, service_endpoint, or another
resource. Allocate walks **interface → device → ancestors → global**. The
canonical service node is not on that chain, so a pool hung on the service
object would never win.

- `Name` is `FieldSchema.resource`. Unique among siblings of kind `resource`
  (**409** on create/rename/move). Different parents may reuse a name
  (closest ancestor wins).
- `Payload.cidrs`: each `netip.ParsePrefix`, stored `Masked().String()`,
  unique in the node, mixed family allowed, cap **256**.
- `Enabled` honored.

**Walk:** closest enabled child with that name (sort_order DESC, name DESC).
No winner → 400 `no resource named %q on the ancestor chain`.

**Occupancy (v1):** a CIDR is occupied iff any service or endpoint field
(including list items) equals that canonical string **and** the matching
schema node has `resource` equal to that name. Scan is global by resource
**name**. Allocate does **not** write; two saves can pick the same prefix
(**last-write-wins**).

**API:** `GET /api/config/resources/free?interface_id=&name=&family=`
(`family` = `4` / `6` / `0`). `device_id` is a fallback when the interface is
unknown. No `service_id` query param. Response:
`{scope_id, cidrs:[{prefix, free}]}`.

---

## 4. CLI objects under `_catalog/cli`

Looked up **globally** by `(definition name, platform)` via
`LookupCLIObject`. Tree location is ignored for translation. Conventional
path:

`global / _catalog / cli / <ServiceType.Name> / <platform>`

**GUI:** Config tree → `_catalog` → `cli` → type folder → **Add CLI object**.
Set **Service type** (empty = baseline, not translation). Add features; each
**add** / **remove** blob is one Go `text/template`. The update editor is
hidden in v1 (missing update ⇒ remove then add).

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
| `platform` | Lower-cased NetBox platform: `eos`, `ios-xr`, `sros`, `sros-md`, `vrp`, `ciscosmb`. Unique per type for translation objects. |
| `payload_kind` | Default `cli`. `netconf` / `restconf` can be stored and previewed; **push requires `cli`**. |
| `service_type_id` | Set for translation. Empty/zero = baseline CLI (applies when the object's **parent** is on the device ancestor chain). |
| Context | Pattern language: `interface <name>`, `router bgp <as>` (not raw RE2). Empty / `global` = no wrap. When `enter` is set: one enter, remove, add, exit. `RemoveAtRoot` = remove unwrapped, then wrapped add. |

Do **not** put golden/baseline CLI under `_catalog`. `_catalog` is a child of
`global` but is **not** an ancestor of a PE under a site. Global baseline CLI
objects are **direct children of `global`**.

Missing translator → preview/push error `no CLI object for <Name>/<platform>`.

### Template language

Each feature blob is parsed with `missingkey=error`. Guard optional data with
`{{if}}`. Output is split on newlines; blank lines are dropped.

| Func | Use |
| ---- | --- |
| `join` | `strings.Join` |
| `include` | `{{include "macro-name"}}` — `ConfigMacro`, nested at most 8 |
| `eq` / `ne` | Equality via `fmt.Sprint` |
| `sdpid` | `{{ sdpid (index .Others 0).NeighborIP }}` — SR OS SDP from neighbor last octet. Errors on empty/non-IPv4 |
| `macColon` / `macHyphen` / `macCisco` | MAC display forms |

**Cleanup contract** (generic push, several endpoints on one device):

1. Remove is rendered **once** for the first endpoint, then each endpoint's
   add body.
2. Put shared teardown in the feature **remove** blob, keyed by `.Name`.
3. Empty context: remove and add as-is. Non-empty `enter`: wrap as above.

### Generic template context (`GenericRenderData`)

There is no `.Remote`, `.LocalVLAN`, `.PeerLocal*`, `.SDPID` field, or
`.StaleSubinterfaces`. Peers are `.Others` / `.Interfaces`.

```
.Name               string            Service.ServiceID (e.g. CN00012)
.Description        string            Service.Comment
.ServiceNumericID   int               PseudowireID, else Fields["service_numeric_id"]
.ConnectionType     string            chosen connection type name ("" if none)
.Fields             map[string]any    Service.Fields
.FieldMeta          map[string]FieldMeta
.Vars               map[string]any    resolved config variables
.Device             DCIMDevice
.Interface          DCIMInterface
.LocalIface         string            Interface.Name
.Current            RenderEndpoint    this UNI (after same-name fill)
.Interfaces         []RenderEndpoint  all UNIs including Current
.Others             []RenderEndpoint  Interfaces without Current
```

`RenderEndpoint`: `Device`, `Interface`, `LocalIface`, `Fields`,
`Commercial`, `NeighborIP`.

`NeighborIP` is the peer device loopback **only when that peer’s device ≠
current device**. Same-device peers leave it empty — use
`(index .Others 0).LocalIface` and `index (index .Others 0).Fields "vlan"`.
Do not call `sdpid` on an empty NeighborIP.

**Peer access** (`.Others0` is not valid `text/template`):

```
{{ (index .Others 0).NeighborIP }}
{{ index .Current.Fields "vlan" }}
{{ range .Interfaces }}{{ .LocalIface }}{{ end }}
{{ (index .FieldMeta "bandwidth_mbps").Unit }}
```

`.Vars` is a map: `{{index .Vars "mtu"}}` (not `.Vars.mtu`).

Baseline CLI objects see `.Name`, `.Device`, `.Vars` (and `.Interface` /
`.LocalIface` when parented under an interface). They do **not** see
service endpoints.

VLAN is **not** promoted to `.LocalVLAN`. Use `index .Current.Fields "vlan"`.

### Example: EOS add blob (sketch)

Remove:

```
no router bgp vpls {{.Name}}
```

Add:

```
interface {{.LocalIface}}
no switchport
interface {{.LocalIface}}.{{index .Current.Fields "vlan"}}
description {{.Description}}
encapsulation vlan
client dot1q {{index .Current.Fields "vlan"}}
exit
exit
```

Point-to-point neighbor:

```
neighbor {{ (index .Others 0).NeighborIP }}
```

---

## 5. Parameter objects

Variable **definitions** live in the Variables catalog. Values live on
**parameter objects** in the tree.

**GUI:** right-click a folder / site / location / device / interface /
service → **Add parameter object**. Scalar types get typed inputs; list/map
stay JSON.

Closest ancestor wins; at one parent, higher `sort_order` wins. Parameter
children of a **service** node merge into `.Vars` for that service’s
translation only (not into Allocate).

`PUT /api/config/assignments` with a non-parameter `scope_id` still remaps
onto the reserved `parameters` child. Prefer assigning on the parameter node.

Secrets are redacted on read (`***`). A PUT that sends `***` or omits the
value leaves the stored secret unchanged.

---

## 6. Preview, instantiate, push, move

**Preview:** Config tree Preview dock, or `POST /api/config/render` with
`{ "service_id": 123 }` or `{ "device_id": 45 }`. Baseline CLI **previews**;
**apply** is still `POST /api/service/:id/push` (that service only).

**Create from the tree:** right-click a folder/site/location (or New
service). Pick a definition (cards + connection-type thumbnails). The form
is `schema` + connection type + homogeneous interfaces (`interfaces.min`
slots of `role: "interface"`; add/remove when max is 0). `_catalog` is not a
customer-service list. Default parent is `_services`.

```http
PUT /api/service/:id/endpoints
{
  "fields": { "bandwidth_mbps": 100 },
  "endpoints": [
    { "role": "interface", "device_id": 10, "interface_id": 44, "fields": { "vlan": 100 } },
    { "role": "interface", "device_id": 11, "interface_id": 80, "fields": { "vlan": 200 } }
  ]
}
```

The whole set is replaced. Drag a virtual **service_ref** onto another
interface to rebind (same PUT; server tears down Applied* on the old
device, then replace, then add on the new device). Device login for that
teardown/add is `DeviceSyncAuth` (exact name, else `default`).

**Push** (`POST /api/service/:id/push`) per endpoint device:

- `LookupCLIObject(definition name, platform)` (`sros-md` → `sros`)
- require `payload_kind=cli` and a `CLISessionApplier`
- fill same-name defaults, render `GenericRenderData`, apply remove then add
- stamp `AppliedDeviceID` / `AppliedIface` / `AppliedPlatform` /
  `AppliedFields` on success

No `PrepareELINEApply` on this path. Failures are per device; no automatic
rollback of siblings.

Device login uses `DeviceSyncAuth` (same credentials as device-sync: exact
device name, else `default`). EOS records the operator on the
configure-session description; SR OS and IOS-XR put it on the commit
comment.

Default tree delete of a service node **detaches** it. Devices are
attach-only; Detach never deletes the DCIM row.

---

## Config variables vs service fields vs endpoint fields

| Data | Lives on | Template | When to use |
| ---- | -------- | -------- | ----------- |
| Config variable | Parameter object | `index .Vars "name"` | Inherited (global → site → device → interface) |
| Service field | `Service.Fields` / definition `schema` | `.Fields.name` | One value for the instance |
| Interface field | `ServiceEndpoint.Fields` / `interfaces.fields` | `index .Current.Fields "vlan"` (filled from same-name service field if empty) | Varies per UNI |
| Connection type | `services.connection_type_id` | `.ConnectionType` | One choice per instance |
| Prefix pool | `kind=resource` CIDR list | value already in `.Fields` / `.Current.Fields` after Allocate | Named pool on the UNI’s ancestor chain |
| Inventory | Device / interface row | `.Device` / `.Interface` / `.LocalIface` | Already in DCIM |

---

## Checklist for a new definition

- [ ] Topology written as homogeneous min/max (not named roles).
- [ ] Per-interface and per-service fields named; same name = render default.
- [ ] Prefix lists use `items.resource`; resource nodes sit on site/device/
      interface (not under the service node).
- [ ] Type created in Catalog → Service types (form builder). Confirm it
      appears when creating a technical service from the Config tree.
- [ ] One CLI object per platform you will push, under
      `_catalog/cli/<Name>/`, `payload_kind=cli`, service type set.
- [ ] Feature remove blob keyed by `.Name`, safe if the object is absent.
- [ ] Add blob uses `.Others` / `.Current.Fields` / `.ConnectionType`;
      `{{if}}` around optional maps; `missingkey=error`.
- [ ] Preview from the Config tree; push to a lab device; drag a ref to
      another interface and confirm teardown + add.
- [ ] Unrealize keeps the commercial row; Lime delete still 403s.
- [ ] `sros-md` has its own CLI object or can inherit `sros`.
- [ ] No new Go types, no new `cmd/`, no built-in product package.

When the CLI cannot express the service, that is a **driver** gap
(`internal/drivers/README-DRIVERS.md`), not a cfgmgmt one.

---

## API map

| Method | Path | Purpose |
| ------ | ---- | ------- |
| GET/POST | `/api/config/scopes` | List / create tree nodes (`kind`: folder, site, location, device, parameter, cli, service, resource, …) |
| PUT/DELETE | `/api/config/scopes/:id` | Update / delete (service default = detach; device uses `/detach`) |
| POST | `/api/config/scopes/:id/move` | Reparent |
| POST | `/api/config/scopes/:id/detach` | Device only |
| GET/POST | `/api/config/scopes/:id/features` | CLI features |
| PUT/DELETE | `/api/config/features/:id` | Update / delete a feature |
| GET | `/api/config/resources/free` | Free CIDRs (`interface_id`, `name`, `family`; optional `device_id`) |
| GET/POST | `/api/config/service-types` | Definitions (`interfaces`, `connection_types`; not `endpoint_roles`) |
| PUT/DELETE | `/api/config/service-types/:id` | Update / delete (409 if in use); rename cascades |
| GET/PUT | `/api/config/service-types/:id/connection-types/:ctid/image` | Connection-type image |
| GET/POST | `/api/config/variables` | Variable definition catalog |
| GET/PUT | `/api/config/assignments` | Values on a parameter node |
| GET/POST | `/api/config/macros` | Named `{{include}}` snippets |
| POST | `/api/config/render` | Preview device or service |
| GET | `/api/service` | Commercial list. `q`, optional `category` (omit = all rows) |
| PUT | `/api/service/:id/type` | Set definition + fields + `connection_type_id` (incl. Lime) |
| GET/PUT | `/api/service/:id/endpoints` | Homogeneous endpoints; rebind teardown on PUT |
| POST | `/api/service/:id/push` | Apply CLI (service translation only) |
| POST | `/api/service/:id/unrealize` | Drop realization, keep commercial row |
| DELETE | `/api/service/:id` | Delete row (Lime 403); cleanup flags for any definition |
| PUT / POST | `/api/service/:id/eline`, `.../eline/push` | **410 Gone** |
| * | `/api/config/platform-packs`, `/api/config/templates` | Gone (410); use CLI objects |

Write routes need `RequireWrite`. Config seed runs from
`util.MigrateDatabase` → goose then `cfgmgmt.Seed` (no built-in products).
