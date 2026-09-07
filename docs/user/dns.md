---
title: DNS
order: 55
---

# DNS zone editor

The DNS zone editor is optional. Turn it on under **Admin → Settings →
Factum**. Off by default. Turning it off hides the menu; it does not
delete SOA templates, DNS templates, DNSSEC policies, or zones.

This is separate from **Admin → Settings → Destinations → DNS**, which
is the device-record sync (`factum2-dns` writing A/AAAA lines for
devices, then `dnsmgr2 sync`).

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

Reverse zones use a prefix as the name (`192.168.0.0/16`,
`2001:db8::/32`), matching dnsmgr2.

## Config file for dnsmgr2

When the zone editor is on and **Destinations → DNS → Config file** is
set, `factum2-dns` writes that path as a `dnsmgr2.yaml`: host template
(BIND paths and reload commands), SOA templates, zone templates, and the
zone list. The existing **Destination file** is still the records file
(`sources[].name`). Empty config-file path means "do not overwrite a
locally maintained yaml".

BIND path fields on the same Destinations tab default to the Ubuntu
layout from dnsmgr2's example config when left blank.

## Sync

A DNS job still writes the records file (devices plus zone-editor
records) and runs `dnsmgr2 sync`. Zone-editor records for a zone named
the same as **default domain** are merged into that `$DOMAIN` section
with the device records.
