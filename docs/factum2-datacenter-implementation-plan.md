# Factum2 datacenter GUI implementation plan

Scope: implement (1) a Konva room/floor view, (2) Vue/SVG rack elevations, and (3) a Vue Flow connection view. This is a coding plan, not an implemented change.

## 1. Architecture and repository baseline

Repository inspected: `abundo/factum2`, main tree `357113c61d7fd09d2087cc258b6486c084de3ecf`. Recheck the implementation checkout before making migrations or choosing new filenames.

Verified foundations:

- Vue 3 SPA, Vite, Nuxt UI, Tailwind 4, Vue Router, Pinia and Axios; frontend source is `web/frontend/src/`.
- Go/Echo backend, GORM models in `models/`, explicit goose migrations in `internal/dbmigrate/sql/`. Do not use runtime AutoMigrate or edit the released baseline migration.
- `models/device.go` already defines Device, DeviceType, Interface, Connection and Site.
- Site already represents a hierarchy of NetBox regions/sites/locations and Factum-local sites. Reuse it; do not create a parallel building/location hierarchy.
- Device has a local `DeviceTypeID`, distinct from NetBox `ModelID`. Preserve this distinction.
- Connection already records direct interface-to-interface NetBox cables, with local device/interface foreign keys. Reuse these records for graph edges.
- The inspected Device and DeviceType models do not contain rack placement or physical height/depth fields. Add these deliberately.
- Imported inventory is owned by NetBox. Local objects are supported, and diagram layout can remain local independently of inventory ownership.

Recommended architecture: all three renderers consume explicit backend DTOs using Factum IDs. Rendering libraries never own inventory state. Nuxt UI supplies search, forms, drawers, menus and error messages. Each route loads its drawing library lazily; build output remains embedded in the existing Go release binary.

## 2. Ownership and scope decisions

| Data | Owner and write policy |
|---|---|
| Imported racks, device dimensions and rack positions | NetBox; import and display initially. Reject local API edits to these fields. |
| Factum-created racks and device placements | Factum; editable through validated APIs. |
| Floor geometry, rack drawing coordinates and annotations | Factum; editable even when the depicted rack is imported. |
| Direct cable endpoints | Existing Connection data, synced from NetBox; display only initially. |
| Graph node positions and user view preferences | Factum; never included in inventory sync. |

Dragging a rack on a floor plan changes its drawing position, not its physical site assignment. Dragging a device inside a rack changes inventory placement and follows ownership rules.

First release includes all three views, with local inventory editing and read-only imported placement/cabling. NetBox write-back, cable creation, patch-panel tracing, 3D, and automatic discovery are follow-up features. Disabled imported editing must show a useful explanation and an existing NetBox link where available.

## 3. Proposed data model

Names below are proposals; follow current model/DTO naming conventions during implementation.

| Model | Fields / relationships |
|---|---|
| Rack (new) | FactumModel, local site-node FK, name, source, NetBox ID, height in U, width/depth in mm, starting unit, numbering direction, version. |
| DeviceType (extend) | Nullable physical height, nullable full-depth flag, optional front/rear image references. Keep unknown values distinct from zero/false. |
| DevicePlacement (new) | Unique local device FK, local rack FK, physical offset from rack bottom, front/rear face, source, version. One authoritative placement per physical device. |
| FloorPlan (new) | Local site-node FK, name, width/height in mm, grid size in mm, revision. Multiple plans may reference the same site if useful. |
| FloorPlanRack (new) | Floor-plan FK, rack FK, x/y in mm, rotation in 90-degree increments; unique rack per plan. |
| FloorPlanAnnotation (new) | Floor-plan FK, bounded kind such as label/aisle/zone, geometry and text. |
| ConnectionViewLayout (new) | Scope and owning user, revision, versioned JSON of node coordinates keyed by stable Factum node IDs. Never store cable inventory in this JSON. |

Use integer half-U ticks internally where fractional device heights must be represented: two ticks equal 1U. Keep display numbering separate from physical offset so descending labels do not invert placement math. For the initial local editor, support full-width devices anchored on whole-U boundaries; display imported fractional placements faithfully. Zero-U devices belong in a separate rack-accessory list. Shelves, half-width carriers, chassis bays and depth-specific clearance beyond the full-depth flag need an explicit later model, not invented free U space.

