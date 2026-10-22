---
title: DNS
order: 55
---

# DNS zone editor

The DNS zone editor is optional. Turn it on under **Admin → Settings →
Factum**. Off by default. Turning it off hides the menu; it does not
delete SOA templates, DNS templates, DNSSEC policies, or zones.

This is separate from **Admin → Destinations → DNS**, which
is the device-record sync (`factum2-dns` writing a JSON records file
for devices, then applying BIND/Kea through the dnsmgr2 library).

## What you edit

| Tab | Role |
| --- | --- |
| **Zones** | Name, type (`forward` / `reverse4` / `reverse6`), DNS template, records |
| **DNS templates** | Named `dns_template`: which SOA, default TTL, NS hostnames with an optional IPv4 or IPv6 address, optional DNSSEC policy |
| **SOA templates** | MNAME, RNAME, refresh/retry/expire/minimum. Serial is not stored; dnsmgr2 assigns `YYYYMMDDnn` on sync. |
| **DNSSEC policies** | BIND `dnssec-policy` name and key/signature timings. On sync, `factum2-dns` writes the policy into the zone include. dnsmgr2 emits the block in `named.conf` and sets `dnssec-policy "…"` plus `inline-signing` on each zone whose DNS template uses it. |

A zone's SOA and apex NS come from its DNS template. Each nameserver row
is a hostname and an optional IPv4 or IPv6 address. Add another row with
the same hostname for another address. On sync, `factum2-dns` writes the
SOA template, the DNS template (default TTL, NS hostnames, DNSSEC policy),
and every DNSSEC policy into the zone include. dnsmgr2 uses that DNS
template when it writes the BIND zone file, so the zone file gets one
apex NS per hostname. An A or AAAA is added to the JSON records file only
when that hostname is inside the zone. An address on a nameserver outside
the zone is kept on the template and is not written into the zone. The
record editor is for everything else (A, AAAA, MX, TXT, …). Leave TTL
empty to use the template default.

## DHCP

Optional. Turn it on under **Admin → Destinations → DHCP**.
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
  reservation. The list-button on the MAC field opens current IPv4 and
  IPv6 Kea leases (MAC, IP, hostname); picking a row copies that MAC
  onto the record. The DNS dest worker reads leases from the Kea
  control socket (`lease4-get-all` / `lease6-get-all`, which needs the
  `lease_cmds` hook) and falls back to the memfile CSV if that command
  is not loaded.

Default DNS servers for DHCP clients are on **Destinations → DHCP**.
Kea paths, restart commands, and `host_dhcp_template` stay in the
administrator-managed `dnsmgr2.yaml`. The Kea subnet JSON
(`kea-dhcp4.dnsmgr2.json`) is still included from the main Kea config as
`"subnet4": <?include "/etc/kea/kea-dhcp4.dnsmgr2.json"?>`.

When DHCP is on and **Destinations → DHCP → Include file** names a
`worker.paths` key (usually `dns_prefixes_file`), `factum2-dns` writes
enabled prefixes into the path that key has on the worker. List that path
from the main config after `host_dhcp_template`:

    dnsmgr2:
      - host_dhcp_template: isc_kea
      - include: /etc/dnsmgr2/factum2-prefixes.yaml

Reverse zones use a prefix as the name (`192.168.0.0/16`,
`2001:db8::/32`), matching dnsmgr2.

## Include file for dnsmgr2

`/etc/dnsmgr2/dnsmgr2.yaml` is maintained by the operator (BIND/Kea
paths, sqlite serial DB, and host templates). SOA templates and DNS
templates are written by `factum2-dns`. A template in the include
replaces a template of the same name in the main file.

When the zone editor is on and **Destinations → DNS → Include file** names
a `worker.paths` key (usually `dns_zones_file`), `factum2-dns` writes the
path that key has on the worker as `soa_templates:`, `zone_templates:`,
`dnssec_policies:`, and `zones:`. Each zone carries `dnssec_policy` when
its DNS template has one. List that worker path from the main config:

    dnsmgr2:
      - host_dns_template: isc_bind
      - include: /etc/dnsmgr2/factum2-zones.yaml

The **Destination file** is a `worker.paths` key (usually `dns_dest_file`)
for the JSON records file (`sources[].type: json`, `sources[].name`). The
path, for example `/etc/dnsmgr2/factum2-records.json`, is only in the
worker YAML. An empty include-file field with the zone editor off means
"do not write a zone list" (zones stay inline in the main yaml).

## Sync

A DNS job writes the JSON records file (devices plus zone-editor
records), the zone include (SOA templates, DNS templates, DNSSEC
policies, and zones), and (if DHCP is
on) the prefix include, then `factum2-dns` applies the
administrator-managed `dnsmgr2.yaml` in-process (BIND zone files,
`rndc`, optional Kea). No separate `dnsmgr2` binary is required on the
worker. dnsmgr2 writes each DNSSEC policy at the top of the BIND include
(`named.conf.dnsmgr2`) and references it from the zone statement. A
policy named `default` is BIND's built-in policy and is not written as a
block. Zone-editor records for a zone named the same as **default
domain** are merged into that domain's `records` array with the device
records.

The DNS host still needs BIND (`named-checkzone`, `rndc`) and, if DHCP
is on, Kea. Those stay OS packages.
