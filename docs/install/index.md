# Production install

No Go or Node is required on the host. Tagged releases ship linux/amd64
and linux/arm64 binaries, systemd units, and `install.py`.

The copy at `/etc/factum2/install.py` is only a launcher: after you pick a
tag it runs **that release's** `install.py` so install steps match those
binaries.

The same steps are in the
[repository README](https://github.com/abundo/factum2/blob/main/README.md)
if you prefer to work from a clone.

## 1. PostgreSQL

```sh
sudo -u postgres psql
create database factum2;
create user factum2_user with encrypted password '<changeme>';
grant all privileges on database factum2 to factum2_user;
alter database factum2 owner to factum2_user;
```

Schema migrations are a dedicated command (`factum2-web migrate`). They
do **not** run when the GUI or a sync CLI starts. `install.py` applies
them during install; to run them by hand, stop the GUI first:

```sh
sudo /opt/factum2/factum2-web migrate -f /etc/factum2/factum2.yaml
```

## 2. Config and installer

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
the `curl` commands.

Edit `/etc/factum2/factum2.yaml`: set `db:` credentials and `web.jwtsecret`
(`openssl rand -base64 48`). Keep `web.bind` on loopback; TLS is
terminated at a reverse proxy ([Reverse proxy](reverse-proxy.md)). Edit
`/etc/factum2/factum2-worker.yaml` for the local worker (`worker.listen`,
`worker.token`, `worker.tls_cert` / `worker.tls_key`). Almost all other
runtime settings live in the database and are edited from Admin in the GUI.

## 3. Select a release

```sh
sudo /etc/factum2/install.py
```

On a TTY the installer lists GitHub releases. Non-interactive:
`sudo /etc/factum2/install.py --install latest --yes`.

That copies binaries to `/opt/factum2`, stops `factum2-web` if it is
running, applies schema migrations, then installs systemd units:

- **this host (primary):** `factum2-web.service` and `factum2-worker.service`
- **each enabled worker node:** `factum2-worker.service`

## 4. First admin user

```sh
sudo /opt/factum2/factum2-web createadmin -f /etc/factum2/factum2.yaml
```

The process listens on `web.bind` (loopback in the example config).
Production access is through a reverse proxy that terminates TLS — see
[Reverse proxy](reverse-proxy.md). Operator documentation is in the GUI
at **Documentation** (`/doc`).

## Next

- [Reverse proxy](reverse-proxy.md) — Caddy (or Traefik / Apache) in front
  of `factum2-web`
- [Worker nodes](workers.md) — DNS, Icinga, LibreNMS, Oxidized, Prometheus
  hosts
- [Admin settings](../user/settings.md) — NetBox, Lime, destinations, JWT
  is already in YAML

From-source development is [DEV.md](https://github.com/abundo/factum2/blob/main/DEV.md),
not this page.
