# Prometheus and Grafana

Factum does not poll devices. `factum2-prometheus` writes a Prometheus
[file_sd](https://prometheus.io/docs/guides/file-sd/) JSON. Prometheus
scrapes [snmp_exporter](https://github.com/prometheus/snmp_exporter), which
walks each device over SNMP.

This directory is a compose stack for that scrape: Prometheus, snmp_exporter,
Alertmanager, and Grafana. Grafana starts with a device list and a device
page (status, uptime, traffic, packets, errors, port table).

## Start the stack

From this directory:

```sh
docker compose up -d
```

Podman works the same way (`podman compose up -d`).

| Service | URL | Login |
| --- | --- | --- |
| Grafana | http://127.0.0.1:3000 | `admin` / `admin` |
| Prometheus | http://127.0.0.1:9090 | |
| Alertmanager | http://127.0.0.1:9093 | |
| snmp_exporter | http://127.0.0.1:9116 | |

Change the Grafana password before exposing port 3000. The home dashboard
is **Devices and Virtual Machines**. A device name opens **LibreNMS-style
Device**.

Prometheus targets stay empty until `prometheus/file_sd/` contains a JSON
file. `prometheus/file_sd/targets.json` ships as `[]`.

## Point Factum at the file

On the worker that runs `factum2-prometheus`, set the path in
`factum2-worker.yaml`. The value is a path on that host, and it must be the
file this compose stack mounts:

```yaml
worker:
  paths:
    prometheus_dest_file: /path/to/factum2/examples/prometheus/prometheus/file_sd/targets.json
```

Mount the directory, not the file. `factum2-prometheus` writes
`targets.json.tmp` and renames it into place. Renaming onto a single-file
bind mount returns `EBUSY`.

In Factum, Admin → Settings → Prometheus:

- Enable Prometheus.
- Destination file: leave blank to use the `prometheus_dest_file` key above.
- snmp_exporter auth: leave blank to use `public_v2` (community `public`,
  defined in the exporter image). To use another community, add a new name
  in `snmp/auths.yml` (the sample is `factum_v2`) and put that name here.
  A second config file cannot replace `public_v2`; the exporter rejects
  the duplicate key.
- snmp_exporter module: leave blank. Blank means `if_mib`. That name is
  stored as a label. The scrape modules are fixed in `prometheus/prometheus.yml`
  (`if_mib`, `system`, `hrDevice`, `hrStorage`). Do not relabel the module
  label onto `__param_module`, or the extra modules are dropped.
- Reload URL: optional. `http://127.0.0.1:9090/-/reload` when Prometheus is
  on the same host. The stack runs with `--web.enable-lifecycle`. Leave the
  URL empty to wait for the file_sd refresh (30s).

Devices are included when they are enabled, have a primary IPv4, have the
NetBox custom field `monitor_grafana`, and are not listed in the ignore
boxes. The target address is that IPv4. The `device` label is the Factum
name, with the default domain appended when the name has no dot. `instance`
in Prometheus is the IP.

Check a scrape before waiting for Grafana:

```sh
curl -fsS 'http://127.0.0.1:9116/snmp?target=192.0.2.10&module=if_mib&module=system&auth=public_v2' | head
```

`up{job="snmp"}` in Prometheus is 1 when the walk succeeded. Grafana's
device list fills on the next refresh.

A hand-written target, for a device that is not in Factum yet:

```json
[
  {
    "targets": ["192.0.2.10"],
    "labels": {
      "auth": "public_v2",
      "device": "edge-1.example.com",
      "site": "Stockholm",
      "role": "edge",
      "manufacturer": "Arista",
      "model": "DCS-7050SX3",
      "platform": "EOS"
    }
  }
]
```

`auth` is the auth name (`public_v2` from the image, or a name added in
`snmp/auths.yml`). It is not the community string. Virtual machines synced
by Factum get `vm: "true"`.

## SNMP community

The image auth `public_v2` uses community `public`. That matches Factum
when the auth setting is blank.

`snmp/auths.yml` is loaded beside the image config and can only add auth
names. `factum_v2` is the sample. Change its `community`, set Factum's
snmp_exporter auth to `factum_v2`, and restart the exporter
(`docker compose restart snmp-exporter`). The next sync writes
`"auth": "factum_v2"` on each target, and Prometheus sends that name to
the exporter.

Do not replace `/etc/snmp_exporter/snmp.yml`. The image file is generated.
A generator-style file (`walk: [sysUpTime, interfaces]`, `lookups:`,
`overrides:`) is rejected and produces no `ifOperStatus` or `ifHCInOctets`.

`system` supplies `sysUpTime` (hundredths of a second; dashboards divide by
100) and `sysDescr`. `if_mib` supplies interface status and traffic.
`hrDevice` and `hrStorage` supply CPU and memory on hosts and appliances
that implement HOST-RESOURCES-MIB. A panel stays empty when the device does
not have that MIB.

Optional modules, added in `prometheus/prometheus.yml` under `params.module`,
when a vendor or server graph is empty:

| Module | Use |
| --- | --- |
| `cisco_device` | Cisco CPU (`cpmCPUTotal1minRev`) |
| `juniper` | Junos CPU and environment |
| `ucd_la_table`, `ucd_memory` | Linux net-snmp load and memory |
| `arista_sw` | Arista-specific counters |

Walks get slower as modules are added. An unknown module name fails the
scrape.

## When the container cannot reach the devices

The exporter is on the compose network and dials each target from inside
the container. If device addresses are only reachable from the host
network namespace, give snmp-exporter the host network and point Prometheus
at it:

```yaml
# snmp-exporter service
network_mode: host
# drop the ports: section; the process listens on the host's :9116
```

In `prometheus/prometheus.yml`, change the snmp job's last relabel
`replacement` to `host.docker.internal:9116`, and add to the prometheus
service:

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

The self-metrics job `snmp_exporter` needs the same address.

## Alerts

`prometheus/alerts.yml` pages on a failed scrape, on an admin-up interface
that is operationally down, and on a sustained error rate. Shutdown ports
are not paged. A down device inhibits its interface alerts.

Alertmanager starts with a receiver that stores alerts and sends nothing.
Uncomment `slack_configs` in `alertmanager/alertmanager.yml`, or add
another receiver, then restart Alertmanager.

## Other dashboards

The provisioned boards are `grafana/dashboards/devices.json` and
`device.json`. To start from the Grafana SNMP mixin instead (fleet plus
one device, including Cisco and Juniper CPU fallbacks), import the JSON
from
<https://github.com/monitoring-mixins/website/tree/master/assets/snmp/dashboards>.
Those boards filter on `instance` (the IP). This stack's boards filter on
the Factum `device` label.

Linux servers are a better fit for node_exporter and Grafana dashboard
[1860](https://grafana.com/grafana/dashboards/1860-node-exporter-full/)
when the host can run an agent. This stack only scrapes SNMP.
