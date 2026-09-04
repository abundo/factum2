# Factum

Factum tracks network infrastructure (devices, customers, services) and
syncs it with external systems of record. NetBox, Lime CRM and BECS sync
**into** Factum; DNS, Icinga, LibreNMS, Oxidized and Prometheus are synced
**from** Factum.

This site is the public documentation. The same operator pages are also
built into the web GUI at **Documentation** (`/doc`), so an installed
release always shows the docs that shipped with that binary.

## Using Factum

- [Overview](user/index.md)
- [Devices](user/devices.md)
- [Services](user/services.md)
- [Config](user/config.md)
- [Jobs](user/jobs.md)
- [Admin settings](user/settings.md)
- [Software bill of materials](user/sbom.md)

## Install

- [Production install](install/index.md)
- [Reverse proxy](install/reverse-proxy.md)
- [Worker nodes](install/workers.md)

Developer setup and architecture notes stay in the repository:
[DEV.md](https://github.com/abundo/factum2/blob/main/DEV.md) and
[AGENTS.md](https://github.com/abundo/factum2/blob/main/AGENTS.md).

Source: [github.com/abundo/factum2](https://github.com/abundo/factum2).
