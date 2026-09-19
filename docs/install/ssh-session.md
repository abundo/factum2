# SSH session daemon

`factum2-driver start` is an **opt-in** process that owns SSH CLI sessions
so `factum2-web`, `factum2-device-sync`, and one-shot `factum2-driver`
commands can share one PTY per device. The installer copies
`factum2-driver.service` onto the primary and onto worker/jump hosts but
does **not** enable it (the process holds device passwords in RAM).

This is not the worker hub. Do not add `factum2-driver` to
`worker.commands`.

## Enable on the primary

The primary can reach the boxes. Unix socket only; no `driver.token`:

```sh
sudo systemctl enable --now factum2-driver
```

That runs `/opt/factum2/factum2-driver -f /etc/factum2/factum2-worker.yaml start`
and binds `/run/factum2-driver/session.sock` (`0660`, group `factum`).
Existing worker YAML is enough. Callers probe that path with no extra
config (`driver.socket` omitted). If the daemon is down, each `Run` falls
back to the in-process pool (connect failure only; HTTP 502 is not
replayed).

Relocate both listen and probe with `driver.socket` or
`FACTUM_DRIVER_SESSION_SOCKET`. `socket: none` disables unix.

## Jump host

The primary cannot dial some devices. On a host that can:

1. Install as a worker (the installer already copies the unit).
2. `sudo systemctl enable --now factum2-driver`
3. Set a TCP listen. Non-loopback requires TLS 1.2+, `driver.token`, and
   `driver.allow_cidrs` (device IPs the daemon may `ssh.Dial`, every A/AAAA).

```yaml
# on the jump host (factum2-worker.yaml)
driver:
  listen: 192.0.2.10:8092
  token: <shared secret>
  tls_cert: /etc/factum2/driver.crt
  tls_key: /etc/factum2/driver.key
  allow_cidrs: ["10.0.0.0/8"]
```

Point **each caller** at the daemon (`session_url` skips the unix probe):

```yaml
# factum2.yaml (web) and/or factum2-worker.yaml (device-sync / CLI)
driver:
  session_url: https://jump.example:8092
  session_token: <same secret>
  tls_ca: /etc/factum2/driver-ca.crt   # optional; system CAs otherwise
```

The session client always verifies TLS (`tls_ca` optional). It never sets
`InsecureSkipVerify`. Loopback TCP (`listen: 127.0.0.1:8092`) needs a
token but not TLS.

## Rollback

Stop the unit, or set `driver.platforms: []` / `[none]` in the YAML that
binary reads. No database migration.
