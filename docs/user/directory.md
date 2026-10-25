---
title: Directory
order: 56
---

# Directory

**Admin → AAA → Directory** is the internal LDAP directory: people, roles,
and folders. When Authentication points at an external directory, that
Active Directory or OpenLDAP tree is a second root.

## Internal directory

Select the base DN at the top of the internal tree. Enable the directory,
set the listen address (empty uses `:389`), and set the search password.
The bind DN is `cn=svc-factum,<base DN>`. NetBox, LibreNMS, Icinga, and
other applications bind with that account, then search with `(uid=%s)`.

**New folder** creates an organizational unit. Folders nest. A person or
role can sit in a folder. **Default** publishes a person under `ou=People`
and a role under `ou=Roles`.

A role can contain other roles (**Contains roles**). `member` lists the
people and roles placed directly in that role. `memberOf` on a person
lists every role above them, including through further levels. A search
can also use the Active Directory matching rule
`1.2.840.113556.1.4.1941` (`LDAP_MATCHING_RULE_IN_CHAIN`) on `member` and
`memberOf`.

The directory is read-only on the wire. Clients can bind and search.
`userPassword` is never returned; a bind checks the local password.
Accounts that sign in through an external directory, and any account
whose password is not a local hash, are left out. Anonymous bind returns
only the root DSE.

An optional LDAPS listen address serves LDAPS. **LDAPS certificate**
chooses a row from Services → Certificates. The worker reads
`<lego storage>/certificates/<domain>.crt` and the matching `.key`.
`<domain>` is that certificate's first name, with `*` written as `_`.
`worker.paths.lego_storage` on that worker must be the directory lego
uses. **Worker hub certificate** uses the worker hub pair instead.
Leave the listen address empty to keep LDAPS off. The plain and LDAPS
addresses must differ.

On each worker that should serve the directory, add an `ldap` command to
`worker.commands`. The command line is not executed. The name starts the
listener. The worker pulls the directory from the hub and keeps a copy at
`worker.ldap_state` (default `/var/lib/factum2/ldap/cache.json`). While
the hub is down it keeps serving that copy. Point each application at any
of those workers. The copy refreshes about every 30 seconds and contains
password hashes, so keep the directory mode restricted to the factum user.

**RADIUS on the same worker uses this directory** makes that worker's
RADIUS server bind to `127.0.0.1` on the plain listen port. The worker
needs both `radius` and `ldap`. Renaming a role or moving it between
folders updates the group DN stored on RADIUS policies and on
Authorization mappings. MikroTik MS-CHAPv2 still needs Active Directory.

## External directory

When LDAP is enabled under Authentication, the configured base DN is a
second tree. Active Directory is shown for browsing. OpenLDAP entries can
be added and changed when `ldap_writeback.bind_dn` and
`ldap_writeback.bind_password` are set in the config file. People need a
username, a name, and a surname. A new group needs at least one member.
