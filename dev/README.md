# Local development stack

Compose project `factum-dev`: the upstream systems factum talks to, sized
for a Linux laptop (~3–4 GiB RAM). Isolated from the live instance
(`:8090`, database `factum2`) and from `testdata/itest`.

Ports are published on all interfaces (lab credentials, not production).
From another machine, use this host's address in place of `127.0.0.1`.

| Role | App | Port |
| --- | --- | --- |
| Index | portal | http://127.0.0.1:18080 |
| GUI | factum-web | http://127.0.0.1:8091 |
| Source | NetBox | http://127.0.0.1:18000 |
| Dest | LibreNMS (no syslog/snmptrapd) | http://127.0.0.1:18001 |
| Dest | Oxidized | http://127.0.0.1:18888 |
| Dest | Icinga 2 API | https://127.0.0.1:15665 |
| Dest | BIND (`lab.example`) | `127.0.0.1:18053` |
| Worker hub | factum-worker | `127.0.0.1:18443` |
| Shared Postgres | factum2 + netbox DBs | `127.0.0.1:15432` |
| Shared MariaDB | librenms | `127.0.0.1:13306` |
| Shared Redis | netbox db0/db1, librenms db2 | `127.0.0.1:16379` |

Factum-web and factum-worker run in compose with the host `build/` directory
bind-mounted at `/opt/factum2`. Rebuild with `./install.py --compose`.

## Start

Needs `docker compose` or `podman compose` (override with `FACTUM_COMPOSE`).

```sh
make dev-up
```

`dev-up` builds `build/` if needed, waits for NetBox/LibreNMS/…, migrates
factum, seeds Settings/admin/tokens, registers the NetBox webhook and custom
fields (`factum2-netbox check --update`), then starts factum-web and factum-worker.
NetBox is populated from [netbox-demo-data](https://github.com/netbox-community/netbox-demo-data)
(SQL dump for this image's minor version, cached under `dev/data/netbox/`).
First postgres volume init loads it; `seed.sh` restores if the netbox DB is
still empty. `make dev-reset` reloads a fresh dump.

Index of lab links: http://127.0.0.1:18080. Login: http://127.0.0.1:8091 —
`admin` / `admin`. NetBox (`:18000`) and LibreNMS (`:18001`) use the same
user/pass.

```sh
./install.py --compose              # make + migrate + restart factum services
./install.py --compose --skip-build # restart only
```

```sh
make dev-down          # keep volumes
make dev-reset         # wipe volumes and start again
```

## Sync

Sync CLIs run inside `factum-worker` (Job overview, or):

```sh
./dev/compose.sh exec factum-worker /opt/factum2/factum2-netbox sync -f /etc/factum2/factum2.yaml
./dev/compose.sh exec factum-worker /opt/factum2/factum2-icinga sync
```

Dest files are `/data/...` inside the worker, bind-mounted from `dev/data/`.

Oxidized exits if `router.db` has no nodes, so `prepare.sh` writes a dummy
`lab-dummy:127.0.0.1:ios` line when the file is missing. `factum2-oxidized
sync` replaces that file. Dest files under `dev/data/` are gitignored.

Lab passwords are in `dev/.env` and are not for any other use.
