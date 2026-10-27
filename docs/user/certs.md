---
title: Certificates
order: 45
---

# Certificates

Factum can request TLS certificates with [lego](https://github.com/go-acme/lego)
using the **DNS-01** challenge and **RFC2136** dynamic updates.

Enable **Certificates** under Admin → Destinations. That shows
**Services → Certificates**. Certificates, accounts, and challenges are tabs
on that page. The feature also includes **certs** in [jobs](jobs.md).

## Import

**Import .lego.yaml** reads an existing [lego](https://go-acme.github.io/lego/advanced/file-configuration/)
configuration file. Accounts, RFC2136 DNS-01 challenges, and certificates are
added or updated by name. Rows that are not in the file are left as they are.

An optional `.env` next to the YAML fills RFC2136 credentials (nameserver,
TSIG, TTL) on every imported challenge. Without that file, credentials already
stored in Factum are kept. Challenges other than RFC2136 DNS-01 are skipped.
HTTP-01, TLS-ALPN-01, and a certificate that uses a non-RFC2136 challenge are
rejected.

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

`factum2-certs sync` (worker role `certs`) fetches config from the primary.
The lego YAML, .env, binary, and storage fields are `worker.paths` keys
(`lego_yaml`, `lego_env`, `lego_bin`, `lego_storage`). The worker writes:

- `.lego.yaml` at the path for **lego YAML**
- `.env` at the path for **.env file** (RFC2136 credentials and extra env)

then runs the binary named by **lego binary** in the YAML directory. Install
[lego](https://go-acme.github.io/lego/) on that host and set the paths in
`factum2-worker.yaml`.

Worker `commands` example:

```yaml
certs:
  cmd: /opt/factum2/factum2-certs
  args: ["sync", "--job"]
```
