# Worker nodes

A worker is a `factum2-worker start` process on a remote host — typically
the DNS, Icinga, LibreNMS, Oxidized, or Prometheus server. The primary
dials **out** to it, so the worker host only needs one inbound firewall
rule scoped to the primary's IP (`/hub` on `worker.listen`).

Co-located CLIs reach Factum's REST API through that hub via a unix
socket (`/run/factum2-worker/api.sock`). Worker networks then do not need
a route to the primary's HTTPS port. The primary host still serves HTTPS
to operators and to NetBox's webhook, via a
[reverse proxy](reverse-proxy.md) in front of `factum2-web`.

```
  worker host                              management network
  ───────────                              ──────────────────
  factum2-worker :8443 /hub  <── wss:// ──  factum2-web (dials out)
  unix /run/factum2-worker/api.sock         HTTPS :443  <── operators
  CLIs ── HTTP ────────────^               HTTPS :443  <── NetBox webhook
```

`/hub` is WSS only. There is no `ws://` fallback.

## SSH login for upgrades

`install.py` on the primary upgrades each enabled worker over SSH as
`factum` (`--ssh-user` / `$SSH_USER`), not as root. Root login is
refused. Copies into `/opt/factum2` and `/etc`, `systemctl`, and
`groupadd` run with `sudo -n` (no password prompt).

Once on each worker, using an admin login that can create the account:

```sh
getent group factum >/dev/null || sudo groupadd --system factum
id factum >/dev/null 2>&1 || sudo useradd --create-home --home-dir /home/factum --gid factum --shell /bin/bash factum
sudo install -m 700 -d /home/factum/.ssh
sudo install -m 600 /dev/null /home/factum/.ssh/authorized_keys
# primary's deploy key (the key install.py will use; often root on the primary)
sudo tee -a /home/factum/.ssh/authorized_keys
sudo chown -R factum:factum /home/factum/.ssh
sudo install -m 440 /path/to/factum2-install.sudoers /etc/sudoers.d/factum2-install
sudo visudo -cf /etc/sudoers.d/factum2-install
```

The sudoers file is `examples/factum2-install.sudoers` in the repo and
the release tarball, and
`/usr/share/doc/factum2/factum2-install.sudoers` in the deb. It is
root-equivalent (`NOPASSWD: ALL`) so upgrades stay non-interactive.
`PermitRootLogin` can stay off. Check from the primary:

```sh
ssh -o BatchMode=yes -o ConnectTimeout=10 factum@worker-host sudo -n true
```

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

To share SSH CLI sessions with `factum2-web` / device-sync / the driver
CLI from a host that can reach the boxes, enable the opt-in
[`factum2-driver` session daemon](ssh-session.md) on this node. Do not
put `factum2-driver start` in `worker.commands`.

To store NOS images on this host, enable
[`factum2-storage`](storage.md) and add a `worker.commands.storage` entry
that runs `/opt/factum2/factum2-storage` (no args). Do not put
`factum2-storage start` in `worker.commands`.

Full detail, including the unix-socket ACL and `FACTUM_WORKER_API_SOCKET`,
is in the
[repository README](https://github.com/abundo/factum2/blob/main/README.md#installing-a-worker-node).
