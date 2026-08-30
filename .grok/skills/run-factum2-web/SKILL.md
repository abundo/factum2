---
name: run-factum2-web
description: Build, launch, and drive the factum2 web GUI (Go backend + Vue SPA) in an isolated instance - use when asked to run, start, screenshot, or verify a UI change in the factum web app. Provisions its own throwaway Postgres DB and admin user so it never touches the real running instance's data.
---

Drives factum2's web GUI end to end: an isolated Postgres DB + admin user +
built frontend + backend on a private port, then a browser REPL
(`browser.mjs`) to click through it and take screenshots. All paths below
are relative to the repo root (`factum2/`), not this skill directory.

**Why isolated, not the already-running instance:** this machine usually
already has a real factum2-web instance running on `:8090`
(`/opt/factum2/factum-web start`, reading `/etc/factum2/factum2.yaml`) with
real org data - do not log into it, seed data into it, or reuse its DB.
This skill's `factum2_skilltest` DB/role and port `18090` are separate from
that, by design.

## Prerequisites

- Postgres runs in Docker. `setup.sh` defaults to container
  `postgresql-db-1` and superuser `factum2_user` (override with
  `FACTUM_PG_CONTAINER` / `FACTUM_PG_SUPERUSER`). Auth is via `docker
exec` inside the container — no database password belongs in this repo.
  The default DB `factum2` is the **real** one; don't touch it.
