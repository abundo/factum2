# Worker nodes

A worker is a `factum2-worker start` process on a remote host — typically
the DNS, Icinga, LibreNMS, Oxidized, or Prometheus server. The primary
dials **out** to it, so the worker host only needs one inbound firewall
rule scoped to the primary's IP (`/hub` on `worker.listen`).

Co-located CLIs reach Factum's REST API through that hub via a unix
socket (`/run/factum2-worker/api.sock`). Worker networks then do not need
a route to the primary's HTTPS port. The primary still serves HTTPS to
operators and to NetBox's webhook.

```
  worker host                              management network
  ───────────                              ──────────────────
  factum2-worker :8443 /hub  <── wss:// ──  factum2-web (dials out)
  unix /run/factum2-worker/api.sock         HTTPS :443  <── operators
  CLIs ── HTTP ────────────^               HTTPS :443  <── NetBox webhook
```

`/hub` is WSS only. There is no `ws://` fallback.

## 1. Binary and group

Prefer re-running `/etc/factum2/install.py` on the primary: it copies
`factum2-worker` to each enabled node, runs `groupadd -r factum`, and
installs the systemd unit (`Group=factum`). systemd `Group=` without the
group fails the unit and takes hub dispatch down.

## 2. Config

Start from `examples/factum2-worker.yaml` on the worker host:

- `worker.listen` — bind address for `/hub`, e.g. `:8443`
- `worker.token` — shared secret; same value as **Worker nodes** → Token
  in the GUI
- `worker.tls_cert` / `worker.tls_key` — required. SAN must match the
  hostname or IP in Address (Go ignores CN)
- `worker.commands` — only the tools this host should run. Add `--job` to
  a command's `args` for structured job events
- `factum.url` / `factum.token` — HTTPS fallback if the unix socket is
  missing; omit on start-only hosts

`netbox` / `lime` / `becs` talk to Postgres directly. Only put those in
`worker.commands` on the primary, where `/etc/factum2/factum2.yaml` has
`db:`.

## 3. Register in the GUI

Admin → **Worker nodes** → Add. Address is `host:port` matching
`worker.listen`. Paste `/etc/factum2/hub.crt` into **TLS CA certificate**.
Takes effect within about ten seconds; no primary restart.

## 4. Systemd (manual fallback)

```sh
getent group factum >/dev/null || sudo groupadd -r factum
sudo cp examples/factum2-worker.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now factum2-worker.service
```

On Icinga hosts, add the notification user to group `factum` so
`factum2-icinga-notifications` can open the socket, then restart Icinga
before closing worker-net access to the primary's `:443`.

The allowlist in `worker.commands` is the security boundary: a hub
message can only select a named command, never an arbitrary shell line.

Full detail, including the unix-socket ACL and `FACTUM_WORKER_API_SOCKET`,
is in the
[repository README](https://github.com/abundo/factum2/blob/main/README.md#installing-a-worker-node).
