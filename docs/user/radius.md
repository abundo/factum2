---
title: RADIUS
order: 57
---

# RADIUS

**Admin → Services → RADIUS** lets routers and switches log in with a username
and password from Active Directory or OpenLDAP. The password check and the
group check happen on the RADIUS worker, against the two directory servers
configured under **Authentication**. factum2 stores the policy and the
login log. It is not in the path of a login, so a factum2 outage does not
lock operators out of the network.

## What is allowed

A login succeeds only when all of these are true:

- The switch's source address is an enabled RADIUS client, and the packet
  carries a valid Message-Authenticator.
- The password is accepted by a directory bind (PAP). The directory keeps
  the password hash; the worker never reads it.
- The switch's address belongs to a device synced from NetBox.
- The device is active.
- The user's `memberOf` groups include a group mapped to that device's
  NetBox role, or a group marked **All devices**.

Someone in a WDM group can log in to devices whose role is WDM. Someone in
an all-devices group can log in to every role. A correct password in the
wrong group is rejected. An address factum2 does not know is rejected.

Role names match the device role synced from NetBox, ignoring case. The
Groups tab lists the roles currently in inventory.

## Worker

On the host that should answer the switches, add a `radius` command to
`worker.commands` (the command line is not executed; the name starts the
UDP listener). Register that host as a worker node. Switches send
Access-Request to it on UDP 1812, or whatever listen address is set on the
Service tab.

The worker copies the client list, the group rules, the device addresses,
and the directory settings to disk (`worker.radius_state`, default
`/var/lib/factum2/radius/cache.json`) and refreshes that copy while the hub
is up. While factum2 is down it keeps using the last copy. Accept and
reject lines are sent to the Log tab when the hub is up, and held on the
worker until then.

The directory bind account and the shared secrets are in that file. Keep
the directory mode restricted to the factum user.

## Switch side

Cisco, Arista, Nokia, Huawei, and Smartoptics logins use PAP. Enable
Message-Authenticator on the switch. Give each switch the shared secret of
its RADIUS client row, and make sure the address it sends from is both
that client address and an address on the device in NetBox (primary or any
interface address).

MikroTik management logins (Winbox, SSH, WebFig, API) send MS-CHAPv2
instead of the password. That check only works when the directory is
Active Directory. Create a computer account, then fill **AD computer
account**, its password, and the NetBIOS domain on the Service tab. The
worker asks a domain controller to verify the response, then still
requires the LDAP group for the device role. The built-in accept
attributes include `MikroTik-Group = write`; change `write` to the
RouterOS group you want, such as `full` or `read`. OpenLDAP cannot check
MS-CHAPv2, so those attempts are rejected.

## Privilege attributes

An accepted login also returns the **Accept attributes** from the Service
tab. The built-in set is what a typical FreeRADIUS `users` file sends so
the switch opens an admin shell:

- Smartoptics `Userrole1` = `admin`
- `Service-Type` = `NAS-Prompt-User`
- Cisco and Arista `shell:priv-lvl=15`, plus Arista `shell:roles=network-admin`
- Nokia SR OS access `both` and netconf (`4`), profile `administrative`,
  default action `permit-all`
- Huawei exec privilege `3`

A box ignores attributes from other vendors. Comment a line with `#` to
stop sending it. Until you save a change, the built-in set is what is
sent.
