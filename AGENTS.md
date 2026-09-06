# AGENTS.md

Project instructions and architecture for agents working in this repository.
Grok auto-loads this file.

See [DEV.md](DEV.md) for setup, build, and run instructions (config, database,
Makefile targets, dev workflow for the web GUI + frontend, binaries list,
worker hub transport). This file covers architecture, conventions, and
environment gotchas that aren't obvious from a single file.

Test coverage is partial: `web/` (handlers, auth, generic CRUD) and
`internal/drivers` (Arista EOS, against a fake eAPI server) have tests, most
packages have none. `internal/drivers` also has a containerlab-backed
integration tier behind the `integration` build tag — see DEV.md's Testing
section before adding tests there.

## Working in this repo

- **Do not use the live instance.** Port `:8090` is the installed
  `/opt/factum2/factum2-web` process reading `/etc/factum2/factum2.yaml` and
  the real `factum2` Postgres database. Do not log into it, seed data into
  it, or point a Vite proxy at it to "verify" a change.
- **UI verification:** use the `run-factum2-web` skill
  (`.grok/skills/run-factum2-web`). It provisions an isolated
  `factum2_skilltest` DB and serves the GUI on `127.0.0.1:18090`.
- **Postgres is Docker**, not a local `postgres` OS user. Container name
  and superuser are local environment (the `run-factum2-web` skill defaults
  to `postgresql-db-1` / `factum2_user`, overridable via
  `FACTUM_PG_CONTAINER` / `FACTUM_PG_SUPERUSER`). The default DB `factum2`
  is the real one — do not point tests or the skill at it. Credentials are
  not stored in this repo; `docker exec` talks to Postgres inside the
  container. Connect with
  `docker exec [-i] <postgres-container> psql -U <superuser> -d <db>`.
  Pass `-i` whenever piping a heredoc into `psql` — without it stdin isn't
  attached and `psql` silently succeeds while doing nothing.
- **CLI flags:** config file is `-f` (default `/etc/factum2/factum2.yaml`;
  worker default `/etc/factum2/factum2-worker.yaml`); web subcommand is
  `start`. Schema changes are `factum2-web migrate` (or `factum2 migrate`) —
  start/createadmin/sync do not AutoMigrate. `web.GuiParams.Bind` defaults
  to `:8090` and overrides YAML `web.bind` — always pass `-b` for an
  isolated instance or it collides with the live process.
- **Browser:** `chromium-cli` is not installed. The skill drives
  `playwright-core` against `/usr/bin/google-chrome-stable` with
  `--no-sandbox`.
- **Device SSH:** reuse one SSH connection per device for the process
  lifetime; do not reconnect per command.
- **NetBox L2VPN on SROS/SROS-MD:** the termination interface label is
  `SAP`, not a subinterface name.
- Frontend stack is Vue 3 + Vite + Vue Router + Pinia + **Nuxt UI** +
  Axios + Tailwind CSS 4 + MapLibre/deck.gl (not PrimeVue). Device drivers
  also include Huawei VRP and Open ROADM MSA — see `internal/drivers/README-DRIVERS.md`.

## Quick commands

```sh
go build ./...                       # compile everything
go vet ./...                         # static checks
cd web/frontend && npm run lint      # oxlint --fix, then eslint --fix
cd web/frontend && npm run format    # prettier --write src/
```

Full build/run commands (Makefile targets, dev server workflow, config file
shape) are in [DEV.md](DEV.md) — don't duplicate them here.

## Architecture

Factum tracks network infrastructure (devices, customers, services) and
syncs it with external systems of record. It's a Go monorepo producing
several single-purpose CLI binaries (`cmd/*`) that share `internal/` and
`models/` packages, plus a web GUI (`factum2-web`) with a Vue 3 SPA frontend
(`web/frontend`).

### Sync model: external systems -> factum -> DNS/monitoring

