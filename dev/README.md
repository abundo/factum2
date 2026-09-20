# Local development stack

Compose project `factum-dev`: the upstream systems factum talks to, sized
for a Linux laptop (~3–4 GiB RAM). Isolated from the live instance
(`:8090`, database `factum2`) and from `testdata/itest`.

Ports are published on all interfaces (lab credentials, not production).
From another machine, use this host's address in place of `127.0.0.1`.

| Role | App | Port |
| --- | --- | --- |
| Index | portal | http://127.0.0.1:18080 |
| GUI | factum-web | http://127.0.0.1:18091 |
| Source | NetBox | http://127.0.0.1:18000 |
| Dest | LibreNMS (no syslog/snmptrapd) + worker | http://127.0.0.1:18001, hub `127.0.0.1:18446` |
| Dest | Icinga Web | http://127.0.0.1:18002 |
| Dest | Oxidized + worker | http://127.0.0.1:18888, hub `127.0.0.1:18447` |
| Dest | Icinga 2 API + worker | https://127.0.0.1:15665, hub `127.0.0.1:18445` |
| Dest | Prometheus + worker | http://127.0.0.1:19090, hub `127.0.0.1:18448` |
| Dest | Grafana (devices + VMs) | http://127.0.0.1:18003 |
| Dest | Alertmanager | http://127.0.0.1:19093 |
| Dest | snmp-exporter | http://127.0.0.1:19116 |
| Dest | BIND (`lab.example`) + Kea DHCPv4 + factum-dns worker (dns + certs) | `127.0.0.1:18053`, hub `127.0.0.1:18444` |
| Worker hub | factum-worker (netbox, device-sync, storage copy) | `127.0.0.1:18443` |
| Software | factum-storage (HTTP + SFTP) | http://127.0.0.1:18088, SFTP `127.0.0.1:12222` (`factum` / `lab`) |
| Shared Postgres | factum2 + netbox DBs | `127.0.0.1:15432` |
| Shared MariaDB | librenms | `127.0.0.1:13306` |
| Shared Redis | netbox db0/db1, librenms db2 | `127.0.0.1:16379` |

Factum-web, factum-worker, and each dest's co-located factum2-worker run in
compose with the host `build/` directory bind-mounted at `/opt/factum2`.
Rebuild the primary with `./install.py --compose`. Pass `--worker` to also restart dest workers.

## Start

Needs `docker compose` or `podman compose` (override with `FACTUM_COMPOSE`).

```sh
make dev-up
```

