# Factum

[![ci](https://github.com/abundo/factum2/actions/workflows/ci.yml/badge.svg)](https://github.com/abundo/factum2/actions/workflows/ci.yml)

Factum tracks network infrastructure (devices, customers, services) and
syncs it with external systems of record — NetBox and Lime CRM sync data
_into_ factum's Postgres DB, while DNS, Icinga and LibreNMS are synced
_from_ factum. It's a Go monorepo producing several CLI binaries (`cmd/*`)
plus a web GUI (`factum-web`) with a Vue 3 SPA frontend (`web/frontend`).

See [DEV.md](DEV.md) for full setup/build/run details (config file shape,
Makefile targets, dev workflow, binaries list) and [AGENTS.md](AGENTS.md)
for architecture notes.

## Prerequisites

- Go 1.25+
- Node.js `^22.18.0` or `>=24.12.0` (only needed to build/dev the web
  frontend — see `web/frontend/package.json`)
- PostgreSQL (app data, via GORM)

## Quickstart

### 1. Create the PostgreSQL database

```sh
sudo -u postgres psql
create database factum2;
create user factum2_user with encrypted password '<changeme>';
grant all privileges on database factum2 to factum2_user;
alter database factum2 owner to factum2_user;
```

Schema migrations run automatically (`gorm.AutoMigrate`) the first time a
binary connects to the DB.

### 2. Create a config file

```sh
sudo mkdir -p /etc/factum2
sudo cp examples/factum2.yaml /etc/factum2/
```

This is the default path every `cmd/*` binary looks for (`-f`, see
`cmd/cmd_base.go`). Almost all runtime settings (NetBox/Lime/DNS/Icinga/
LibreNMS connections, JWT secret, ...) live in the database and are edited
from the admin UI instead — the YAML file only needs `db:` credentials plus,
for a first run, `web.jwtsecret` (required outside `APP_ENV=development`).
See [DEV.md § Configuration](DEV.md#configuration) for the full key
reference.

### 3. Build

```sh
make            # all CLI binaries into build/ (excludes factum-web-release)
make frontend   # builds web/frontend -> web/static/vue
```

Tagged releases (`v*`) are built with [GoReleaser](https://goreleaser.com/)
and published by GitHub Actions — see [DEV.md § Release](DEV.md#release).

### 4. Run the web GUI

```sh
APP_ENV=development go run ./cmd/web start -f /etc/factum2/factum2.yaml -b 0.0.0.0:8090
```

`APP_ENV=development` allows starting without `web.jwtsecret` set (falls
back to an insecure key) — don't use it against anything but a local/dev
database.

Create the first admin user:

```sh
go run ./cmd/web createadmin -f /etc/factum2/factum2.yaml
```

Then log in at `http://localhost:8090`.

For frontend hot-reload during development (`cd web/frontend && npm install
&& npm run dev`, proxies to the backend on `:8090`), installing via
`./install.py` (GitHub release on production, or `--source` from this tree
in the lab), and everything else, see [DEV.md](DEV.md).

## Installing a worker node

A worker node is a `factum-worker` instance running the `start` subcommand
on a remote host (typically the DNS/Icinga/LibreNMS/Oxidized server, or any
other host that needs to run one of the sync tools). The primary dials
**out** to it, so the worker host only needs one inbound firewall rule
scoped to the primary's IP — see [AGENTS.md § Worker / hub
transport](AGENTS.md#worker--hub-transport-internalworker) for why the dial
direction is reversed.

1. **Build and copy the binary.** `make factum-worker` (or `make release`
   for every binary) builds `build/factum-worker`; copy it to the target
   host, e.g. `/opt/factum2/factum-worker`. (`./install.py --source`
   automates this step — plus the systemd install in step 4 — over ssh for
   every node already registered and enabled in the `worker_nodes` table.)

2. **Create the config file**, starting from `examples/factum2-worker.yaml`:

    ```sh
    sudo mkdir -p /etc/factum2
    sudo cp examples/factum2-worker.yaml /etc/factum2/factum2-worker.yaml
    ```

    Then edit it for this host:

    - `factum.token` — shared secret matched against the primary's
      `Settings.FactumApiToken` (admin UI, Factum tab). Needed by any of the
      REST-config-fetching tools this worker runs (`factum-dns`,
      `factum-icinga`, `factum-librenms`, `factum-oxidized`) and by
      `factum-worker run` if you use it from this host.
    - `worker.listen` — bind address for the hub listener the primary dials
      into, e.g. `:8443`.
    - `worker.token` — shared secret this worker expects from the primary on
      connect; set the same value on the matching `WorkerNode.Token` in step 3.
    - `worker.commands` — trim the map down to only the commands this host
      should handle, with `cmd` pointing at that tool's path on this host
      (e.g. `/opt/factum2/factum-dns`). Add `--job` to a command's `args` to
      get structured sync-job events instead of plain console output (see
      [DEV.md § Sync job events](DEV.md#worker-hub-transport)).

    Generate both secrets with `openssl rand -base64 32`.

    `netbox`/`lime`/`becs` are the exception: unlike the others, they talk to
    Postgres directly instead of fetching config over REST (see AGENTS.md's
    "Sync model" section), and default to reading `/etc/factum2/factum2.yaml`
    (the _full_ config, with a `db:` section) rather than
    `factum2-worker.yaml`. Only put them in a worker's `commands` map on the
    primary host itself, where that full config already exists at the
    default path.

3. **Register the node with the primary**: admin UI → Worker nodes → Add,
   with Address set to `host:port` matching this node's `worker.listen` and
   Token matching its `worker.token`. Takes effect within one
   `RemoteManager` reconcile pass (~10s) — no primary restart needed.

4. **Install and start the systemd unit:**

    ```sh
    sudo cp examples/factum-worker.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now factum-worker.service
    ```

5. **Verify**: `/sync/status` in the web UI (or `GET /api/worker/status`)
   lists connected nodes and what they handle; `journalctl -u factum-worker
-f` on the worker host for logs.

## NetBox webhook (partial sync)

`POST /api/netbox-webhook` lets NetBox push change events instead of waiting
for `factum-netbox sync`'s full polling sync. On a Device, Interface or IP
Address create/update (or an Interface/IP delete) it resyncs just that one
device (interfaces, addresses and tags included). On a Device deletion it
removes the matching netbox-sourced factum row by the payload's id —
NetBox has already deleted the object, so it cannot be re-fetched. Cable
and site create/update re-fetch that one object and upsert the local
Connection/Site row; their deletions remove the row by the payload's id
the same way. Tenant events are ignored — customer→tenant sync is
factum→NetBox.

The endpoint isn't a logged-in user or a `factum.token` service client, so
it authenticates differently: it verifies NetBox's HMAC-SHA512
`X-Hook-Signature` header against a shared secret, and rejects every request
if that secret isn't set. Configure it in two places:

1. **factum** — admin UI → Settings → NetBox tab → "Webhook secret", set to
   a random string, then Save.
2. **NetBox** (3.x/4.x split webhook config into a reusable "Webhook"
   endpoint definition plus one or more "Event Rules" that bind it to
   specific object types/events):

    - **Operations → Webhooks → Add**:
        - Name: e.g. `factum-sync`
        - URL: `https://<factum-host>/api/netbox-webhook`
        - HTTP method: `POST`, HTTP content type: `application/json` (default)
        - Secret: the same string entered in factum's admin UI above
        - Leave the body template blank — factum expects NetBox's default
          payload shape (`event`/`model`/`data`/...).
        - Enable SSL verification unless `<factum-host>` is on a self-signed
          cert reachable only internally.
    - **Operations → Event Rules → Add**:
        - Object types: DCIM → Device, DCIM → Interface, DCIM → Cable,
          DCIM → Site, IPAM → IP Address
        - Events: enable Creations, Updates and Deletions. Device/cable/site
          deletions remove the matching factum row; interface/IP deletions
          still resync the parent device.
        - Action type: Webhook, Action: the webhook created above.
    - NetBox's webhook edit page has a "Test" action that sends a real signed
      sample payload — useful for confirming the secret matches without
      waiting for a real change.

`factum-netbox check` reads the live NetBox extras API and verifies that
setup: a webhook whose URL is `{PublicBaseURL}/api/netbox-webhook`, enabled
event rules covering Device / Interface / IP Address / Cable / Site
create+update+delete,
and reports the custom fields factum needs. Pass `--update` to create
missing fields and patch drifted label/description/group/object types
(never `required`, and never a type change: NetBox forbids that). Selection fields without a prescribed
choice list (`role`) are reported if missing; NetBox will not accept a
select field with no choices. Missing `alarm_destination` /
`alarm_timeperiod` are created with seed choices (example addresses and
SLA windows) and those lists are never updated if the field already
exists. `connection_method` gets a `ssh`/`telnet` choice set.
Integration fields (`becs_oid`, `librenms_id`, `optical_role`) are
only created when that source/destination is enabled. The webhook
secret is write-only in NetBox, so the check only confirms factum has one
configured. Exits non-zero if anything required cannot be fixed.

## License

[AGPL-3.0-or-later](LICENSE). Copyright (c) 2026 Anders Löwinger.