Factum is a hub, not a source of truth for everything: NetBox and Lime CRM
are upstream sources synced _into_ factum's Postgres DB (`factum2-netbox
sync`, `factum2-lime sync` — see `internal/netbox`, `internal/lime`), while
DNS, Icinga, LibreNMS, Oxidized and Prometheus are downstream targets
synced _from_ factum (`factum2-dns sync`, `factum2-icinga sync`,
`factum2-librenms-cli sync`, `factum2-oxidized sync`, `factum2-prometheus
sync`) — same shape as Icinga: `internal/oxidized`'s
`FactumOxidizedClient.Sync` filters devices (enabled, `CfBackupOxidized`,
not on any `Settings.OxidizedIgnore*` list, has a primary IPv4), writes
oxidized's router.db (`Settings.OxidizedDestFile`, `name:ip:model` per
line; Oxidized CSV must map `name: 0`, `ip: 1`, `model: 2`), and asks
oxidized to reload only if the file's content actually changed. Prometheus
(`internal/prometheus`) writes a file_sd JSON of snmp_exporter targets
(`Settings.PrometheusDestFile`) from devices with `CfMonitorGrafana` and
POSTs `Settings.PrometheusReloadURL` only if the file changed. BECS is an upstream
source like Netbox: `factum2-becs sync` (`internal/becs`) writes ibos
elements into Netbox (matched by custom field `becs_oid`) then calls
`netbox.Sync` so factum sees the result. The Netbox write path is its own
reconciler; `internal/netbox`'s helpers write to factum keyed by
`netbox_id` and are not reused for the BECS→Netbox half.

Netbox's and Lime's own settings aren't in the YAML config at all — they
live in the `Settings` DB row (`util.GetOrCreateSettings`,
`internal/util/settings.go`), editable from the admin UI. Their client
constructors (`cmd/netbox`'s `newNetbox`, `internal/netbox.FactumSyncNetbox`;
`internal/lime.Lime.SyncCustomers`, which builds `lime.Limetool` only after
`lime.DB` is connected, since unlike Netbox it doesn't get a `*gorm.DB` at
construction time) connect directly to the DB and call
`util.GetOrCreateSettings` — this is safe for them because, unlike
DNS/Icinga/LibreNMS/Oxidized below, nothing about Netbox or Lime sync is
meant to run off the primary host.

**Capacity service types (cfgmgmt):** CN/CI types (ELINE, ELAN, L3VPN, …)
are a `ServiceType` + per-NOS CLI objects in the DB, not a new Go
package. Endpoints live in `service_endpoints`. Each type can carry
`sync_source` / `netbox_type` so device-sync and NetBox reverse-import
are not ELINE-hardcoded. The Config **tree** holds folders, attached
devices (detach never deletes DCIM), parameter objects, CLI objects, and
service objects (views onto `models.Service`, virtual refs on ports).
Translation CLI lives under `_catalog/cli/<type>/<platform>`; baseline
CLI is a child of `global` or a site/device, never `_catalog`. How to
design one:
[docs/cfgmgmt-service-design.md](docs/cfgmgmt-service-design.md).

**L2VPN path (device → Netbox → factum Service):**
`factum2-device-sync` writes on-device services using cfgmgmt mappings
(ELINE→EVPL L2VPN, ELAN→VPLS, L3VPN→VRF) plus terminations. `factum2-netbox
sync` then reverse-imports L2VPNs onto matching factum `Service` rows
(`internal/netbox.syncServiceEndpointsFromL2VPNs`): match by
`Service.L2VPNNetboxID` or `Service.ServiceID == L2VPN.Name`, resolve
terminations to physical ports + VLAN/subinterface, set `ServiceType`
from the mapping (ELINE/ELAN/…) and `service_endpoints` (roles from the
type). Does not create Service rows (Lime/manual still own that). Skips
services whose `ServiceType` is already set to something other than the
mapped type or empty.

DNS, Icinga, LibreNMS, Oxidized and Prometheus are different: their CLI tools
(`factum2-dns`, `factum2-icinga`, `factum2-librenms-cli`, `factum2-oxidized`,
`factum2-prometheus`) are meant to run on a _different host_ than the primary
(the DNS/Icinga/LibreNMS/Oxidized/Prometheus server itself), so they can't just
open a direct Postgres
connection the way Netbox/Lime do. They still talk to the primary's existing
REST handlers (same paths and JSON; see "Service-to-service auth" below),
but the transport is the hub unix socket when a co-located `factum2-worker
start` is running. HTTPS (`factum.url` + bearer `ConfigFactum.Token`) is
only a probe-time fallback — see `util.FactumHTTP` and "Worker / hub
transport" below. Worker networks do not need a route to the primary's
HTTPS port once this stack is live and socket ACLs cover mixed-UID CLIs
(group `factum` + the icinga/nagios user). The primary still serves HTTPS
to operators and NetBox's webhook; closing worker-net `:443` is an operator
firewall step, not something the process unbinds.

- **Generic client-side fetch**: `util.FetchRemoteConfig[T]`
  (`internal/util/remoteconfig.go`) GETs a path and unmarshals JSON.
  Transport is `util.FactumHTTP`: Stat/Dial the unix socket
  (`/run/factum2-worker/api.sock`, overridable via `FACTUM_WORKER_API_SOCKET`
  / `factum.socket`), and use it when that probe succeeds; otherwise HTTPS
  to `factum.url` with `Authorization: Bearer {token}`. After a successful
  unix Dial, 502 / timeout / hub-disconnected are **not** retried over
  HTTPS (`:443` may already be closed). `FACTUM_WORKER_API_SOCKET=none` (or
  `0`) forces HTTPS even if the socket exists. Each package wraps
  `FetchRemoteConfig` in its own helper returning its own config type -
  `internal/dns/remote_config.go`, `internal/icinga/remote_config.go`,
  `internal/librenms/remote_config.go`, `internal/oxidized/remote_config.go`,
  `internal/prometheus/remote_config.go`.
  `internal/icinga`/`internal/librenms` also expose a `RemoteClient`
  convenience (`FetchRemoteConfig` + constructing the API client in one
  call). `NewFactumIcingaClient`/`NewFactumLibrenmsClient`/`dns.NewDNSClient`/
  `NewFactumPrometheusClient` all fetch eagerly at construction time (not
  lazily inside `Sync()`/`Update()`), and now return an error since the
  fetch can fail.
- **Server side**: one `GET /api/<service>-config` route per service
  (`web/handle_dns.go`, `web/handle_icinga.go`, `web/handle_librenms.go`,
  `web/handle_oxidized.go`, `web/handle_prometheus.go`), each reading the
  relevant `Settings` fields and registered in `web.go` with
  `RequireAPIAuth, RequireAdminOrServiceToken`
  (not under `adminApi`: plain `RequireAdmin` 401s a service-token caller
  outright, since there's no "user" in context for it to check a role on).
  The GUI Oxidized browser (`/oxidized`) is a separate set of
  `RequireRead` routes on the same handler file
  (`/api/oxidized/nodes`, `/api/oxidized/node/config|versions|version|diff`)
  that proxy oxidized-web through `internal/oxidized`'s REST methods.
  `Settings.OxidizedApiURL` must be reachable from factum-web for that
  page. Deleted devices are not in the API: `/nodes` is the current
  source list, and version/fetch/diff look the node up there first.
- **None of these have local YAML config** (`util.ConfigIcinga`,
  `util.ConfigDNS`, `util.ConfigOxidized`, `util.ConfigLibrenms`,
  `util.ConfigPrometheus` are all runtime-only DTOs, not part of
  `ConfigRoot` at all - `ConfigRoot.Icinga`/`.Defaultdomain`/`.Librenms`
  were removed once each was wired up).
  LibreNMS's Sync regex-filter lists (`RolesEnabled`/`InterfacesDisabled`,
  plus the still-unused `PersistentDevices` - see the delete-flow note
  below) used to be the one exception, YAML-only with no DB-backed
  equivalent; they're now plain `Settings.LibrenmsRolesEnabled`/
  `LibrenmsInterfacesDisabled`/`LibrenmsPersistentDevices` text columns
  (newline-separated, same convention as DNS/Oxidized's `Ignore*` fields),
  edited on the LibreNMS admin tab like everything else here. The map shape
  they had in the old YAML (`name: regex`) is gone - the key was never read,
  only the regex value, so `FactumLibrenmsClient.syncInterfaces` now compiles
  each non-empty line instead of each map value.
- **`util.CommonConfig`** (`internal/util/remoteconfig.go`) holds the subset
  of settings shared by _every_ remote-config-fetching tool, not tied to one
  service - currently just `DefaultDomain` (`Settings.DefaultDomain`, edited
  in the admin UI's Factum tab, not the DNS tab - it's also needed by
  Icinga/LibreNMS/Oxidized/Prometheus sync, matching factum device names against
  fully-qualified DNS names). `util.NewCommonConfig(settings)` builds it;
  every `web.ApiXxxConfig` handler embeds it in its response via that
  helper, and every `util.ConfigXxx` runtime type embeds it too. A tool
  that needs nothing service-specific can
  fetch just this from `GET /api/common-config` (`web.ApiCommonConfig`) —
  `drivers.NewDriverName` does. A `factum2-worker start` host only needs
  `worker.listen`/`.token`/`.tls_cert`/`.tls_key`/`.commands` (all local - see "Worker / hub
  transport" below). `factum.url`/`.token` are Stat/Dial fallback for
  co-located CLIs and may be omitted from start-only YAML; they remain
  required for `factum2-worker run` (`POST /api/worker/run` stays on HTTPS,
  not the hub RPC) and for any CLI that cannot open the socket.
- DNS's `Settings.DnsDestFile` and `IgnoreModels`/`IgnorePlatforms`
  (newline-separated text) are carried over as-is from the old YAML config -
  `internal/dns.Sync` doesn't actually filter on the ignore fields, never
  did even back when they were a YAML map.
- LibreNMS's own MySQL credentials are _not_ part of any of this - they're
  not in `Settings` or fetched over REST at all. `factum2-librenms-cli`
  assumes it runs co-located with the LibreNMS server, and reads them
  directly from LibreNMS's own `.env` file on disk
  (`NewFactumLibrenmsClient` in `internal/librenms/factum2-librenms.go`,
  trying `/opt/librenms/.env` then `/opt/librenms-docker/.env`), wiring the
  result into `LibrenmsClient.DBConfig` for `PortsGet`/`PortsUpdateIgnore`.
- **Triggering a sync**: in production the trigger is always a cron job on
  the primary itself, not a human or a scheduler on the worker/target host -
  `factum2-dns`/`factum2-icinga`/`factum2-librenms-cli`/`factum2-oxidized`/
  `factum2-prometheus` are never invoked by their own local cron, only ever
  dispatched by the primary
  over the hub. The Job overview page
  (`web/frontend/src/views/sync/SyncOverviewPage.vue`) exposes the same path
  manually, with one button per `worker.SyncTargets` entry; clicking one
  calls `POST /api/sync/:target`
  (`web.ApiSyncTrigger`, `web/handle_sync.go`), which calls
  `ctrl.RemoteManager.StartJob("sync", requestedBy, []string{target})`
  (`internal/worker/hub.go`) - this side needed no changes to add a new
  target's button, it already handles all of `SyncTargets` generically. The
  "Sync all" button instead calls `POST /api/sync/all`
  (`web.ApiSyncTriggerAll`), which orders every enabled target sources-
  first via `worker.SequencedSyncAllTargets` and calls the same `StartJob`
  with all of them - one `Job` row dispatching a `JobTask` per target, one
  target at a time, each waited on to finish before the next starts
  (`RemoteManager.dispatchRemainingSequentially`) - a destination sync
  (DNS/Icinga/LibreNMS/Oxidized/Prometheus) must never run while, or before, the
  source sync (NetBox/Lime/BECS) that's supposed to feed it is still
  updating factum's own data. The parent `Job`'s `ExpectedTasks` (set once
  at creation) is what lets `resolveTask` tell "batch truly finished" apart
  from "only the first target's `JobTask` row exists so far" - see "Jobs
  and job tasks" below. The other half - actually running the sync
  on whichever host runs that target's CLI - goes through
  `factum2-worker`'s agent commands (`internal/worker`), reusing their
  existing predefined-command mechanism rather than each service CLI
  having its own bespoke listener: an agent activates a command by name
  via `worker.commands` (e.g. a `worker.commands.librenms` entry running
  `factum2-librenms-cli sync`), and `StartJob` dispatches a `command`
  envelope per task to exactly one connected node whose hello-reported
  roles include the target name (deliberately _not_ a fan-out to every
  matching node the way `factum2-worker run`'s `SendCommand`/`RunAndWait`
  still is - see "Jobs and job tasks" below for why). A sync trigger still
  can't run anything outside the agent's own `worker.commands` allowlist,
  same security property as before. Unlike the old rabbitmq-based
  `PublishSync`, which silently "succeeded" even with nobody listening,
  the dispatch's matched-node count is known immediately, so
  `ApiSyncTrigger`/`ApiSyncTriggerAll` return a real error (or, for a
  batch, record it against that one target's task) if no connected node
  handles the target - the `Job`/`JobTask` rows are still created and
  immediately marked failed either way, so they show up in job history
  rather than just vanishing.
- **Which worker nodes are up**: the Job overview page's "Troubleshooting"
  section links to `/sync/status` (`JobStatusPage.vue`), which reads
  `GET /api/worker/status` (`web.ApiWorkerStatus`, `web/handle_worker.go`) -
  a plain merge of the DB's configured `models.WorkerNode` rows with
  `RemoteManager.StatusAll()`'s live connection state. Unlike the old
  rabbitmq-based ping/pong (a fanout broadcast with a fixed timeout window,
  answered by _every_ running `factum2-worker start` instance regardless of
  `worker.commands`), this is a passive read of state `RemoteManager` already
  knows - it's always either connected or actively retrying every
  configured node (see "Worker / hub transport" below), so there's no
  broadcast round trip and no "did we wait long enough" timeout to tune.
- **Jobs and job tasks**: `models.Job`/`JobTask`/`JobTaskEvent` persist a
  triggered sync's outcome for the "Recent jobs" table on `/sync/status`
  (`JobStatusPage.vue`, via `GET /api/jobs`/`GET
