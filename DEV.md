# Development

Factum is a Go monorepo (module `github.com/abundo/factum2`) that produces
several CLI binaries plus a web GUI. The web GUI's frontend is a Vue 3 SPA
that's either served from disk (dev) or embedded into the `factum-web`
binary (release build).

## Prerequisites

- Go 1.25+
- Node.js `^22.18.0` or `>=24.12.0` (see `web/frontend/package.json`)
- PostgreSQL (app data, via GORM)
- A sibling checkout of [limetool](https://github.com/abundo/limetool) at
  `../limetool` (`go.mod` replace-points there)

## Database setup

```sh
sudo -u postgres psql
create database factum;
create user factum_user with encrypted password '<changeme>';
grant all privileges on database factum to factum_user;
alter database factum owner to factum_user;
```

Schema migrations run automatically (`gorm.AutoMigrate`) whenever a binary
connects to the DB — see `internal/util/db.go`.

## Configuration

Most binaries embed `cmdbase.Params` (`cmd/cmd_base.go`), which loads a YAML
config file (`-f`, default `/etc/factum2/factum2.yaml`) into
`util.ConfigRoot` (`internal/util/config.go`). That struct is the source of
truth for available keys (`db`, `factum`, `web`, `worker`, `ldap_writeback`).
See `examples/factum2.yaml` / `examples/factum2-worker.yaml`.

`factum-worker` (`cmd/worker`) is the exception: it embeds
`cmdbase.ParamsAgent` instead, loading the smaller `util.ConfigAgentRoot`
(just `factum` and `worker`) — a remote worker's config file only needs
those two sections, not `db`/`web`.

Netbox, Lime and DNS are exceptions: none has a YAML config key. Their
settings live in the `Settings` DB row instead (`util.GetOrCreateSettings`,
`internal/util/settings.go`), editable from the admin UI's Netbox/Lime/DNS
tabs — every place that needs one of these (`factum-netbox`,
`internal/netbox`, `internal/librenms` for Netbox; `factum-lime`,
`internal/lime` for Lime; `factum-dns`, `internal/dns` for DNS) reads it
from there, so it needs a DB connection (hence `-f` still has to point at a
config with valid `db:` credentials, even for the read-only `factum-netbox
get-*` commands).

LibreNMS/Icinga/DNS/Oxidized/device-sync are DB-backed too, but their CLIs
normally run on a different host than the primary, so they can't reach the
DB directly — they fetch config over REST (`GET /api/<service>-config`),
authenticated with `factum.token` (see below). Their YAML only needs
`factum.url`/`factum.token` (plus `worker.*` if that host also runs
`factum-worker start`). LibreNMS's own MySQL credentials are read from
LibreNMS's `.env` on disk, not from factum config.

`factum.token` is a shared secret (matched against the primary's
`Settings.FactumApiToken`, set from the admin UI's Factum tab) that lets a
process with no browser session — `internal/factum`'s HTTP client, used by
`factum-dns` and `factum-librenms-cli` — authenticate to the API as a
service instead of a logged-in user. Set it in both places (admin UI and
the remote host's `factum.token` config key) or those commands get 401s.

`--debug` / `--loglevel` control logging (`cmdbase.SetupLog`); debug level
also enables source locations in log output.

`web.jwtsecret` must be set to a random secret for anything but local dev —
`factum-web gui` refuses to start without it unless `APP_ENV=development`,
in which case it falls back to a fixed insecure value and logs an error. It
signs the API auth cookie (`web/auth.go`); anyone who knows it can forge a
login for any user.

`ldap_writeback.bind_dn` / `ldap_writeback.bind_password` are optional and
only needed if the admin UI's "Allow password changes to be written back to
LDAP/AD" switch (Settings.LdapAllowPasswordChange, on the Authentication
settings page) is turned on. Unlike every other LDAP setting, these live
only here, never in the DB or the Settings API — same reasoning as
`web.jwtsecret` — because they need much stronger directory permissions
than the ordinary read-only `ldap_bind_dn`/`ldap_bind_password` search
account, which any admin can already see via `GET /api/admin/settings`.

## Binaries (`cmd/...`, built to `build/`)

| Binary                        | Source                     | Purpose                                                                                                    |
| ----------------------------- | -------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `factum`                      | `cmd/factum`               | Query the factum HTTP API (`get-device`, `get-devices`, `show-config`)                                     |
| `factum-driver`               | `cmd/driver`               | Run device-driver commands over the Factum API (`exec`, `version`, ...)                                    |
| `factum-dns`                  | `cmd/dns`                  | Push device data into DNS (`update`)                                                                       |
| `factum-icinga`               | `cmd/icinga`               | Sync Icinga with factum (`get-hosts-down`, `get-services-down`, `show-events`, `sync`)                     |
| `factum-icinga-notifications` | `cmd/icinga-notifications` | Icinga2 `NotificationCommand` - builds and sends the HTML alert email for a host/service notification      |
| `factum-lime`                 | `cmd/lime`                 | Sync customers from Lime CRM (`sync`)                                                                      |
| `factum-librenms`             | `cmd/librenms`             | Sync/query LibreNMS with factum (`sync`, `get-devices`, `get-device`, `get-device-ports`, `get-locations`) |
| `factum-becs`                 | `cmd/becs`                 | Sync BECS elements into Netbox (then factum) (`get-element`, `sync`)                                       |
| `factum-netbox`               | `cmd/netbox`               | Query/sync NetBox (`get-device`, `get-devices`, `get-device-type`, `sync`, `check`)                        |
| `factum-oxidized`             | `cmd/oxidized`             | Sync Oxidized with factum (`sync` — currently a no-op stub, see below)                                     |
| `factum-web`                  | `cmd/web`                  | Web GUI + API server (`gui`, `createadmin`)                                                                |
| `factum-worker`               | `cmd/worker`               | Hub-transport task runner/agent (`start`, `run`, `show-config`)                                            |

CLIs are built with `github.com/GiGurra/boa` on top of `spf13/cobra`
(`boa.CmdT[...]`), not raw cobra - `factum-icinga-notifications` is the one
exception: Icinga2 invokes it with its own fixed flag shape (`-d`, `-l`,
`-r`, `-t`, `--HOSTNAME`, a bare `--SERVICE` sentinel, ...), which collides
with boa's global `-d`/`-l` (Debug/Loglevel, present on every other `cmd/*`
binary). It parses `os.Args` directly with `github.com/jessevdk/go-flags`
instead, using the same short/long flag names as the Python script it
replaced.

`factum-oxidized sync` writes oxidized's `router.db` from factum devices
(filtered by enabled/`CfBackupOxidized`/`Settings.OxidizedIgnore*` plus a
primary IPv4) and asks oxidized to reload only if the file changed.

## Building

```sh
make            # all Go binaries except the web-release variant, into build/
make factum-web # just the web binary (frontend NOT embedded, served from disk)
make frontend   # npm ci && npm run build in web/frontend -> web/static/vue
make release    # all binaries + factum-web-release (frontend embedded via go:embed)
make snapshot   # GoReleaser snapshot into dist/ (does not publish)
make install    # release build + install to /opt/factum2 + restart factum-gui.service
```

`INSTALL_DIR` in the `Makefile` is `/opt/factum2`.

## Testing

```sh
make test            # go test ./... - no device, no database, no network
```

GitHub Actions runs `go vet`, `make test`, `make`, and a GoReleaser snapshot
on every push to `main` and on pull requests
(`.github/workflows/ci.yml`). The workflow checks out
`github.com/abundo/limetool` as a sibling of the factum2 checkout because
`go.mod` replace-points at `../limetool`.

Device drivers (`internal/drivers`) are tested at two levels. The default
tests run against `fakeEOS` (`driver_arista_eos_test.go`), an `httptest` TLS
server speaking eAPI's JSON-RPC - no refactoring was needed to make that
possible, since `eapiRunCmds` builds its URL from `DriverParam.Name` and
`eapiClient` already skips certificate verification. They cover request
shapes, reply decoding, the eAPI fallback and its error joining.

What a fake can't cover - the NETCONF/OpenConfig transport, real EOS reply
shapes, and whether the NETCONF and eAPI paths actually agree - is in
`driver_arista_eos_integration_test.go`, behind the `integration` build tag
and run against a [containerlab](https://containerlab.dev) cEOS lab:

```sh
make lab-up              # containerlab deploy (needs the cEOS image, see below)
make test-integration    # go test -tags integration ./internal/drivers/
make lab-down
```

The topology and the EOS startup config that enables eAPI and the NETCONF
agent (neither is on by default) are in `internal/drivers/testdata/clab/`.
cEOS-lab is distributed only to registered Arista accounts - download the
image, `docker import cEOS64-lab-<version>.tar.xz ceos:<version>`, and set
`CEOS_IMAGE` if it isn't the version the topology defaults to. Point the
tests at a real switch instead with `FACTUM_TEST_EOS_HOST`/`_USER`/`_PASS`;
they skip themselves when it's unset, and `FACTUM_TEST_EOS_IFACE` selects the
one interface the write test modifies (it restores what was there).

Containerlab has no image for Nokia SR OS (SR Linux is a different NOS with a
different management model) and none for IOS-XR that runs without a lot of
resources, so `driver_nokia_sros.go` and `driver_iosxr.go` can get the fake-
server treatment for their CLI/SSH transports but not the container-backed
tier.

### Web integration tests (LDAP, mail, Postgres)

`internal/ldapauth` and `internal/mail` have no coverage in the default
`web/*_test.go` tier (sqlite in-memory, no directory server or SMTP relay -
`web/auth_test.go`'s LDAP tests stub `ldapauth.Authenticate` at a sentinel
seam rather than hitting a real server). `web/ldap_mail_integration_test.go`
covers the real thing instead, against a small Postgres + OpenLDAP + mailpit
(SMTP catcher with a REST API to assert what arrived) stack:

```sh
make itest-up               # docker compose up, in testdata/itest
make test-integration-web   # go test -tags integration ./web/
make itest-down             # also fixes ownership of testdata/itest/ldap/ -
                             # openldap chowns that bind mount to its own
                             # uid on every start
```

Same build-tag convention as the driver tests above (`go test ./...` never
picks these up), and same skip-if-unreachable behavior instead of failing
outright - see the file's header comment for the `FACTUM_TEST_LDAP_*`/
`FACTUM_TEST_SMTP_*`/`FACTUM_TEST_MAILPIT_API` overrides. The seed user/group
(`uid=testuser`, `cn=admins`) are loaded from `testdata/itest/ldap/
bootstrap.ldif` on first boot - edit it and re-run `itest-up` (after
`itest-down`, so the container reseeds) to add more directory fixtures.

Postgres runs in this stack too (port 55432, throwaway - not currently used
by any test, but there for a future test that wants `util.MigrateDatabase`
against a real Postgres instead of the sqlite fakes `web/auth_test.go`
uses).

### NetBox and LibreNMS

Both are REST-ish clients configured with a plain base URL
(`netboxtool.ConfigNetbox.URL`, `util.ConfigLibrenms.URL`) - the same shape
as the Arista eAPI client above - so the same two-tier pattern applies, and
is the intended next step (not yet built):

- **Default tier**: an `httptest` fake server per package (`internal/netbox`,
  `internal/librenms`), mirroring `fakeEOS` in
  `internal/drivers/driver_arista_eos_test.go` - fast, no containers, covers
  request/response shape. NetBox talks GraphQL (`netboxtool`'s
  `graphqlPageSize`-driven paging), not plain REST, so its fake needs to
  decode a GraphQL POST body rather than match REST paths/verbs the way
  LibreNMS's or eAPI's fake can.
- **Opt-in real-container tier**: official images
  (`netbox-community/netbox-docker`, `librenms/librenms`) are each a
  multi-container app in their own right (NetBox: its own Postgres+Redis;
  LibreNMS: MySQL+Redis, and `factum-librenms-cli` also reads LibreNMS's own
  MySQL directly for `PortsGet`/`PortsUpdateIgnore` - see AGENTS.md's sync
  section) - too heavy to fold into `testdata/itest` above without slowing
  down every LDAP/mail test run. Give each its own compose file/Makefile
  targets instead (`netbox-lab-up`/`-down`, `librenms-lab-up`/`-down`),
  following `lab-up`/`lab-down`'s pattern for containerlab, rather than
  vendoring the upstream projects' compose files - point at a checkout of
  them instead.

## Release

Releases are built with [GoReleaser](https://goreleaser.com/) (pure Go,
`CGO_ENABLED=0`; `factum-web` with `-tags release` so the Vue SPA is
embedded) and published to GitHub when a `v*` tag is pushed
(`.github/workflows/release.yml`). Tests must pass first.

    git tag -a v0.1.0 -m "v0.1.0"
    git push origin v0.1.0

Each release is one `tar.gz` (and a `.deb`) per linux/amd64 and
linux/arm64, containing every `cmd/*` binary plus `LICENSE`, `README.md`,
and `examples/`. The `.deb` installs binaries to `/opt/factum2` and systemd
units to `/lib/systemd/system`.

Local dry-run (writes `dist/`, does not publish):

    goreleaser check
    make snapshot

## Running the web GUI in dev

Backend (serves `/api`, reads `web/static/vue` off disk at runtime so a
frontend rebuild doesn't need a Go rebuild):

```sh
APP_ENV=development go run ./cmd/web start -f /etc/factum2/factum2.yaml -b 0.0.0.0:8090
```

`APP_ENV=development` falls back to an insecure JWT signing key if
`web.jwtsecret` isn't set in config (logging an error) and disables the
panic-recovery middleware — don't run this mode against anything but a
local/dev DB.

Frontend (hot-reloading Vite dev server on `:5173`, proxies `/api/*` to
`localhost:8090` — see `web/frontend/vite.config.js`):

```sh
cd web/frontend
npm install
npm run dev
```

Create the first admin user with:

```sh
go run ./cmd/web createadmin -f /etc/factum2/factum2.yaml
```

### Frontend build output and routing

- `npm run build` outputs to `web/static/vue` (`base: '/'`).
- `factum-web` serves the SPA at `/` and falls back to `index.html` for any
  unmatched path so client-side routes survive a hard refresh (see
  `web/web.go`); unmatched `/api/*` returns a JSON 404 instead of falling
  through to the SPA.
- Build-mode-specific static filesystem: `web/fs_dev.go` (`os.DirFS`,
  default) vs `web/fs_release.go` (`//go:embed all:static`, `-tags
release`).

### Frontend stack

Vue 3, Vite, Vue Router, Pinia, Nuxt UI, Axios, Tailwind CSS 4, MapLibre +
deck.gl. Linting via `oxlint` + `eslint`, formatting via `prettier`.

```sh
cd web/frontend
npm run lint    # oxlint --fix, then eslint --fix
npm run format  # prettier --write src/
```

Layout:

```
web/frontend/src/
  api/        axios wrapper (http.js, baseURL "/api", withCredentials) + one
              module per resource (customers, devices, users, roles, sync, ...)
  components/ shared components (DeviceInterfacePicker, ServiceEditDialog, ...)
  layout/     app chrome (AppLayout, AppTopbar, AppSidebar, AppMenu, ...)
  router/     vue-router routes
  stores/     pinia stores (auth.js, deviceCredentials.js)
  views/      route-level pages, grouped by domain (admin/, auth/, customer/,
              device/, service/, sync/)
```

## Worker hub transport

- The primary (`factum-web`) dials **out** to remote `factum-worker` hosts
  over WebSocket (`internal/worker/hub.go`/`hub_agent.go`), not the other
  way round — see `AGENTS.md`'s "Worker / hub transport" section for the
  wire protocol and why the dial direction is reversed. The admin UI's
  "Worker nodes" page (`models.WorkerNode`) is where you register a
  `factum-worker start` instance's address/token so the primary can reach
  it; sync-trigger buttons (`handle_sync.go`) and `factum-worker run`
  (`handle_worker.go`'s `ApiWorkerRun`) both dispatch predefined shell
  commands (`worker.Commands` in config) through the same path and stream
  logs back — see the comment on `util.ConfigWorkerCommand` for why the
  command line is never taken from the wire directly.
- **Sync job events**: add `--job` to a `worker.commands` entry's `args` to
  have that predefined command report structured info/warning/error events
  (persisted as `models.SyncJobEvent`, visible in `/sync/status`'s job
  history) instead of plain console output — it's per-entry opt-in, not
  automatic, since `worker.commands` can run arbitrary shell commands, not
  just the five sync tools that understand the flag:
    ```yaml
    worker:
        listen: ":8443"
        token: "<shared secret, matches this node's WorkerNode.Token>"
        roles: ["librenms"]
        commands:
            librenms:
                cmd: /path/to/factum-librenms-cli
                args: ["sync", "--job"]
    ```
    See `AGENTS.md`'s "Sync jobs" bullet for how the agent decides a stdout
    line is a structured event rather than plain text.

See [README.md § Installing a worker node](README.md#installing-a-worker-node)
for how to set up a `factum-worker` instance on a remote host.
