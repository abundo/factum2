---
title: Admin settings
order: 60
---

# Admin settings

Administrators configure Factum from **Admin**. Most runtime settings live
in the database (this UI), not in `/etc/factum2/factum2.yaml`. The YAML
file is process config: database URL, bind address, JWT secret, worker
TLS. After install, you almost never edit YAML to turn NetBox or DNS on.

## Settings → Factum

Feature switches (all off by default except as noted):

- **Organization** — customers and contacts
- **Optical / WDM modeling** — ROADM/transponder inventory, paths,
  maintenance impact
- **IP address management** — namespaces, VRFs, prefixes

Also set the **API token** (service-to-service, not a user password),
**default domain** (used when matching short device names to FQDNs),
**public URL** (absolute links in email), and how many finished jobs
housekeeping keeps. Unfinished jobs are never deleted.

The **Email** tab is SMTP for password reset and similar. Send a test
message from that tab after filling the host.

**Dashboard** (under Settings) is the shortcut links on the home page:
name, URL, group, optional icon.

## Settings → Sources

Enable and credential **BECS**, **NetBox**, and **Lime**. NetBox also has
a webhook secret (HMAC on `POST /api/netbox-webhook`) and an option to
sync Factum customers to NetBox tenants.

A source that is disabled is skipped by [jobs](jobs.md). Credentials are
used by the corresponding sync tool, which may run on the primary
(NetBox/Lime/BECS talk to Postgres) rather than a remote worker.

## Settings → Destinations

DNS, Icinga, LibreNMS, Oxidized, and Prometheus each have an enabled
flag, destination file or API URL, and ignore lists (newline-separated).
LibreNMS delayed delete lives here. Oxidized **API URL** is what the GUI
Oxidized browser uses; it must be reachable from `factum2-web`.

These tools normally run on the destination host, talking back through a
[worker](jobs.md) — not by opening Postgres from that host.

## Worker nodes

Who the primary dials for hub transport: name, `host:port`, shared token,
optional TLS CA (paste the worker's `hub.crt`), skip-verify, enabled.
Edits take effect within about ten seconds. Address SAN must match the
certificate; there is no `ws://` fallback. This page only registers who
to dial; the worker binary, TLS cert, and `worker.commands` allowlist are
configured on the worker host.

## Device sync

Credentials and options for `factum2-device-sync` (read on-device
services into NetBox/Factum). Separate from the GUI device-interface
refresh, which uses credentials you type in the session.

## AAA

- **Users** / **Roles** — local accounts; `admin` cannot be deleted
- **Authentication** — local and LDAP (AD or generic), bind mode, TLS
- **Authorization** — map LDAP group DNs to Factum roles, plus a default
  role for users who match no group