/api/jobs/:id/tasks/:taskid/events`, `web/handle_sync.go`). A `Job` is
  one triggered unit of work (a single-target trigger, or one "sync all"),
  made up of one or more `JobTask`s - one per target - so there's no
  separate "job" concept per target the way the old `SyncJob` model had;
  `Job.Type` is `"sync"` today but exists so a future non-sync job kind can
  reuse the same table. `JobTask.TaskID` is the same ID already generated
  for wire-protocol correlation, not a second ID space, but is internal
  (`json:"-"`) - the REST API addresses tasks by their ordinary numeric ID
  like every other resource. Completion is detected via the existing
  `StreamExit` `LogMsg` (`RemoteManager.finishJobTask`/`resolveTask`), not
  a new signal; a `Job`'s `FinishedAt` is stamped, event-driven, the moment
  its last sibling `JobTask` finishes - not computed at read time, since
  `GET /api/jobs` is polled every few seconds by the overview page. "Last
  sibling" can't be "no unfinished `JobTask` rows exist" any more now that
  a batch's rows are created one at a time as each target's turn comes up
  (`StartJob`'s sequential dispatch, above) rather than all up front -
  early in a batch there may be zero unfinished rows simply because most
  targets' rows don't exist _yet_. `resolveTask` instead compares the
  finished row count against `Job.ExpectedTasks` (the target count, stamped
  once at job creation, `json:"-"` like `TaskID`) to tell the two apart.
  `JobTask.ErrorCount`/
  `WarningCount` are denormalized counters incremented alongside each
  `JobTaskEvent` insert (`RemoteManager`'s `EnvelopeEvent` handling), so
  the job list can show error/warning counts without a live `COUNT(*)`
  over events on every poll. Structured per-line events are opt-in, not
  automatic: each of the sync tools'
  CLI `sync` subcommands (`becs`, `dns`, `icinga`, `librenms`, `netbox`,
  `lime`, `oxidized`, `device-sync`) takes a `--job` flag that switches its
  `internal/jobevent.Reporter` from human-readable console output
  (`ConsoleReporter`, the default - unaffected if you run the binary by
  hand) to JSON lines on stdout (`StdoutReporter`) - an operator opts a
  `worker.commands` entry in by adding `--job` to its `args`, the same way
  any other flag would be added; nothing is force-injected by the agent
  (unlike `worker.commands` entries running arbitrary shell commands, an
  auto-injected flag would break anything that isn't one of those
  tools). The agent (`hub_agent.go`'s `streamOutput`) sniffs each
  **stdout** line (never stderr) for `{"level":"info"/"warning"/"error",
"message":...}` and relays it as an `event` envelope instead of a plain
  `log` line only if the decoded `level` is exactly one of those three
  values - a bare JSON-parse success isn't enough, since some tools already
  print well-formed-but-unrelated JSON (`internal/icinga/icinga.go`
  dumping a request body) that would otherwise silently decode into a
  blank event and swallow the real line.
- **Housekeeping**: `job_task_events` (and the parent `jobs`/`job_tasks`
  rows) are not pruned automatically. `housekeeping` is a job target
  (`worker.HousekeepingTarget`) that runs **in-process on the primary**
  (`RemoteManager.createAndDispatchTask` / `runHousekeeping`) against
  Postgres — it is not dispatched over the hub and does not need a
  `worker.commands` entry. `internal/housekeeping.Trim` keeps the newest
  `Settings.JobHistoryKeep` finished jobs (default 50 when unset/`< 1`,
  matching `GET /api/jobs`) plus any unfinished jobs, and deletes older
  finished jobs with their tasks and events, plus orphan events
  (`job_task_id IS NULL`). It is a valid scheduler / `POST /api/sync/:target`
  target (`worker.IsValidJobTarget`) but is **not** in `SyncTargets`, so
  "Sync all" and schedule target `"all"` never include it. Nothing starts
  it on its own; an operator creates a `JobSchedule` (or clicks Run on
  the Job overview Maintenance tile).
- `Sync()` in `internal/librenms/factum2-librenms.go` gets its Netbox client
  the same way as everything else on this page: `internal/netbox`'s
  `FetchRemoteConfig`/`RemoteClient` fetch `Settings.NetboxApiURL/
