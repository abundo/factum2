# Factum

[![ci](https://github.com/abundo/factum2/actions/workflows/ci.yml/badge.svg)](https://github.com/abundo/factum2/actions/workflows/ci.yml)

Factum tracks network infrastructure (devices, customers, services) and
syncs it with external systems of record — NetBox and Lime CRM sync data
_into_ factum's Postgres DB, while DNS, Icinga and LibreNMS are synced
_from_ factum. It's a Go monorepo producing several CLI binaries (`cmd/*`)
plus a web GUI (`factum2-web`) with a Vue 3 SPA frontend (`web/frontend`).

See [DEV.md](DEV.md) for full setup/build/run details (config file shape,
Makefile targets, dev workflow, binaries list) and [AGENTS.md](AGENTS.md)
for architecture notes. To add a capacity service type (roles, platform
packs, CLI templates), see
[docs/cfgmgmt-service-design.md](docs/cfgmgmt-service-design.md).

## Prerequisites

- PostgreSQL (app data, via GORM)
- Python 3 (only for `install.py` on a production host)
- Go 1.25+ and Node.js `^22.18.0` or `>=24.12.0` (only to build/dev from
  source — see `web/frontend/package.json`)

## Quickstart

### Production (GitHub release)

No Go or Node needed. Tagged releases ship linux/amd64 and linux/arm64
binaries, systemd units, and `install.py`. The copy at
`/etc/factum2/install.py` is only a launcher: after you pick a tag it
runs **that release's** `install.py` (from the tarball, or the git tag
if the archive is older) so install steps match those binaries.

#### 1. Create the PostgreSQL database

```sh
sudo -u postgres psql
create database factum2;
create user factum2_user with encrypted password '<changeme>';
grant all privileges on database factum2 to factum2_user;
alter database factum2 owner to factum2_user;
```

Schema migrations are a dedicated command (`factum2-web migrate` / `factum2
migrate`) — they do **not** run when the GUI or a sync CLI starts, because
rewriting tables while `factum2-web` is serving is unsafe. `install.py`
applies them during install (step 3); to run them by hand, stop the GUI
first:

```sh
sudo /opt/factum2/factum2-web migrate -f /etc/factum2/factum2.yaml
```

#### 2. Config and installer

```sh
sudo mkdir -p /etc/factum2
sudo curl -fsSL -o /etc/factum2/factum2.yaml \
  https://raw.githubusercontent.com/abundo/factum2/main/examples/factum2.yaml
sudo curl -fsSL -o /etc/factum2/factum2-worker.yaml \
  https://raw.githubusercontent.com/abundo/factum2/main/examples/factum2-worker.yaml
sudo curl -fsSL -o /etc/factum2/install.py \
  https://raw.githubusercontent.com/abundo/factum2/main/install.py
sudo chmod +x /etc/factum2/install.py
```

If the repo is private, add `-H "Authorization: Bearer $GITHUB_TOKEN"` to
the `curl` commands (or `export GITHUB_TOKEN=...` before running
`install.py`).

Edit `/etc/factum2/factum2.yaml`: set `db:` credentials and `web.jwtsecret`
(`openssl rand -base64 48`). Edit `/etc/factum2/factum2-worker.yaml` for
the local worker (`worker.listen`, `worker.token`, `worker.tls_cert`/
`worker.tls_key`; `factum.url`/`token` are
Stat/Dial fallback, omit on start-only hosts). Almost all other runtime
settings (NetBox/Lime/DNS/Icinga/LibreNMS, ...) live in the database and
are edited from the admin UI. See
[DEV.md § Configuration](DEV.md#configuration) for the full YAML key
reference.

#### 3. Select a release

```sh
sudo /etc/factum2/install.py
```

On a TTY the installer lists GitHub releases; highlight one and press Enter.
Non-interactive: `sudo /etc/factum2/install.py --install latest --yes`.
A standalone copy offers to replace itself from the latest GitHub
**release** (`--self-update`), never from `main` — an unreleased installer
must not drive binaries that lack its commands (for example `factum2-web
migrate` on a tag from before that subcommand existed).

That copies binaries to `/opt/factum2`, stops `factum2-web` if it is running,
applies schema migrations (`factum2-web migrate`, when that tag has the
command), then installs systemd units:

- **this host (primary):** `factum2-web.service` and `factum2-worker.service`
- **each enabled worker node:** `factum2-worker.service`

A unit that is not on disk yet is installed and `systemctl enable --now`'d.
If the file is already there and matches this release, it is left alone
and the unit is restarted. If it has been modified, the installer prints a
diff and asks before overwriting (`--yes` overwrites without asking).

#### 4. Create the first admin user

```sh
sudo /opt/factum2/factum2-web createadmin -f /etc/factum2/factum2.yaml
```

Then log in at the address in `web.bind` (the example config uses
`http://127.0.0.1:8091`).

### From source (development)

```sh
sudo mkdir -p /etc/factum2
sudo cp examples/factum2.yaml /etc/factum2/
```

```sh
make            # all CLI binaries into build/ (excludes factum2-web-release)
make frontend   # builds web/frontend -> web/static/vue
```

Tagged releases (`v*`) are built with [GoReleaser](https://goreleaser.com/)
and published by GitHub Actions — see [DEV.md § Release](DEV.md#release).

```sh
go run ./cmd/web migrate -f /etc/factum2/factum2.yaml
APP_ENV=development go run ./cmd/web start -f /etc/factum2/factum2.yaml -b 0.0.0.0:8090
```

`APP_ENV=development` disables panic-recovery middleware — don't use it
against anything but a local/dev database. `web.jwtsecret` is still
required (see `examples/factum2.yaml`).

```sh
go run ./cmd/web createadmin -f /etc/factum2/factum2.yaml
```

Then log in at `http://localhost:8090`.

For frontend hot-reload (`cd web/frontend && npm install && npm run dev`,
proxies to the backend on `:8090`), `./install.py --source` from this tree,
and everything else, see [DEV.md](DEV.md).

## Installing a worker node

A worker node is a `factum2-worker` instance running the `start` subcommand
on a remote host (typically the DNS/Icinga/LibreNMS/Oxidized/Prometheus server, or any
other host that needs to run one of the sync tools). The primary dials
**out** to it, so the worker host only needs one inbound firewall rule
scoped to the primary's IP (`/hub` on `worker.listen`) — see [AGENTS.md §
Worker / hub transport](AGENTS.md#worker--hub-transport-internalworker)
for why the dial direction is reversed.

Co-located CLIs (`factum2-dns`, `factum2-icinga`, `factum2-librenms`,
`factum2-oxidized`, `factum2-prometheus`, `factum2-device-sync`, `factum2-driver`,
`factum2-icinga-notifications`) reach the primary's REST handlers through
that hub connection, via a localhost-only unix socket
(`/run/factum2-worker/api.sock`). Worker networks then do **not** need a
route to the primary's HTTPS port. The primary still serves HTTPS to
operators and to NetBox's `POST /api/netbox-webhook`.

```
  worker host                              management network
  ───────────                              ──────────────────
  factum2-worker :8443 /hub  <── wss:// ──  factum2-web (dials out)
  unix /run/factum2-worker/api.sock         HTTPS :443  <── operators
  CLIs ── HTTP ────────────^               HTTPS :443  <── NetBox webhook
```

| Path                                        | Direction                                                       | Required?                                                                                                      |
| ------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Primary → `worker.listen` `/hub` (`wss://`) | outbound from primary / inbound on worker, scoped to primary IP | Yes                                                                                                            |
| Worker network → primary HTTPS `:443`       | worker → primary                                                | **No**, once this stack is live _and_ mixed-UID CLIs can open the socket (group `factum` + icinga/nagios user) |
| Operator browser → primary HTTPS            | inbound on primary, management net                              | Yes                                                                                                            |
| NetBox → `POST /api/netbox-webhook`         | inbound on primary, from NetBox                                 | Yes                                                                                                            |

Closing worker-net → primary `:443` is an **operator firewall step** after
this stack is in production; the software does not unbind the port. Do not
close it until Icinga notification commands can open the unix socket (see
group `factum` below). `factum2-worker run` is not tunneled (`POST
/api/worker/run` NDJSON) and still needs HTTPS from whatever host you run
it on — typically a management-net host, not the worker.

`/hub` is WSS (`wss://`); config secrets on `EnvelopeResponse` are
TLS-protected. Keep it scoped to the primary's IP as defense in depth.
There is no `ws://` fallback.

1. **Build and copy the binary.** `make factum2-worker` (or `make release`
   for every binary) builds `build/factum2-worker`; copy it to the target
   host, e.g. `/opt/factum2/factum2-worker`. (`/etc/factum2/install.py` from
   a GitHub release, or `./install.py --source` from this tree, automates
   this step — plus `groupadd -r factum` and the systemd unit in step 4 —
   over ssh for every node already registered and enabled in the
   `worker_nodes` table.)

2. **Create the config file**, starting from `examples/factum2-worker.yaml`:

    ```sh
    sudo mkdir -p /etc/factum2
    sudo cp examples/factum2-worker.yaml /etc/factum2/factum2-worker.yaml
    ```

    Then edit it for this host:

    - `factum.url` / `factum.token` — Stat/Dial fallback only (socket
      missing, unreadable, or undialable). They are **not** a retry path
      for unix 502 / timeout after Dial succeeded. Start-only hosts may
      omit both. Keep them for `factum2-worker run` if you use it from this
      host, for `factum2-icinga-notifications` until the icinga/nagios user
      is in group `factum`, and for any CLI not co-located with a worker.
      Force HTTPS even if the socket exists with
      `FACTUM_WORKER_API_SOCKET=none` (or `0`) in that CLI's environment
      (same as `factum.socket: none`).
    - `worker.listen` — bind address for the hub listener the primary dials
      into, e.g. `:8443`. Only `/hub` is served here (WSS).
    - `worker.token` — shared secret this worker expects from the primary on
      connect; set the same value on the matching `WorkerNode.Token` in step 3.
    - `worker.tls_cert` / `worker.tls_key` — PEM files for the hub listener.
      Required to start. Generate a cert whose SAN matches the hostname or
      IP in Address (Go ignores CN):

      ```sh
      sudo openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
        -keyout /etc/factum2/hub.key -out /etc/factum2/hub.crt \
        -subj "/CN=$(hostname -f)" \
        -addext "subjectAltName=DNS:$(hostname -f)"
      sudo chmod 640 /etc/factum2/hub.key /etc/factum2/hub.crt
      ```

      If Address is an IP, use `IP:192.0.2.10` in the SAN instead (or as well).
    - `worker.commands` — trim the map down to only the commands this host
      should handle, with `cmd` pointing at that tool's path on this host
      (e.g. `/opt/factum2/factum2-dns`). Add `--job` to a command's `args` to
      get structured sync-job events instead of plain console output (see
      [DEV.md § Sync job events](DEV.md#worker-hub-transport)).
    - Relocate the unix socket with `FACTUM_WORKER_API_SOCKET` on the worker
      unit **and** in CLI environments. Do not set only `worker.api_socket`
      or only `factum.socket` — they will drift.

    Generate both secrets with `openssl rand -base64 32`.

    `netbox`/`lime`/`becs` are the exception: unlike the others, they talk to
    Postgres directly instead of fetching config over the hub (see AGENTS.md's
    "Sync model" section), and default to reading `/etc/factum2/factum2.yaml`
    (the _full_ config, with a `db:` section) rather than
    `factum2-worker.yaml`. Only put them in a worker's `commands` map on the
    primary host itself, where that full config already exists at the
    default path.

3. **Register the node with the primary**: admin UI → Worker nodes → Add,
   with Address set to `host:port` matching this node's `worker.listen` and
   Token matching its `worker.token`. Paste `/etc/factum2/hub.crt` into
   **TLS CA certificate** so the primary verifies this node's hub cert
   (skip verification only in lab). Takes effect within one
   `RemoteManager` reconcile pass (~10s) — no primary restart needed.

4. **Install and start the systemd unit.** Prefer re-running
   `/etc/factum2/install.py` on the primary: it runs `groupadd -r factum`
   (idempotent) **before** copying `factum2-worker.service` (`Group=factum`)
   to each enabled worker, compares it with whatever is
   already in `/etc/systemd/system`, and asks before overwriting a modified
   file. systemd `Group=` without the group fails the unit (`Failed to
determine group credentials`) and takes **hub command dispatch** down —
   do not copy the unit until `groupadd` has run.

    Manual fallback:

    ```sh
    getent group factum >/dev/null || sudo groupadd -r factum
    sudo cp examples/factum2-worker.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now factum2-worker.service
    ```

    On Icinga hosts, add the notification UID to the group so
    `factum2-icinga-notifications` can open the socket (until then, Stat
    EACCES falls back to HTTPS). Supplementary groups take effect at
    process start, so restart the Icinga daemon (and any long-lived
    notification helper) after `usermod`, then verify as that UID before
    closing `:443`:

    ```sh
    sudo usermod -aG factum icinga    # or nagios, matching the NotificationCommand user
    sudo systemctl restart icinga2    # or nagios
    sudo -u icinga stat /run/factum2-worker/api.sock
    ```

    The socket dir is `/run/factum2-worker` (`root:factum` `0750`); the
    socket is `0660` (chmod'd by `factum2-worker start`, independent of
    umask). Connecting to it is equivalent to possessing the service token.

5. **Verify**: `/sync/status` in the web UI (or `GET /api/worker/status`)
   lists connected nodes and what they handle; `journalctl -u factum2-worker -f` on the worker host for logs. Confirm a co-located CLI
   (e.g. `factum2-librenms show-config`) hits the socket. Then, if mixed-UID
   CLIs are in group `factum` and you are not using `factum2-worker run` from
   this host, you may close worker-net → primary `:443`. Leave
   `FACTUM_WORKER_API_SOCKET=none` unset when you do — a unix 502 after Dial
   will not fail over to HTTPS.

## NetBox webhook (partial sync)

`POST /api/netbox-webhook` lets NetBox push change events instead of waiting
for `factum2-netbox sync`'s full polling sync. On a Device, Interface or IP
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

`factum2-netbox check` reads the live NetBox extras API and verifies that
setup: a webhook whose URL is `{PublicBaseURL}/api/netbox-webhook`, enabled
event rules covering Device / Interface / IP Address / Cable / Site
create+update+delete,
and reports the custom fields factum needs. Pass `--update` to create
the factum webhook (`factum-sync` at `{PublicBaseURL}/api/netbox-webhook`)
and its event rule when missing, patch drifted webhook HTTP settings,
and create missing custom fields / patch drifted label/description/group/object types
(never `required`, and never a type change: NetBox forbids that). Selection fields without a prescribed
choice list (`role`) are reported if missing; NetBox will not accept a
select field with no choices. Missing `alarm_destination` /
`alarm_timeperiod` are created with seed choices (example addresses and
SLA windows) and those lists are never updated if the field already
exists. `connection_method` gets a `ssh`/`telnet` choice set.
Integration fields (`becs_oid`, `librenms_id`, `optical_role`) are
only created when that source/destination is enabled. Creating a webhook
requires `PublicBaseURL` and the webhook secret in Settings; the secret
is write-only in NetBox, so a verify-only check only confirms factum has
one configured. Exits non-zero if anything required cannot be fixed.

## License

[AGPL-3.0-or-later](LICENSE). Copyright (c) 2026 Anders Löwinger.