- `tmux` (used to satisfy `createadmin`'s interactive TTY requirement).
- `chromium-cli` is **not installed on this machine** - `browser.mjs` uses
  `playwright-core` (declared in this skill's `package.json`) driving the
  system `/usr/bin/google-chrome-stable` instead. Run `npm install` in
  this skill directory once before first use.

## Run (agent path)

```bash
cd factum2
.grok/skills/run-factum2-web/setup.sh     # idempotent - safe to re-run
node .grok/skills/run-factum2-web/browser.mjs <<'EOF'
nav /login
login
nav /sync/status
screenshot job-status
quit
EOF
.grok/skills/run-factum2-web/teardown.sh
```

`setup.sh` prints the URL/credentials when done
(`http://127.0.0.1:18090/`, `admin@local` / `skilltest123`). It also seeds
one demo `Job` with two subjob tabs of very different sizes (a `dns` task
with 40 events including a warning and an error, an `icinga` task with
just 1) - the fixture the `JobDetailModal` top-alignment fix was built to
verify - but only if the `jobs` table is empty, so it won't duplicate data
on re-runs.

### `browser.mjs` commands

A minimal `chromium-cli`-alike REPL reading one command per line from
stdin:

| Command                     | Effect                                                         |
| --------------------------- | -------------------------------------------------------------- |
| `nav <url>`                 | Navigate; bare paths resolve against `http://127.0.0.1:18090`  |
| `login`                     | Fills+submits the login form with the seeded admin creds       |
| `click <selector>`          | First match; see chaining below                                |
| `fill <selector> <text...>` |                                                                |
| `press <key>`               | e.g. `Enter`                                                   |
| `wait-for <selector>`       |                                                                |
| `screenshot [name]`         | Saved under `.grok/skills/run-factum2-web/.state/screenshots/` |
| `eval <js>`                 | Runs in page context                                           |
| `console-errors`            | Prints collected `console.error`/`pageerror` text              |
| `quit`                      |                                                                |

Chain into nested/repeated elements with `>>` and `nth=N` segments, e.g.
to click the details button in the first row of the _second_ table on a
page (see Gotchas - the Job Status page has two `<table>`s):

```
click table >> nth=1 >> tbody tr >> nth=0 >> button
```

### Representative interaction (verified this session)

Opens the seeded job's detail modal and confirms switching tabs doesn't
move the header/tab bar (the bug `JobDetailModal.vue`'s top-alignment fix
addresses):

```bash
node .grok/skills/run-factum2-web/browser.mjs <<'EOF'
nav /login
login
nav /sync/status
click table >> nth=1 >> tbody tr >> nth=0 >> button
screenshot modal-tab1
click [role=tablist] button >> nth=1
screenshot modal-tab2
click [role=tablist] button >> nth=0
screenshot modal-tab1-again
console-errors
quit
EOF
```

`modal-tab1.png` and `modal-tab1-again.png` should be pixel-identical -
that's proof the modal's top edge (and thus the clickable tab bar) doesn't
shift when tab content height changes.

## Run (human path)

Hot-reloading dev workflow against the **real** DB/instance, per `DEV.md` -
different port (`8090` + Vite on `5173`), not what this skill uses:

```bash
go run ./cmd/web migrate -f /etc/factum2/factum2.yaml
APP_ENV=development go run ./cmd/web start -f /etc/factum2/factum2.yaml -b 0.0.0.0:8090
cd web/frontend && npm run dev
```

## Gotchas

- **CLI flags**: config file is `-f` (default `/etc/factum2/factum2.yaml`),
  web subcommand is `start`. Schema changes are `migrate` (`setup.sh`
  runs it before `createadmin`). If a doc disagrees, trust
  `cmd/cmd_base.go` / `cmd/web/factum-web-cli.go`.
- **`-b`/`--bind` silently overrides `web.bind` from the config file, and
  defaults to `:8090`** if you don't pass it on the command line - even
  with `web.bind` set correctly in the YAML, omitting `-b` binds `:8090`
  and collides with the real running instance. Always pass `-b` explicitly
  (`setup.sh` does).
- **`createadmin` needs a real TTY**: it calls `term.ReadPassword`, which
  panics with `inappropriate ioctl for device` if stdin is a pipe (even a
  heredoc). `setup.sh` runs it inside a throwaway `tmux` session and
  `send-keys`s the password twice.
- **No local `postgres` OS user** - Postgres is fully containerized
  (`docker exec <postgres-container> psql ...`), there's nothing to
  `sudo -u postgres` into. When piping a heredoc into `psql` via `docker
exec`, you need `docker exec -i` - without `-i`, stdin isn't attached,
  `psql` silently receives an empty script and exits 0 as if the seed
  guard's `IF NOT EXISTS` check just found existing rows. This bit us
  once: the seed step reported success but inserted nothing.
- **`go run` isn't the actual server pid** - it execs a separate compiled
  binary as a child. Killing the `go run` wrapper's pid (or a loose
  `pkill -f` on its command line) can leave the real server listening.
  Both `setup.sh` (before restarting) and `teardown.sh` kill by port
  (`lsof -ti:$PORT`) instead.
- **The Job Status page has two `<table>` elements** (worker-node status,
  then recent jobs) - `table` alone is ambiguous; use
  `table >> nth=1 >> ...` for the jobs table.
- **Playwright's own `>> nth=N` string chaining needs a `css=` prefix on
  every segment to work reliably**, and still misbehaved when chained more
  than once in testing. `browser.mjs`'s `click`/`fill`/`wait-for` instead
  split on `>>` themselves and reduce it into `.locator()`/`.nth()` calls -
  no `css=` prefix needed in commands you type.
- **`web/static/vue` is gitignored and disposable** - rebuilding it
  (`npm run build`) never touches git state, and the real running instance
  at `:8090` is a separately installed release build with its own
  `go:embed`-ed assets, so rebuilding here can't affect it either way.

## Troubleshooting

- `listen tcp ...: bind: address already in use` from `go run ./cmd/web
start` - something's already listening on that port. `setup.sh` now
  kills any existing listener on its target port before starting, but if
  you're running the binary manually, check `lsof -ti:<port>` first.
- Seed step reports success but `GET /api/jobs` returns `[]` - almost
  certainly the missing `docker exec -i` gotcha above; confirm with
  `docker exec "$FACTUM_PG_CONTAINER" psql -U "$FACTUM_PG_SUPERUSER" -d factum2_skilltest -c
"SELECT count(*) FROM jobs;"`
  (defaults: `postgresql-db-1` / `factum2_user`).
- `createadmin` panics with `inappropriate ioctl for device` - it wasn't
  run under a TTY; use `setup.sh` (which wraps it in `tmux`) rather than
  piping a password directly into it.
