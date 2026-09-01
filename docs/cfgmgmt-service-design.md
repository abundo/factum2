# Designing a Factum service type

How to add a **capacity service** (CN/CI) that Factum can validate, preview,
and push to devices. The machinery lives in `internal/cfgmgmt`. You design
the type and its platform packs in the database (Config GUI or API); you
do **not** add a new Go package per service.

ELINE uses the same **generic endpoints** + **platform packs** path as
every other capacity type. It still has NetBox L2VPN reconcile on save
(and reverse-import from `factum-netbox sync`). Do not add
`Service.EndpointA/B*` columns for a new type.

Related code:

| Piece | Where |
| ----- | ----- |
| Models | `models/config.go` (`ServiceType`, `PlatformPack`, `ServiceEndpoint`) |
| Engine | `internal/cfgmgmt/` |
| HTTP | `web/handler_config.go`, `web/handler_service.go` |
| GUI | Config page (types + packs), service edit dialog (endpoints + push) |
| Seeded ELINE packs | `internal/drivers/templates/*.tmpl` |

---

## What you are designing

A Factum **service type** is a vendor-agnostic class of connectivity:

- **Name** — stored on `Service.ServiceType` (e.g. `ELAN`, `L3VPN`).
- **Schema** — optional per-service fields (`Service.Fields`), available in
  templates as `.Fields`.
- **Endpoint roles** — how many terminations, on which devices/interfaces,
  and which typed fields each termination carries.
- **Platform packs** — one Go `text/template` per NOS that turns that
  intent into CLI (the only payload kind that can be applied today).

A **service instance** is a `models.Service` row (create wizard, Lime sync,
or API) with that type set. Endpoints are a separate table
(`service_endpoints`), replaced as a set via `PUT /api/service/:id/endpoints`.

Wavelength (VL/VI) and dark fiber (LF/LI) are **not** cfgmgmt services.
They have no `ServiceType` and no device CLI. Do not invent packs for them.

```
  ServiceType (name, schema, endpoint_roles)
       │
       ├─ PlatformPack (eos / ios-xr / sros / …)
       │     apply_template + optional cleanup
       │
       └─ Service instance  (CN/CI + service_type)
              └─ ServiceEndpoint[]  (role, device, interface, fields)
                     │
                     ▼
              Render  →  Preview (POST /api/config/render)
              Push    →  CLI session on each endpoint device
```

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
   service edit dialog.
5. **Per-service fields.** Anything shared by every endpoint (VRF name,
   RT/RD, numeric service id, MTU, bandwidth). These live on
   `Service.Fields` as `.Fields` in templates. The create wizard and
   service dialog render a form from `schema`. Well-known names are also
   copied to dedicated Service columns for list views and older API
   clients: `bandwidth_mbps` → `Service.BandwidthMbps`,
   `max_mac_addresses` → `Service.MaxMacAddresses`.
6. **What is not a service field.** Device/site-wide knobs (AS number,
   loopback, NTP, default MTU) belong in **config variables** on the scope
   tree, not on the service type. Templates see them as `.Vars`.
7. **Platforms you will push.** Each NOS needs its own pack. `sros-md`
   falls back to a `sros` pack if no dedicated row exists. Huawei `vrp`
   can store a pack and preview, but cannot apply a CLI session yet.

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
the device-sync mapping. ELINE/ELAN/L3VPN have **no platform packs**
besides the seeded ELINE ones until you add them.

---

## 1. Create the service type

**GUI:** Config → Service types → add.

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

## 2. Write a platform pack per NOS

**GUI:** Config → Platform packs → add.

**API:** `POST /api/config/platform-packs`

```json
{
  "service_type_id": 2,
  "platform": "eos",
  "payload_kind": "cli",
  "apply_template": "...",
  "cleanup_template": ""
}
```

| Field | Rule |
| ----- | ---- |
| `service_type_id` | ID of the type, not the name. |
| `platform` | Lower-cased NetBox platform: `eos`, `ios-xr`, `sros`, `sros-md`, `vrp`. Unique per type. |
| `payload_kind` | Default `cli`. `netconf` / `restconf` can be stored and previewed; **push requires `cli`**. |
| `apply_template` | Go `text/template`. Required to render or push. |
| `cleanup_template` | Optional teardown. If empty, a `{{define "cleanup"}}` inside the apply template is used. |

