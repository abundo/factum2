---
title: Overview
order: 10
---

# Overview

Factum is a hub, not a source of truth for everything. Upstream systems
(NetBox, Lime CRM, BECS) sync **into** Factum's database. Downstream
systems (DNS, Icinga, LibreNMS, Oxidized, Prometheus) are generated
**from** Factum.

These pages describe the web GUI. They ship inside the `factum2-web`
binary, so they match the version you are logged into — not whatever is
currently on GitHub.

## What you will see

| Area | Menu | Typical use |
| --- | --- | --- |
| Dashboard | Home | Welcome plus admin-configured shortcut links |
| Customers / Contacts | Organization | Only if **Organization** is enabled under Admin → Settings → Factum |
| Network map / Devices / Oxidized | Devices | Inventory, topology, config backups |
| Prefixes | IPAM | Only if **IP address management** is enabled |
| Services / Config / Maintenance | Provisioning | Capacity services, CLI templates, optical maintenance |
| Job overview / status / scheduler | Jobs | Trigger and watch syncs |
| Settings, users, workers | Admin | Administrators only |

Some entries appear only when the matching feature is on (Oxidized,
optical, IPAM, organization). Turning a feature off hides the UI; it does
not delete stored data.

## Roles

A user with no role only sees the dashboard, their profile, and this
documentation. **Viewer** can read; **operator** can write; **admin** can
change settings, users, and workers.

## Related pages

- [Devices](devices.md)
- [Services](services.md)
- [Config](config.md)
- [Jobs](jobs.md)
- [Admin settings](settings.md)
