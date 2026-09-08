---
title: DNS
order: 55
---

# DNS zone editor

The DNS zone editor is optional. Turn it on under **Admin → Settings →
Factum**. Off by default. Turning it off hides the menu; it does not
delete SOA templates, DNS templates, DNSSEC policies, or zones.

This is separate from **Admin → Settings → Destinations → DNS**, which
is the device-record sync (`factum2-dns` writing a JSON records file
for devices, then applying BIND/Kea through the dnsmgr2 library).

## What you edit

| Page | Role |
| --- | --- |
| **SOA templates** | MNAME, RNAME, refresh/retry/expire/minimum. Serial is not stored; dnsmgr2 assigns `YYYYMMDDnn` on sync. |
| **DNS templates** | Named `dns_template`: which SOA, default TTL, NS hostnames, optional DNSSEC policy |
| **DNSSEC policies** | BIND `dnssec-policy` name and key/signature timings. dnsmgr2 writes the **name** into `named.conf` (`dnssec-policy "…"`). |
| **Zones** | Name, type (`forward` / `reverse4` / `reverse6`), DNS template, records |

A zone's SOA and apex NS come from its DNS template. The record editor
is for everything else (A, AAAA, MX, TXT, …). Leave TTL empty to use the
template default.

## DHCP

Optional. Turn it on under **Admin → Settings → Destinations → DHCP**.
Off by default. Turning it off hides the IPAM DHCP fields and the
zone-editor MAC column; it does not delete stored values.

When it is on:

- Each allocated prefix in IPAM can enable a DHCP server,
  with a dynamic range (must sit inside the prefix), a default gateway
  (empty = first usable address in the prefix), and DNS servers (empty =
  the Destinations → DHCP default DNS server list).
- A/AAAA records in the zone editor gain a **MAC** column. That is a
  DHCP host reservation, not a DNS comment. `factum2-dns` writes it as
  a `mac` field on the JSON A/AAAA record so dnsmgr2 can emit a Kea
  reservation.

Default DNS servers for DHCP clients and Kea paths (config dir, include
file, restart command) are on **Destinations → DHCP**. The include file is
a JSON array of subnets (typically `kea-dhcp4.dnsmgr2.json`); the main Kea
config must include it as `"subnet4": <?include "/etc/kea/kea-dhcp4.dnsmgr2.json"?>`.

When DHCP is on and **Destinations → DNS → Config file** is set,
`factum2-dns` includes `dhcp:` / `host_dhcp_template` / prefixes in the
generated `dnsmgr2.yaml`.

Reverse zones use a prefix as the name (`192.168.0.0/16`,
`2001:db8::/32`), matching dnsmgr2.

## Config file for dnsmgr2

When the zone editor or DHCP is on and **Destinations → DNS → Config
file** is set, `factum2-dns` writes that path as a `dnsmgr2.yaml`: host
template (BIND paths and reload commands), SOA templates, zone
templates, the zone list, and (if DHCP is on) Kea host template, global
DNS servers, and enabled prefixes. The existing **Destination file** is
the JSON records file (`sources[].type: json`, `sources[].name`). Empty
config-file path means "do not overwrite a locally maintained yaml"
(that yaml must use `type: json` if the dest file is JSON).

BIND path fields on the same Destinations tab default to the Ubuntu
layout from dnsmgr2's example config when left blank.

## Sync

A DNS job still writes the JSON records file (devices plus zone-editor
records) and the `dnsmgr2.yaml` config, then `factum2-dns` applies them
in-process (BIND zone files, `rndc`, optional Kea). No separate `dnsmgr2`
binary is required on the worker. Zone-editor records for a zone named
the same as **default domain** are merged into that domain's `records`
array with the device records.

The DNS host still needs BIND (`named-checkzone`, `rndc`) and, if DHCP
is on, Kea. Those stay OS packages.