Treat missing height as unknown: show an unplaced/needs-data entry and do not silently assume 1U. Reject placing VMs. Do not infer rack placement by parsing free-text `CfLocation`.

Resolve racks to Site through explicit local IDs and NetBox `(kind, id)` mappings. Audit existing `Device.SiteID` semantics before joining it to local Site IDs; do not assume IDs from the two systems are interchangeable.

## 4. Backend and API contracts

Implement domain operations in a proposed `internal/dcim/` package, keeping Echo handlers thin. Continue existing `RequireAPIAuth`, `RequireRead`, `RequireWrite` and applicable scope checks. Use explicit DTOs and field allowlists so generic updates cannot bypass ownership or placement checks.

Proposed endpoints; align final route names with `web/web.go`:

| Endpoint | Purpose |
|---|---|
| GET /api/dcim/racks?site_id=... | Bounded rack listing with occupancy summary. |
| POST /api/dcim/racks | Create a local rack. |
| PUT /api/dcim/racks/:id | Update allowed local rack properties with expected version. |
| DELETE /api/dcim/racks/:id | Delete an empty local rack; explicitly handle diagram references. |
| GET /api/dcim/racks/:id/elevation | Rack metadata, placements, device summaries, dimensions, ownership and data-quality issues. |
| PUT /api/dcim/devices/:id/placement | Place or move a local device atomically. |
| DELETE /api/dcim/devices/:id/placement | Unmount a local device without deleting its inventory record. |
| GET/POST /api/dcim/floor-plans | List/create plans, scoped to a local Site ID. |
| GET /api/dcim/floor-plans/:id | Complete bounded plan snapshot and rack summaries. |
| PUT /api/dcim/floor-plans/:id/layout | Save geometry changes atomically with expected revision. |
| GET /api/dcim/connections/graph | Bounded graph by site, rack or seed device; configurable capped neighbor depth. |
| GET/PUT /api/dcim/connection-view-layouts/:scope | Load/save the current user's graph layout with expected revision. |

Use existing response/error conventions. Define validation errors with machine-readable reasons such as overlap, out_of_bounds, imported_read_only, stale_version and unknown_dimensions. Stale revisions and concurrent placement conflicts return 409; malformed input and prohibited writes follow existing API status conventions.

Placement transaction:

1. Lock the device and affected rack rows in a consistent documented order.
2. Re-read placement/version and ownership after taking locks; all local placement writers and sync writers must follow the same coordination contract.
3. Validate device type, site compatibility, rack bounds and affected occupancy intervals.
4. A full-depth device blocks both faces. Shallow devices conflict on their own face. Compute occupied U using interval unions; do not double-count a full-depth device when reporting whole-rack utilization.
5. Commit placement and version changes atomically. Moving between racks cannot leave two placements or remove the old one on failure.
6. Return the authoritative placement and refreshed summary.

Apply the same checks when changing a rack's height or a device type's physical size, not only on drag/drop. For cross-site moves, require a separate explicit inventory operation rather than quietly changing the device's site.

Floor-plan saves validate finite coordinates, object limits, rotated footprints, room boundaries and rack footprint collisions. Labels and zones may overlap; rack footprints may not. Grid snapping occurs in world coordinates. Rack dimensions are inventory metadata and cannot be resized by manipulating a canvas rectangle.

## 5. NetBox import extension

Inspect `internal/netbox` and `internal/netboxtool` before extending them. If required fields are absent, add them to the in-tree NetBox client in `internal/netboxtool`.

Import order: site hierarchy, device types and physical properties, racks, devices/placements, then existing connections. Use existing source IDs and idempotent upserts. Ensure device upserts do not erase new local-only fields or layout records.

A failed or partial fetch must never be interpreted as upstream deletion. Reconcile missing imported racks/placements only after a successful complete fetch for the relevant scope. Preserve local racks and layouts. Show unavailable imported references as unresolved entries until reconciliation handles them explicitly.

If upstream occupancy is inconsistent, preserve and flag the imported truth rather than silently moving devices or dropping conflicting rows. All local placement writes must treat those occupied areas conservatively until resolved.

Acceptance: repeat sync produces no duplicates; imported placement changes appear in elevation; floor coordinates and graph layouts survive sync unchanged; no writes are made to NetBox in this release.

## 6. Frontend implementation

Use existing JavaScript conventions. Add `konva`, `vue-konva` for the floor route and `@vue-flow/core` plus only the optional Vue Flow control packages actually used. No new framework or standalone service is needed.