Seeded ELINE packs are refreshed from embed files on migrate **only** when
the stored body still matches `seed_checksum`. Operator edits are left
alone. Your new packs are never overwritten by Seed.

### Template language

`cfgmgmt.Render` parses with `missingkey=error`: a reference to an absent
field fails the preview/push. Guard optional data with `{{if}}`.

Allowed functions (no file, HTTP, or shell):

| Func | Use |
| ---- | --- |
| `join` | `strings.Join` |
| `include` | `{{include "macro-name"}}` — body of a `ConfigMacro`. Nested at most 8 deep. |
| `eq` / `ne` | Equality via `fmt.Sprint`, so `1` and `"1"` compare equal. |

Output is split on newlines; blank and whitespace-only lines are dropped.
Write one CLI command (or MD-CLI line) per line.

**Cleanup contract** (generic push, several endpoints on one device):

1. Cleanup is rendered **once** for the first endpoint, then each
   endpoint's apply body.
2. If the apply template invokes cleanup with
   `{{template "cleanup" .}}`, that invoke is stripped from per-endpoint
   bodies (`RenderPackApplyBody`) so teardown does not run N times.
3. Put shared teardown in `{{define "cleanup"}}` and invoke it at the top
   of apply, **or** put it in `cleanup_template` and leave apply as body
   only.

Idempotent cleanup: `no` / `delete` of objects keyed by `.Name`
(the service ID). Assume the object may not exist yet.

### Generic template context

Packs execute against `cfgmgmt.GenericRenderData`:

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

Non-ELINE packs leave Peer/Remote/SDPID/Stale zero; do not reference them.

### Example: EOS ELAN (VPLS-shaped) body

Sketch only — match your own lab's CLI. Cleanup keyed by service ID:

```
{{define "cleanup"}}
no router bgp vpls {{.Name}}
{{end}}
{{template "cleanup" .}}
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

SR OS packs should be MD-CLI block-paste (see
`internal/drivers/templates/sros_eline.tmpl`): one `/configure … { }`
block, `delete` in cleanup, apply-groups for invariants.

IOS-XR packs should return to `root` the way
`internal/drivers/templates/iosxr_eline.tmpl` does.

### Shared snippets

Config → Macros. A pack can `{{include "eline-defaults"}}`. Macros see the
**same data** as the caller. Use them for repeated banners or apply-group
names, not for per-platform whole services (that is what packs are).

---

## 3. Preview before you push

**GUI:** Config → Preview (device) or render a service.

**API:**

```http
POST /api/config/render
{ "service_id": 123 }
```

or `{ "device_id": 45 }` for that device's baseline templates **plus**
every terminating service.

Each source is `{source, kind, platform, payload_kind, commands[], error}`.
Fix `error` (unknown field, missing pack, template parse) before pushing.

A device render also includes enabled `ConfigTemplate` rows whose platform
matches and whose scope is on the device's ancestor chain. Those are
**baseline/golden config**, not services. Do not put service logic there.

---

## 4. Instantiate, attach endpoints, push

1. **Create** a CN/CI service with this type (wizard or
   `POST /api/service`). Lime-synced rows can have the type attached later
   (`PUT /api/service/:id/type`) without editing Lime-owned fields.
2. **Endpoints** in the service dialog, or:

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

   The whole set is replaced. ELINE also derives `PseudowireID` and, when
   NetBox is configured, reconciles L2VPN + subinterfaces.
3. **Push** `POST /api/service/:id/push` with device credentials
   (`username`, `password`). Per endpoint device:

   - look up the pack for `service_type` + device platform
   - require `payload_kind=cli` and a `CLISessionApplier` driver
     (`eos`, `ios-xr`, `sros` / `sros-md`)
   - render cleanup once, then each endpoint body, one CLI session
   - ELINE: `PrepareELINEApply` (SR OS SDP guard), stamp `AppliedEndpoint*`,
     teardown abandoned devices

   EOS uses the session name as `configure session`; other platforms ignore
   it. Failures are returned per device; there is no automatic rollback of
   siblings that already succeeded.

---

## Config variables vs service fields vs endpoint fields

| Data | Lives on | Template | When to use |
| ---- | -------- | -------- | ----------- |
| Config variable | Scope tree assignment | `.Vars["name"]` | Inherited (global → site → device → interface). Platform-filterable. |
| Service field | `Service.Fields` / type `schema` | `.Fields.name` | One value for the whole instance (VRF, RT, numeric id). |
| Endpoint field | `ServiceEndpoint.Fields` / role `fields` | `.Endpoint.Fields.name` and, for `vlan`, `.LocalVLAN` | Varies per termination. |
| Inventory | Device / interface row | `.Device` / `.Interface` / `.LocalIface` | Already in DCIM; do not duplicate as a field. |

Resolution walks interface scope → device scope → parents → root, then the
variable's default. Required variables with no value fail resolve; render
skips failed vars in `.Vars` rather than aborting the whole device.

Secrets are redacted on read (`***`). A PUT that sends `***` or omits the
value leaves the stored secret unchanged.

---

## ELINE extras

ELINE is a normal cfgmgmt type with extra NetBox and peer-render behaviour:

- Endpoints: `PUT /api/service/:id/endpoints` (roles `a`/`b`).
  `PUT /api/service/:id/eline` is an A/B DTO adapter onto the same helper.
- Push: `POST /api/service/:id/push` (and `/eline/push`, same path).
- Render context: `GenericRenderData` with `.Remote`, `.PeerLocal*`,
  `.SDPID`, `.StaleSubinterfaces` filled from sibling endpoints.
- Packs: seeded from `internal/drivers/templates/{eos,iosxr,sros}_eline.tmpl`.
- Edit the pack in the GUI to tweak CLI; leave `seed_checksum` mismatched
  so migrate will not clobber it. Change the embed file + Seed when the
  default for **new** databases should move.
- Reverse-import: `factum-netbox sync` writes `service_endpoints` for every
  L2VPN whose type matches a service type's `netbox_type` (evpl→ELINE,
  vpls→ELAN), using that type's endpoint roles.

Do not add `EndpointA*` columns for a new type.

---

## Checklist for a new type

- [ ] Topology and roles written down (names, min, max).
- [ ] Per-endpoint fields scalar (`vlan`, `string`, `int`, `ip`). VLAN
      field named `vlan` if templates need `.LocalVLAN`.
- [ ] Per-service data either in `schema` / `Service.Fields` or in config
      variables — not both for the same key.
- [ ] Type created (or built-in roles/schema updated). Confirm it appears
      in the create wizard.
- [ ] One pack per platform you will push; `payload_kind=cli`.
- [ ] `{{define "cleanup"}}` (or `cleanup_template`) keyed by `.Name`,
      safe if the object is absent.
- [ ] Apply body uses `{{if}}` around optional maps; `missingkey=error`.
- [ ] Preview `POST /api/config/render` for each platform with a lab
      device + two endpoints.
- [ ] Push to a lab device; confirm cleanup + re-apply is idempotent.
- [ ] `sros-md` either has its own pack or can inherit `sros`.
- [ ] No new Go types, no new `cmd/`, no ELINE columns.

When the CLI cannot express the service, that is a **driver** gap
(`internal/drivers/README-DRIVERS.md`), not a cfgmgmt one. Packs cannot
reach NETCONF/OpenConfig until `payload_kind` other than `cli` is applied.

---

## API map

| Method | Path | Purpose |
| ------ | ---- | ------- |
| GET/POST | `/api/config/service-types` | List / create type (`sync_source`, `netbox_type`) |
| PUT/DELETE | `/api/config/service-types/:id` | Update / delete (not built-in delete) |
| GET/POST | `/api/config/platform-packs` | List (`?service_type_id=`) / create pack |
| PUT/DELETE | `/api/config/platform-packs/:id` | Update / delete pack |
| GET/POST | `/api/config/macros` | Named `{{include}}` snippets |
| POST | `/api/config/render` | Preview device or service |
| PUT | `/api/service/:id/type` | Set type on an instance (incl. Lime) |
| GET/PUT | `/api/service/:id/endpoints` | Endpoints (including ELINE) |
| POST | `/api/service/:id/push` | Apply CLI |

Write routes need `RequireWrite`. Config seed runs from
`util.MigrateDatabase` → `cfgmgmt.Seed`.