`dev-up` builds `build/` if needed, starts databases, creates the Icinga
MariaDB DBs so Icinga Web can come up with NetBox/LibreNMS (instead of
after them), waits for those apps, migrates factum, seeds
Settings/admin/tokens (all lab features on, including the DNS zone
editor and the Software repository), registers the NetBox webhook and
custom fields (`factum2-netbox check --update`), then starts factum-web,
factum-worker, and factum-storage.
After web is up it posts **sample service definitions** (ELINE, ELAN,
POLARIX) into Catalog → Service types — Factum itself ships none. ELINE
CLI add/remove bodies come from `dev/templates/eline-*.tmpl` (written by
`prepare.py`). EOS uses a real MPLS LDP pseudowire + patch-panel apply.
Re-run `./dev/prepare.py` then `./dev/service_definitions.py` on an
already-running lab to refresh a stub EOS pack.
Each step prints elapsed seconds (`==> wait-apps +45s`). Each dest container (dns, icinga,
librenms, oxidized, prometheus) runs its own factum2-worker with that
dest's command, matching production. The dns worker also runs **certs**
(lego) and applies **Kea DHCPv4** (`192.0.2.0/24` on `br-lab-dhcp`, plus
IPAM prefixes from Destinations → DHCP). Two veth dhclients
(`veth-dhcp1` / `veth-dhcp2`) run inside the dns container. factum-worker
handles netbox and
device-sync.
NetBox starts empty. To load the upstream
[netbox-demo-data](https://github.com/netbox-community/netbox-demo-data) SQL
dump, pass `--demo` (`make dev-up SEED_ARGS=--demo`, or `./dev/seed.py --demo`
on an already-running lab). For a local inventory, copy
`dev/netbox-seed.example.yaml` to `dev/netbox-seed.yaml` (gitignored) and
edit; `seed.py` applies it via the NetBox API when that file exists, or run
`./dev/netbox-seed.sh` later.

Index of lab links: http://127.0.0.1:18080. Login: http://127.0.0.1:18091 —
`admin` / `admin`. NetBox (`:18000`), Icinga Web (`:18002`), and Grafana
(`:18003`) use the same user/pass; LibreNMS (`:18001`) is `admin` /
`Admin-lab1!` (password policy requires 8+ characters and a symbol).
Grafana's home dashboard lists devices and virtual machines the way
LibreNMS does (status, hostname, location, uptime, ports), fed by
Prometheus via snmp-exporter.

```sh
./install.py --compose              # make + migrate + restart factum-web / factum-worker / factum-storage
./install.py --compose --worker     # also restart dest workers
./install.py --compose --skip-build # restart primary only, no rebuild
```

```sh
make dev-down          # keep volumes
make dev-reset         # stop everything, wipe volumes and dest files (does not start again)
make dev-up SEED_ARGS=--demo   # also load netbox-community demo SQL
```

## NetBox inventory

Copy the example and keep the real file out of git:

```sh
cp dev/netbox-seed.example.yaml dev/netbox-seed.yaml
# edit names, sites, serials — netbox-seed.yaml is gitignored
./dev/netbox-seed.sh                 # or let seed.py apply it
./dev/netbox-seed.sh --dry-run       # print actions, no API writes
```

The example creates manufacturers (Arista, Cisco, Nokia), platforms
(EOS, IOS-XR, SROS-MD), device types 7020R / 7280R / ASR9001 with
interface templates, and device `lu17-lab-r0` with Loopback0 / Management1
addresses (Loopback0 is primary). Repeat a device-type port by name on the
device to assign `ip_addresses`; mark one IPv4 and/or IPv6 `primary: true`.

## Sync

Job overview dispatches each dest command to the worker on that dest
container. Manual CLIs:

```sh
./dev/compose.sh exec factum-worker /opt/factum2/factum2-netbox sync -f /etc/factum2/factum2.yaml
./dev/compose.sh exec icinga /opt/factum2/factum2-icinga sync
./dev/compose.sh exec librenms /opt/factum2/factum2-librenms sync
./dev/compose.sh exec oxidized /opt/factum2/factum2-oxidized sync
./dev/compose.sh exec prometheus /opt/factum2/factum2-prometheus sync
./dev/compose.sh exec dns /opt/factum2/factum2-dns sync
./dev/compose.sh exec dns /opt/factum2/factum2-certs sync
./dev/compose.sh exec dns /usr/local/sbin/lab-dhcp-clients
./dev/compose.sh exec dns ip -4 addr show veth-dhcp1
```

Dest files are local to each dest container (still bind-mounted from
`dev/data/` so `make dev-reset` can wipe them): Icinga `/factum`, Oxidized
`~/.config/oxidized`, Prometheus `/etc/prometheus/targets.json`, DNS
`/etc/dnsmgr2`, `/var/lib/bind`, and `/etc/kea`. Certificates: `/var/lib/lego`.
The lab DHCP subnet `192.0.2.0/24` is listed in administrator-managed
`dnsmgr2.yaml` for the in-container clients; do not also enable that same
prefix in IPAM (duplicate DHCP prefixes fail the DNS job).

Oxidized 0.37 exits if `router.db` has no usable nodes, which takes down
oxidized-web. `prepare.py` (and the oxidized entrypoint) write a dummy
`lab-dummy:127.0.0.1:ios` line when the file is missing or empty, and
`factum2-oxidized sync` keeps that dummy when the inventory is empty.
Factum's Oxidized browser uses `Settings.OxidizedApiURL`, seeded as
`http://oxidized:8888` so factum-web can reach it; `http://127.0.0.1:8888`
only works inside the oxidized container. Dest files under `dev/data/`
are gitignored.

Lab passwords are in `dev/.env` and are not for any other use.