Proposed files beneath `web/frontend/src/`:

| Path | Responsibility |
|---|---|
| api/racks.js, floorPlans.js, connections.js | Existing Axios wrapper and endpoint contracts. |
| views/dcim/FloorPlanPage.vue | Route, loading, toolbar, selection drawer and edit/save/cancel. |
| views/dcim/RackPage.vue | Elevation route, front/rear toggle, unplaced devices and forms. |
| views/dcim/ConnectionsPage.vue | Graph filters, selected connection details and layout persistence. |
| components/dcim/FloorPlanCanvas.vue | Konva stage/layers, pointer-to-world coordinates and events. |
| components/dcim/RackFootprint.vue | Rack footprint, label and occupancy/status indicator. |
| components/dcim/RackElevation.vue | SVG U grid and device rendering. |
| components/dcim/RackDevice.vue | Device shape/image, label and interaction events. |
| components/dcim/ConnectionDeviceNode.vue | Vue Flow node with stable interface handles. |
| composables/useFloorPlanEditor.js | Draft state, snapping, undo/redo and save conflict handling. |
| utils/dcimGeometry.js | Pure rack/floor coordinate helpers and preview validation. |

Keep transient selection/hover state local. Use Pinia only when a draft must survive route changes or be shared across components; do not create a second inventory cache without a clear need.

### View 1: room/floor plan with Konva

- Add routes such as `/dcim/floor-plans/:id` and navigation from the existing Site page.
- Draw a scaled room boundary, grid, rack footprints, labels and simple annotations.
- Start with pan, wheel zoom, fit-to-content, selection, rack search and a Nuxt UI details drawer.
- Then add explicit edit mode, drag from a rack picker, 90-degree rotation, grid snapping and invalid-position preview.
- Represent rack position in mm with a defined origin and rotation anchor. Camera zoom/pan must not affect persisted dimensions.
- Keep a local draft with Save/Cancel and session undo/redo. Save only explicit edits, not every pointer move. Warn before discarding an unsaved draft.
- Navigate from a rack to its elevation. Occupancy colours include a legend; unknown data is visibly distinct from an empty rack.
- Add a table/form alternative for keyboard use and precise numeric coordinates.

Acceptance: a rack moved while zoomed is saved at the correct world location, persists after reload, and remains selected where practical. A rejected or stale save preserves the user's draft and offers reload/reapply; it never overwrites another user's layout silently.

### View 2: rack elevations with Vue/SVG

- Build a read-only SVG component first: numbered rails, configurable height, empty units, multi-U devices, front/rear faces and selected-device highlighting.
- Derive positions from physical offsets and sizes, not array order. Use a shared scale and responsive SVG viewBox.
- Prefer clear labelled rectangles initially. Later add optional device-type front/rear images, always retaining a text fallback.
- Add device detail links using the existing DeviceList navigation/selection convention; inspect that convention before inventing a device-detail URL.
- Add local placement editing via a form, followed by drag/drop with snapped ghost preview and collision feedback. Keep physical device size fixed.
- Imported placements remain visible and read-only. Unplaced devices and zero-U accessories have separate lists.
- Submit one API operation after the drop. On conflict, restore authoritative placement and explain the reason.

Acceptance: 1U/2U/4U and fractional imported heights render correctly; numbering is correct in both directions; full-depth occupancy appears on both views; moving/unmounting never deletes a device; all edits remain usable without drag gestures.

### View 3: connections with Vue Flow

- Reuse `models.Connection` as graph edges and existing Device/Interface records as nodes and ports.
- Key nodes by local device ID and handles by local interface ID; labels can change without breaking links. Key edges by local Connection ID.
- Provide site/rack filters and device-neighborhood mode. Keep parallel cables distinct and label both endpoints.
- For cables crossing the selected scope, return labelled external-device stubs so links do not simply disappear.
- Add selection, fit-to-view, pan/zoom, port highlighting and a details drawer with existing inventory links.
- Start with deterministic site/rack grouping and manual positioning. Add an automatic layout dependency only if actual graph density warrants it.
- Persist node coordinates per user/scope; merge saved coordinates with newly discovered nodes, and ignore stale IDs safely. Moving graph nodes never moves devices physically.
- Disable cable-creation/reconnection handles. Viewing/editing layout must not create or modify cables.

