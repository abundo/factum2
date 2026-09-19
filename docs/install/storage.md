# Software storage daemon

`factum2-storage` holds NOS images and serves them to devices. It can run
on the primary or on any host that already runs `factum2-worker`.

The GUI never opens the repository directory itself. File operations go
through the worker hub (or a unix socket when the daemon is co-located
with `factum2-web`). Copy-to-device is a `worker.commands.storage`
invocation so logs stream like other worker jobs.

## 1. Enable in the GUI

Admin → Settings → Factum → Software. Turn **Software repository** on.
Set the repository directory (on the storage host) and the device-facing
HTTP/TFTP/SFTP listen addresses. **HTTP URL** and **TFTP host** are what
devices put in `copy http://…` / `tftp://…` — they must be reachable from
the network devices, not from the operator browser.

## 2. systemd

`install.py` copies `factum2-storage.service` on the primary and on
workers but does **not** enable it.

```sh
sudo mkdir -p /var/lib/factum2/storage
sudo systemctl enable --now factum2-storage
```

The unit uses `/etc/factum2/factum2-worker.yaml` (same as
`factum2-driver`). TFTP on port 69 needs `CAP_NET_BIND_SERVICE` (already
in the example unit).

Optional YAML:

```yaml
storage:
  socket: /run/factum2-storage/api.sock
```

Empty uses that default. `none` disables the unix API (GUI file ops will
fail unless factum-web can still reach a worker with the storage role).

## 3. Worker command

On the storage host, `worker.commands` must include `storage` so the
node advertises that role and can run copy:

```yaml
worker:
  commands:
    storage:
      cmd: /opt/factum2/factum2-storage
      args: []
```

Restart `factum2-worker` after editing. The daemon (`start`) is a
separate process; do not put `factum2-storage start` in
`worker.commands`.

## 4. Device access

Allow devices to reach the storage host on the HTTP/TFTP/SFTP ports you
configured. Device login for copy uses Admin → Device sync credentials
(exact device name, else `default`).
