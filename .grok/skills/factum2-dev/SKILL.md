---
name: factum2-dev
description: Start or reuse the factum2 laptop lab in dev/ (docker or podman compose) — reuse running containers, start stopped ones, or create with make dev-up. Use when asked to use the lab, factum-dev, compose/podman/docker containers in factum2/dev, drive NetBox/LibreNMS/Icinga/Grafana/Factum there, or /factum2-dev. Installs Chromium in a sidecar on the lab network when a browser is needed.
---

Operate the `dev/` compose lab (project `factum-dev`) through `dev/compose.sh`.
That wrapper picks `docker compose` or `podman compose`. Do not call docker
or podman directly unless a sidecar (below) is not a compose service.

Ports, logins, and dest-sync `exec` examples live in `dev/README.md` — do
not copy them here. Index: http://127.0.0.1:18080. Factum GUI:
http://127.0.0.1:18091. This is not the live instance on `:8090` and not
the isolated `run-factum2-web` GUI on `:18090`.

## Bring the lab up

```bash
.grok/skills/factum2-dev/scripts/ensure.sh
```

- Running `factum-web` → reuse (no restart, no re-seed).
- Containers exist but are stopped → `dev/compose.sh up -d`, wait for
  `/api/version`.
- Nothing there → `make dev-up` (prepare, binaries, seed). First start
  pulls images.

Do not `make dev-reset` or `compose down -v` unless the user asked to wipe
the lab. After a Go rebuild: `./install.py --compose`. After a Vue rebuild:
`make frontend` (bind-mounted into `factum-web`; no compose restart).

## Exec into a service

```bash
./dev/compose.sh exec factum-web curl -fsS http://127.0.0.1:8091/api/version
./dev/compose.sh logs -f factum-web
```

In-network hostnames are the compose service names (`factum-web`, `netbox`,
`librenms`, `icingaweb`, `grafana`, `oxidized`, `prometheus`, `dns`,
`postgres`, `mysql`). Inside the lab, Factum listens on `:8091` (published
on the host as `:18091`).

## Browser (sidecar)

`factum-web` has a 128MiB memory cap, so Chromium cannot be installed
there. `ensure-browser.sh` starts or reuses `factum-dev-browser` on the
lab network and installs Chromium if it is missing.

```bash
.grok/skills/factum2-dev/scripts/ensure-browser.sh
.grok/skills/factum2-dev/scripts/run-browser.sh <<'EOF'
nav /login
login
screenshot home
console-errors
quit
EOF
```

`run-browser.sh` takes an optional target: `factum` (default), `netbox`,
`librenms`, `icinga`, `grafana`, `oxidized`, `prometheus`, `portal`, or an
`http://...` URL. Those aliases are in-network URLs (`http://netbox:8080`,
not host `:18000`). Screenshots land in
`.grok/skills/factum2-dev/.state/screenshots/`.

`login` fills the first username/password form. Override with `ADMIN_USER`
/ `ADMIN_PASS` (LibreNMS is not the default admin/admin — see
`dev/README.md`). REPL commands: `nav`, `login`, `click`, `fill`, `press`,
`wait-for`, `screenshot`, `eval`, `console-errors`, `quit`. Chain with
`>>` and `nth=N` the same way as `run-factum2-web`. A 401 from `/api/me`
on the login page is the SPA checking the session; it is not a failure.

`make dev-down` does not remove the sidecar. Recreate with
`podman rm -f factum-dev-browser` (or `docker rm -f`) then
`ensure-browser.sh` if the lab network was rebuilt.

## Which skill

- This one: full lab, dest apps, already-running compose.
- `run-factum2-web`: throwaway GUI-only DB on `:18090`.