The current Connection model represents direct interface-to-interface cables only. Label this scope in the UI; do not claim end-to-end tracing through patch panels, power connections, or service paths. Add explicit typed terminations and path traversal later if required.

Acceptance: multiple parallel links, external neighbors and unavailable endpoints display clearly; refreshing the graph preserves saved node positions; no endpoint or cable data changes when rearranging the graph.

## 7. Delivery sequence and reviewable changes

Implement the shared foundation first, then rack elevations before the floor plan because floor interactions depend on reliable rack data.

| Change | Deliverable | Completion gate |
|---|---|---|
| 1. Domain contract and migration | Models, DTOs, ownership policy, placement service, migrations and focused tests. | Fresh/existing DB migration works; invalid and concurrent placement writes are rejected. |
| 2. Import and read APIs | Rack/physical-property sync, elevation DTO, floor summaries and graph projection. | Repeated/failed sync cases and ID mapping verified against fixtures. |
| 3. Rack viewer | SVG elevation, front/rear toggle, links and missing-data states. | Representative rack fixtures render correctly. |
| 4. Rack editor | Local create/update/place/unmount, forms and drag preview. | Ownership, overlap and concurrent-editor cases pass. |
| 5. Floor viewer/editor | Konva scene, rack picker, geometry validation, draft/save/undo. | Reload, rotated snapping, save conflict and keyboard workflow verified. |
| 6. Connection view | Vue Flow graph, filters, direct cable detail and saved layouts. | Parallel/cross-scope links and layout persistence verified. |
| 7. Integration and documentation | Site/DCIM navigation, lazy loading, release embedding, operator docs. | Existing build gates pass and all three views work in the isolated dev lab. |

Each change should be independently reviewable. Keep new routes hidden until their backing APIs are usable. Add image support and polished exports after the core three views; these do not block their delivery.

## 8. Verification and performance

Follow AGENTS.md: use the isolated `factum2-dev` compose lab / repository skill when implementing. Never verify against the installed live instance on port 8090 or its production database.

Meaningful tests:

- Go unit tests for occupancy intervals, reversed numbering, unknown dimensions, zero-U handling, source ownership and site mapping.
- PostgreSQL integration tests for simultaneous placement writes, atomic moves, revision conflicts, rack shrink/device-type resize, and sync-versus-edit behavior.
- API tests for read/write permissions, narrow DTOs, scope filtering, graph endpoint identity and incomplete imports.
- Small frontend geometry tests for zoomed pointer conversion, rotation and snapping. Add a frontend test runner only if not already present and needed for these helpers.
- Browser journeys: create a local rack, place/unmount a device, save/reload floor layout, reject an imported move, navigate floor → rack → connection, and handle a stale save.

Suggested initial performance fixtures: a floor with 200 racks, several populated 42U racks, and a bounded graph of 200 devices/500 cables. Measure fit/pan/selection on an agreed development machine before setting numerical budgets. Scope and cap API results, display truncation explicitly, fetch room data in batches, and avoid one request per rack or device. Loading/error/empty/unknown states must exist in all three views.

Run the repository's applicable Go tests/build/vet and frontend lint/build gates. Verify the actual release build embeds the new lazy-loaded assets. Keep operator docs in `docs/user/` and developer details in DEV.md or a linked developer document.

## 9. Future extensions

- NetBox write-back for imported placements: explicit write policy, upstream error handling and reconciliation; do not attempt a distributed transaction between Postgres and NetBox.
- Patch-panel and power tracing: typed endpoints, front/rear port mappings, path traversal and cycle handling.
- Device artwork from the NetBox device-type library, cached/served by the backend with appropriate attribution and safe asset handling.
- 3D with TresJS only if a concrete operational use case emerges.
- Shared named graph layouts or planning scenarios, separate from per-user positions and authoritative inventory.

## References

- [Factum2 DEV.md](https://github.com/abundo/factum2/blob/main/DEV.md)
- [Factum2 AGENTS.md](https://github.com/abundo/factum2/blob/main/AGENTS.md)
- [Existing inventory models](https://github.com/abundo/factum2/blob/main/models/device.go)
- [Vue Konva](https://konvajs.org/docs/vue/index.html)
- [Vue Flow](https://vueflow.dev/)
- [Rackula SVG implementation reference](https://github.com/RackulaLives/Rackula/blob/main/docs/ARCHITECTURE.md)
- [NetBox device-type definitions and images](https://github.com/netbox-community/devicetype-library)
