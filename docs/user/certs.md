---
title: Certificates
order: 45
---

# Certificates

Factum can request TLS certificates with [lego](https://github.com/go-acme/lego)
using the **DNS-01** challenge and **RFC2136** dynamic updates.

Enable **Certificates** under Admin → Destinations. That shows the
Certificates menu and includes **certs** in [jobs](jobs.md).

## What is stored

- **Certificates** — name and list of domains (SAN). Optional overrides for
  certificate **key type** and **enable Common Name**. Empty override uses
  Destinations → Certificates defaults.
- **Accounts** — ACME account (email, server URL, account key type, ToS,
  optional EAB).
- **Challenges** — DNS-01 / RFC2136: nameserver, TSIG key/secret/algorithm,
  resolvers, extra env vars.

Distribution of issued files and restarting services is **out of scope**.
`factum2-certs` only writes lego config and runs lego.

## Sync

`factum2-certs sync` (worker role `certs`) fetches config from the primary,
writes:

- `.lego.yaml` at **lego YAML path**
- `.env` at **.env path** (RFC2136 credentials and extra env)

then runs the **lego binary** in the YAML directory. Install
[lego](https://go-acme.github.io/lego/) on that host.

Worker `commands` example:

```yaml
certs:
  cmd: /opt/factum2/factum2-certs
  args: ["sync", "--job"]
```