NetboxApiToken` from the primary (`GET /api/netbox-config`,
  `web.ApiNetboxConfig`, `util.ConfigNetbox`) via `util.FactumHTTP` rather
  than opening a direct Postgres connection — `factum2-librenms-cli` runs on
  the LibreNMS host, not the primary, and has no access to its Postgres DB.
  LibreNMS's own REST/MySQL and the NetBox API after those credentials are
  fetched stay local / on NetBox's URL; they are not tunneled.

`internal/drivers` talks to network devices directly (Arista EOS, Cisco
IOS-XR, Nokia SR OS, Huawei VRP, Open ROADM MSA): NETCONF against OpenConfig
models for interface state/config on EOS/IOS-XR/SR OS (shared plumbing in
`internal/drivers/openconfig.go`), plus a per-platform CLI-shaped transport
for what NETCONF can't express (arbitrary commands, `show running-config`,
config save). That second transport is Arista's eAPI JSON-RPC for EOS
(`internal/drivers/eapi.go`, which also serves as EOS's fallback when its
NETCONF agent isn't enabled - it's off by default) and SSH CLI screen-
scraping (`sshRunCLI`) for IOS-XR, SR OS, and VRP (VRP has no NETCONF).
Open ROADM is a read-only NETCONF driver against native
`org-openroadm-device` YANG (`driver_openroadm.go`), not OpenConfig; optical
inventory is a separate `OpticalClient` interface (same split as
`ELINEApplier`). `optical.ApplyInventory` / PUT `/api/optical/device/:id/inventory`
persists kind, ports and xconnects (device-sync and
`factum2-driver optical-inventory-apply`).
See `internal/drivers/README-DRIVERS.md` for the per-platform table. It has two entry points, matching
the two hosts it runs on: `NewDriver(DriverParam)` takes everything
pre-resolved and is what the primary's web handlers use
(`web/handle_device_interfaces.go`, which already has the DB in hand), while
`NewDriverName(*util.ConfigFactum, name, user, pass)` resolves a device _by
name over the Factum API_ - `GET /api/device/name/:name` for its platform (via
`internal/factum.FactumClient.GetDeviceByName`) plus `GET /api/common-config`
for `DefaultDomain` (`drivers.DeviceFQDN` needs it to turn a short factum
device name into something resolvable), both through `util.FactumHTTP`.
That's what lets `factum2-driver-cli` (`cmd/driver`, which embeds
`cmdbase.ParamsAgent`) run on any host with network access to the devices,
with no Postgres access at all - same shape as the
DNS/Icinga/LibreNMS/Oxidized tools above.

### Web backend (`web/`)

Echo (`labstack/echo/v4`) HTTP server, one big router built in `web/web.go`.
Key things that live outside the obvious per-resource `handle_*.go` files:

- **No in-memory caching**: device/customer/service list and by-ID endpoints
  (`handle_dcim.go`, `handle_customer.go`, `handler_service.go`) query
  Postgres directly on every request via `gorm.G[T]`, generics-based. There
  used to be an in-memory `internal/tablecache` layer (`Dmgr`/`Cmgr`/`Smgr`)
  kept correct via RabbitMQ-driven invalidation plus a 30-minute forced
  refresh; it was removed as unnecessary complexity for this traffic
  volume - a fresh query is simple and fast enough. `fetchDevices` in
  `handle_dcim.go` still assembles a device's interfaces/addresses via
  separate flat `IN` queries rather than a preload join, which produced huge
  row-multiplying joins for devices with many interfaces/addresses - that
  part predates and is unrelated to the removed cache.
- **Generic CRUD**: `SecureCRUDHandler[Model, RequestDTO]`
  (`web/handle_crud.go`) implements GetAll/GetOne/Create/Update generically
  via a DB model type + a request DTO type, round-tripped through
  `json.Marshal`/`Unmarshal` — the DTO indirection is what prevents mass
  assignment (a client can't set fields absent from the DTO). Used directly
  for `Contact`, `Role`, and `Service`'s Update/Delete (each wrapped by a
  thin hand-written handler that adds a Lime-source guard before
  delegating); `User`, `Customer`, and `Service`'s Create have fully
  hand-written handlers instead because they need extra logic (password
  hashing, role-association flattening, `Service`'s service-ID
  auto-assignment in `ApiServiceCreate`/`web/handler_service.go` — the
  create-service wizard no longer preallocates a number, so this is where
  the caller's optional `service_id` is honored or the next
  available `<type><5-digit>` one is picked) the generic round-trip can't
  express — mirror that split rather than forcing everything through one
  path.
- **Auth**: login issues a JWT (`GenerateJWT`/`web/auth.go`) stored in an
  httpOnly `token` cookie (not a bearer header) — `RequireAPIAuth` middleware
  reads it back and sets `"user"` in the Echo context; `RequireAdmin` (must
  run after `RequireAPIAuth`) checks `models.User.HasRole("admin")`. Routes
  under `/api/admin/*` get both middlewares at the group level in `web.go`.
  The JWT signing key (`jwtKey` in `web/auth.go`) is set once at startup
  from `util.Config.Web.JWTSecret` (`GUI()` in `web.go`) and is required
  in every environment — `factum2-web start` refuses to start without it
  (`APP_ENV=development` does not fall back to a hardcoded key). Set
  `web.jwtsecret` in config. The legacy server-rendered form-POST `/login`
  and GET `/logout` routes (and the `github.com/gorilla/sessions` cookie
  store they used for flash messages) were removed once nothing referenced
  them - the SPA only
  ever calls `POST /api/login`/`POST /api/logout`, which return JSON instead
  of redirecting.
- **Service-to-service auth**: `RequireAPIAuth` also accepts
  `Authorization: Bearer <token>` (checked against `Settings.FactumApiToken`
  via `checkServiceToken`, constant-time compare, empty-configured-token
  never matches) as an alternative to the `token` cookie, for callers with
  no browser session — `internal/factum.FactumClient` (used by
  `internal/dns` and `internal/librenms`, both of which may run on a
  different host than the primary) sends this on the HTTPS fallback.
  Co-located CLIs that hit the worker unix socket send no bearer (the
  filesystem ACL is the auth; connecting is equivalent to possessing the
  service token, whether or not YAML still contains `factum.token`). Hub
  RPC into Echo sets `auth_method=token` via `worker.IsHubAuth` with no
  `"user"` — same ceiling as a bearer, not an admin session. Token auth
  sets `c.Get("auth_method") == "token"` but no `"user"`, so admin-gated
  routes that a remote CLI legitimately needs (e.g.
  `GET /api/librenms-config`, `GET /api/device-sync-config`) use
  `RequireAdminOrServiceToken` instead of `RequireAdmin` — plain
  `RequireAdmin` 401s a token (or hub) caller outright since there's no
  user to check a role on. Role gates used by the same CLIs for ordinary
  data (`RequireRead`/`RequireWrite`, via `requireAnyRole`) also accept a
  service token / hub auth for the same reason — e.g.
  `FactumClient.GetDeviceByName` hits `GET /api/device/name/:name`. An
  anchored allowlist (`internal/worker/hub_allowlist.go`, compiled
  `^(?:pattern)$`) runs **before** `ServeHTTP`; `/api/admin/*`,
  `/api/worker/run`, and `/api/netbox-webhook` are not reachable over the
  hub.
- **DTOs vs models**: models (`models/models.go`, `models/device.go`,
  `models/organisation.go`) embed `FactumModel` (ID/timestamps) and mark
  sensitive/relational fields `json:"-"`; DTOs are the JSON-facing shape.
  When adding an API-exposed field, decide deliberately which side it
  belongs on rather than exposing the model directly.
- **Route structure**: the Vue SPA build (`web/static/vue`) is served at
  `/` with HTML5 fallback for client-side routing (any unmatched path falls
  back to the SPA's `index.html`); `/api/*` has its own catch-all returning
  JSON 404 so it never falls through to the SPA handler. Static asset
  loading is build-tag-gated: `web/fs_dev.go` (default) reads
  `web/static` off disk, `web/fs_release.go` (`-tags release`) embeds it —
  see DEV.md for when each is used.

### Worker / hub transport (`internal/worker`)

The primary dials **out** to remote worker nodes over WebSocket, rather
than workers dialing in - the reverse of a traditional agent-connects-to-
broker model, deliberately: the primary is typically locked down hard, and
this way it needs zero new inbound firewall rules, while each worker host
only needs one narrow inbound rule scoped to the primary's IP
(`/hub` on `worker.listen`). Once this stack is live and mixed-UID CLIs can
open the unix socket, worker nets need no route to the primary's HTTPS
port. The admin-editable `models.WorkerNode`
list (Name/Address/Token/TLSCA/TLSSkipVerify/Enabled, the "Worker nodes"
admin page) is "who to dial", not a security boundary - editing it takes
effect within one `RemoteManager` reconcile pass (~10s), not a code change.

**Wire protocol** (`internal/worker/hub.go`): every message is an
`Envelope{Type, Payload}` - `hello` (agent -> primary, once per connection,
reports `Hostname`/`Roles`), `command` (primary -> agent, a predefined
command to run), `log` (agent -> primary, streamed stdout/stderr/exit),
`event` (structured job lines), `request` (agent -> primary HTTP-subset
RPC: method/path/body), `response` (primary -> agent: status/body, or
`Error` for transport failure → unix 502). `RemoteManager` (primary side,
`web.Controller.RemoteManager`, instantiated once in `web.GUI()`) holds one
supervised, auto-reconnecting connection per enabled `WorkerNode`.
`web.GUI()` registers routes, calls `remoteManager.SetAPIHandler(e)`,
**then** `go remoteManager.Run` so a connected worker never observes a nil
API handler. `factum2-worker start` runs `runHubListener` (`hub_agent.go`)
and the unix HTTP shim (`runLocalAPI`) together under an errgroup — either
dying fails `Start`.

**Local unix API**: CLIs on the worker host speak HTTP to
`/run/factum2-worker/api.sock` (dir `0750`, socket `0660`, group `factum`;
relocate both sides with `FACTUM_WORKER_API_SOCKET`). That is **not** a
route on `worker.listen` — `/hub` stays the only thing on the address the
primary can reach. Filesystem ACL is the auth (no extra token on the
socket). The same anchored allowlist (`^(?:pattern)$`, first-match-wins)
runs on the agent and again on the primary **before** in-process Echo
`ServeHTTP`. Hub auth is service-token equivalent for those routes only.
`util.FactumHTTP` probes Stat/Dial: missing / EACCES / EPERM / undialable
→ HTTPS if `factum.url` is set. After a successful Dial, unix 502 is **not**
retried over HTTPS. `FACTUM_WORKER_API_SOCKET=none` forces HTTPS.

**Concurrency**: `*websocket.Conn` allows one concurrent reader and one
concurrent writer. Every connection, both sides, has exactly one writer
goroutine (`runWriter`) draining a buffered `chan Envelope` ("outbox") -
nothing else ever calls `WriteJSON`/`WriteMessage` on the raw connection.
Dispatching a command (`RemoteManager.SendCommand`, called from an
arbitrary HTTP-handler goroutine), streaming a log line
(`runCommand`/`streamOutput`, one goroutine per running command on the
agent), or sending a hub RPC `request`/`response` just enqueues onto that
channel (`trySend`, bounded by `outboxSendTimeout` so a stuck/dead peer
can't hang the caller forever). Keep this pattern intact when touching
either side of the transport - it's the one thing standing between this
design and a data race. RPC responses use a longer write deadline
(`hubRPCWriteWait` 60s vs `hubWriteWait` 10s); a large `WriteJSON` delays
command dispatch to _that_ node for the duration of the write (up to 60s).
Marshaled-envelope cap is 32 MiB — oversize is a small 413, not a truncated
body and not a torn connection.

**Hub WSS**: `/hub` is TLS-only (`wss://`). `worker.tls_cert`/`worker.tls_key`
are required to start (`Start` loads the pair up front). The primary dials
`wss://<WorkerNode.Address>/hub` and verifies the certificate against
`WorkerNode.TLSCA` (PEM) if set, otherwise the system CA pool. The cert SAN
must match the hostname or IP in Address (Go ignores CN).
`WorkerNode.TLSSkipVerify` disables verification — still encrypted, not
MITM-safe; prefer pasting the worker's `hub.crt` as TLSCA. There is no
`ws://` fallback and no application-level payload cipher. Keep `/hub`
scoped to the primary's IP as defense in depth. TLS 1.2+; ALPN is
`http/1.1` only (WebSocket cannot run over HTTP/2).

**Dispatch**: `RemoteManager.SendCommand(role, args)` fans a `command`
envelope out to every connected node whose hello-reported `Roles` include
`role` - the same "every agent activated for a role receives it" semantics
the old rabbitmq topic-exchange binding had. `RemoteManager.RunAndWait`
additionally registers a temporary waiter (keyed by the generated command
ID) _before_ dispatching, so a fast agent response can't race ahead of the
primary listening for it, and blocks until the first `StreamExit` line (or
`ctx` cancellation) - used by `factum2-worker run` (`POST /api/worker/run`,
`web.ApiWorkerRun`, streamed back as NDJSON - see
`internal/worker/run_client.go`'s `RunRemote`, the CLI's HTTP client).
`RemoteManager.dispatchSingle`/`createAndDispatchTask`/`StartJob` are the
sync-trigger button's variant of the same idea, but deliberately dispatch
each target to _one_ matched node instead of fanning out - see "Jobs and
job tasks" in "Sync model" above for why a job task needs exactly one
execution. There's no separate message shape for a sync-trigger the way
`rabbitmq.SyncMsg` used to be - every dispatch path (job task, ad hoc run)
sends the same `command` envelope, just through a different
`RemoteManager` entry point depending on whether it needs to fan out,
wait, or persist a job/task record.

An agent only ever executes commands from its own `worker.commands` map,
looked up by name — the command line arriving over the wire is never used
to build a shell command directly, so a forged/compromised message can at
most select one of the commands the operator defined for that instance,
not run arbitrary code. Keep this indirection intact when touching
`internal/worker` or `internal/util/config.go`'s `ConfigWorkerCommand`.
`runCommand` (`hub_agent.go`) uses the `Worker`'s own process-lifetime
context (`w.runCtx`, set once by `Start`), not a per-connection context, so
a predefined command keeps running to completion even if the hub
connection drops mid-run. `worker.commands` is always local YAML,
deliberately - an agent's command allowlist is a security boundary tied to
that host. There used to be a separate `worker.roles` field selecting a
subset of `worker.commands` to activate (plus a reserved `"primary"` entry
for a worker instance that logged everything agents reported and
dispatched ad hoc commands from a separate rabbitmq connection); both are
gone - every `worker.commands` entry is activated unconditionally, and
"primary" behavior lives in `RemoteManager` itself rather than a
standalone process.

`worker.listen`/`worker.token`/`worker.tls_cert`/`worker.tls_key` are
effectively required for `factum2-worker start` to be useful - `Start`
errors if `worker.listen` is unset or the TLS pair is missing/unloadable,
since the instance would otherwise have no hub transport at all.
The unix API path cannot be disabled on the listener (`none`/`0` fails
`Start`); CLIs force HTTPS with `FACTUM_WORKER_API_SOCKET=none` instead.
`Start` also fails closed if the socket dir cannot be created at `0750`
(not world-accessible). `factum.url`/`token` are not required to start.

### CLI framework

All `cmd/*` binaries are built with `github.com/GiGurra/boa`
(`boa.CmdT[ParamsStruct]{...}`) on top of `spf13/cobra`, not raw cobra
(`factum2-icinga-notifications` is the exception: Icinga2's flag shape
collides with boa, so it uses `jessevdk/go-flags`). Params structs
normally embed `cmdbase.Params` (`cmd/cmd_base.go`), which supplies `-f`
(not `-c`) config-file loading into the full `util.ConfigRoot` (`db`/
`factum`/`web`/`worker`/`ldap_writeback` — NetBox/Lime/DNS/Icinga/LibreNMS/
Oxidized settings live in the `Settings` DB row, not YAML) and
`--debug`/`--loglevel`. boa treats most fields as required by default, so
embedding the full root forces a config file to fill in every section
regardless of whether the binary actually reads it. Binaries that run off
the primary (`factum2-worker`, `factum2-dns`, `factum2-icinga`,
`factum2-librenms`, `factum2-oxidized`, `factum2-prometheus`,
`factum2-device-sync`, `factum2-driver`, `factum2`) embed `cmdbase.ParamsAgent`
instead, which loads the leaner
`util.ConfigAgentRoot` (`factum`/`worker` only, default file
`/etc/factum2/factum2-worker.yaml`). `cmdbase.ShowConfig()` is the
`show-config` for full-`Params` binaries; `cmdbase.ShowConfigAgent`
(optional remote-config fetch) is the matching helper for `ParamsAgent`
binaries. Follow this pattern (typed params struct + `RunFuncE`) rather
than hand-rolling cobra commands.

### Frontend (`web/frontend`)

Vue 3 + Vite + Vue Router + Pinia + Nuxt UI + Axios + Tailwind CSS 4 +
MapLibre/deck.gl, built to `web/static/vue`. `src/api/*.js` wraps `axios`
(`baseURL: '/api'`, `withCredentials: true` — required since auth is
cookie-based) with one module per backend resource; a 401 response globally
redirects to `/login` except for `/me` and `/login` themselves, which use
401 as an expected "not logged in" / "wrong password" result. Views are
grouped by domain under `src/views/<domain>/`, matching the backend's
resource grouping. Vite's `/api` proxy targets `localhost:8090` — that is
the **live** instance, so do not use `npm run dev` to verify a change;
use the `run-factum2-web` skill instead.
