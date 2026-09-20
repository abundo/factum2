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

Default DNS servers for DHCP clients are on **Destinations → DHCP**.
Kea paths, restart commands, and `host_dhcp_template` stay in the
administrator-managed `dnsmgr2.yaml`. The Kea subnet JSON
(`kea-dhcp4.dnsmgr2.json`) is still included from the main Kea config as
`"subnet4": <?include "/etc/kea/kea-dhcp4.dnsmgr2.json"?>`.

When DHCP is on and **Destinations → DHCP → Include file** is set,
`factum2-dns` writes enabled prefixes into that YAML. List it from the
main config after `host_dhcp_template`:

    dnsmgr2:
      - host_dhcp_template: isc_kea
      - include: /etc/dnsmgr2/prefixes.yaml

Reverse zones use a prefix as the name (`192.168.0.0/16`,
`2001:db8::/32`), matching dnsmgr2.

## Include file for dnsmgr2

`/etc/dnsmgr2/dnsmgr2.yaml` is maintained by the operator (BIND/Kea
paths, sqlite serial DB, SOA templates, zone templates, host templates).
Zone template names in the Factum zone editor must match names in that
file.

When the zone editor is on and **Destinations → DNS → Include file** is
set, `factum2-dns` writes that path as a YAML zone list (`zones:`). List
it from the main config:

    dnsmgr2:
      - host_dns_template: isc_bind
      - include: /etc/dnsmgr2/zones.yaml

The **Destination file** is still the JSON records file
(`sources[].type: json`, `sources[].name`). An empty include-file path
means "do not write a zone list" (zones stay inline in the main yaml).

## Sync

A DNS job writes the JSON records file (devices plus zone-editor
records), the zone include, and (if DHCP is on) the prefix include, then
`factum2-dns` applies the administrator-managed `dnsmgr2.yaml` in-process
(BIND zone files, `rndc`, optional Kea). No separate `dnsmgr2` binary is
required on the worker. Zone-editor records for a zone named the same as **default
domain** are merged into that domain's `records` array with the device
records.

The DNS host still needs BIND (`named-checkzone`, `rndc`) and, if DHCP
is on, Kea. Those stay OS packages.
