# internal/drivers

Device drivers talk to network devices directly (NETCONF, vendor JSON-RPC,
or classic SSH CLI screen-scraping) to read/write interface state, fetch
running config, and provision ELINE services. Each platform lives in its
own `driver_<platform>.go` (+ `driver_<platform>_eline.go` where ELINE is
supported) and registers itself with `registerDriver` in an `init()`, so
`NewDriver`/`SupportedPlatforms` (`driver.go`) never need to change when a
platform is added. Netbox platform names currently registered:

| Platform key(s)   | Driver                     |
| ----------------- | -------------------------- |
| `eos`             | Arista EOS                 |
| `ios-xr`          | Cisco IOS-XR               |
| `sros`, `sros-md` | Nokia SR OS (same factory) |
| `vrp`             | Huawei VRP                 |

This file is the single place that documents _how each driver works_ and
_how they differ from one another_. Per-driver Go files should only carry
comments about that file's own local implementation details - if a comment
starts comparing platforms, it belongs here instead. This is what keeps
adding a fourth, fifth, ... platform from requiring edits to every existing
driver's comments just to keep a comparison table up to date.

## Transport summary

| Platform     | Interface read/write                                                  | Exec / running-config text | Config save                               |
| ------------ | --------------------------------------------------------------------- | -------------------------- | ----------------------------------------- |
| Arista EOS   | NETCONF/OpenConfig, falls back to eAPI                                | eAPI JSON-RPC              | eAPI `copy running-config startup-config` |
| Cisco IOS-XR | NETCONF/OpenConfig (candidate+commit)                                 | SSH CLI                    | not supported (commit already persists)   |
| Nokia SR OS  | NETCONF/OpenConfig read; native `nokia-conf` write (candidate+commit) | SSH CLI (MD-CLI)           | not supported (commit already persists)   |
| Huawei VRP   | SSH CLI only                                                          | SSH CLI                    | SSH CLI `save` + `y` confirmation         |

Three of the four platforms (EOS, IOS-XR, SR OS) expose the standard
`openconfig-interfaces`/`openconfig-platform` YANG models over NETCONF, so
they share one filter/reply shape and one set of dial/edit helpers
(`openconfig.go`) instead of each parsing its own XML. VRP has no
NETCONF/JSON-RPC management API at all, so every one of its operations goes
over plain SSH CLI - the same SSH/idle-detection transport
(`sshRunCLI`/`sshRunCLIPipeline` in `openconfig.go`) that IOS-XR and SR OS
fall back to for the handful of things NETCONF can't do (arbitrary CLI
commands, CLI-text running-config, in SR OS's case also config _writes_
outside interface descriptions - see below).

## Arista EOS (`driver_arista_eos.go`, `driver_arista_eos_eline.go`)

EOS speaks two management interfaces, and this driver uses both:

- **NETCONF** (`openconfig.go`, same OpenConfig models/helpers as SR OS and
  IOS-XR) for reading interface state and editing interface descriptions.
- **eAPI** (`eapi.go`, EOS's JSON-RPC "command-api") for everything
  CLI-shaped - `Exec`, `Version`, `RunningConfigGet`, `RunningConfigSave` -
  since NETCONF has no RPC for arbitrary CLI commands, `show running-config`
  as text, or a config save, and there's no confirmed
  `openconfig-platform` chassis support on EOS the way SR OS has.

EOS's OpenConfig/NETCONF agent isn't on by default, so the two
NETCONF-backed methods (`GetInterfacesStatus`, `SetInterfaceDescriptions`)
fall back to the equivalent eAPI CLI commands when the NETCONF session
can't be established (`eapiFallback`). Whichever transport answers, the
returned values have the same shape - notably interface status, which eAPI
reports in its own camelCase vocabulary that `eapiOperStatus`/
`eapiAdminStatus` map onto the OpenConfig values the NETCONF path produces.
Both list subinterfaces (`Ethernet2.210` and friends): `show interfaces
description` naturally, the NETCONF read because the shared OpenConfig
filter selects the nested subinterfaces list too - so both transports
return the same set of names in the same order.

VLAN/switchport config (`SetInterfaceVLANs`) has no NETCONF path at all
(`openconfig-vlan` is unverified/unused in this codebase) - it always goes
over an eAPI config-mode CLI session, ensuring every referenced VLAN ID
exists in EOS's own VLAN database first (EOS rejects `switchport ...vlan`
for an undeclared VID).

`GetDeviceConfig` parses eAPI's `show running-config` JSON tree
(`eapiConfigLines`/`eapiDescend` walk it the way a hierarchical parser
would walk indented CLI text) into interfaces/VLANs/ELINE/ELAN/VRF/L3VPN.

### EOS ELINE

`ApplyELINE`/`RemoveELINE` provision an `mpls-ldp` pseudowire (for a
cross-device ELINE) plus a patch-panel entry cross-connecting this device's
subinterface to it, or directly to another local subinterface for a
same-device ELINE. The whole change runs inside an EOS "configure session"
(`eosELINESession`) so it commits atomically - either everything lands, or
the session aborts and nothing changes. Stale pseudowire/patch/subinterface
config from a previous apply of the same service is deleted first via the
`cleanup` define in `templates/eos_eline.tmpl` (ApplyELINE renders the full
template; RemoveELINE renders only `cleanup`), keyed deterministically by
service name; deleting something that was never configured is a
confirmed-benign no-op inside a configure session (unlike re-entering an
already-completed _session_ by that name, which errors - hence the
best-effort pre-cleanup in `eosELINESession` that clears any leftover
session before the real apply). Verified end-to-end against real EOS
hardware, including cross-vendor apply/parse/remove against a live SR OS
peer (`driver_eline_integration_test.go`, gated on
`FACTUM_TEST_EOS_*`/`FACTUM_TEST_SROS_*` - see DEV.md's Testing section).

## Cisco IOS-XR (`driver_iosxr.go`, `driver_iosxr_eline.go`)

IOS-XR has supported NETCONF against OpenConfig-modeled YANG paths since
well before the 6.6 train this codebase targets, so - like SR OS and EOS -
interface state/description edits go over NETCONF, reusing
`openconfig.go`'s dial/get helpers and OpenConfig XML shapes. Unlike those
two, though, XR's `<edit-config>` only accepts the candidate datastore:
edits must be locked+committed via `netconfEditConfigCandidate` rather than
applied to running directly (RFC 6241 §8.3) - `GetInterfacesStatus`/
`SetInterfaceDescription(s)` are the only methods shared as-is with the
OpenConfig plumbing; the edit path needs XR's own candidate+commit variant.

NETCONF has no RPC for arbitrary CLI commands or CLI-text running-config,
so `Exec` and `RunningConfigGet` fall back to plain SSH CLI.
`RunningConfigSave` has no XR equivalent: a NETCONF `<commit>` already
applies straight to the running datastore and persists across reload,
so there's no separate "copy running-config startup-config"-style save
step the way EOS needs.

`SetInterfaceVLANs` is unsupported: XR has no per-interface global-VLAN
concept, only bridge-domain/EVI-scoped L2 (see `Interface.SwitchportMode`'s
doc comment in `models.go`).

`GetDeviceConfig` parses `show running-config` via the shared hierarchical
CLI parser (`config_context.go`) and `tagparse.go`'s declarative
`cfg:"..."`-tagged-struct engine, rather than a hand-written regexp+switch
walk.

### IOS-XR ELINE

`ApplyELINE`/`RemoveELINE` provision an l2transport subinterface plus an
`l2vpn / xconnect group default / p2p` entry cross-connecting it to a
pseudowire toward the remote device (or directly to another local
subinterface for a same-device ELINE) - the same shape as EOS's
pseudowire+patch and SR OS's epipe+SAP. Since XR has no NETCONF RPC for
arbitrary CLI text, this goes over the classic SSH CLI
(`sshRunCLIPipeline`) inside a "configure" candidate session: commands are
pasted as one block, then `commit`, then an unconditional `abort` to leave
config mode without risking the interactive "Uncommitted changes found,
commit them?" prompt a scripted session can't answer (mirrors SR OS's
commit-then-discard shape below; XR's `abort` is the equivalent of MD-CLI's
`discard`).

`templates/iosxr_eline.tmpl` hardcodes subinterface MTU `9118` and
references a pre-existing `pw-class cw` (analogous to SR OS's
apply-groups - assumed already provisioned on the device, not created by
this driver). Cross-device p2p entries also set a human-readable
`description PEER=<name> interface=<iface>.<vlan>` from
`ELINERemotePeer.DeviceName`/`RemoteIface`/`RemoteVLAN` - the same
convention SR OS uses on its spoke-sdp, unused by EOS.

Coverage today is network-free command construction only
(`iosxrELINECommands` in `driver_iosxr_eline_test.go`): no containerlab
image and no live-device integration test for XR (unlike EOS and SR OS -
see below). **Not confirmed against real hardware** - the `pw-class cw`
assumption and `iosxrCLIErrorMarkers`'s `"% ..."` pattern are best-effort
guesses at XR's commit-failure text, not something scanned from a real
commit error.

## Nokia SR OS (`driver_nokia_sros.go`, `driver_nokia_sros_eline.go`)

SR OS has no REST/JSON-RPC management API (that's EOS's eAPI). This driver
talks NETCONF (RFC 6241, XML RPCs framed over an SSH subsystem per RFC 6242) for everything except `RunningConfigGet` and `Exec`, which shell out
over classic SSH to run CLI commands directly.

Reads (`Version`, `GetInterfacesStatus`) use the same OpenConfig-modeled
YANG paths as EOS/IOS-XR (`openconfig.go`'s dial/get helpers and OC XML
shapes), since SR OS's NETCONF agent does mirror live interface/platform
state into `openconfig-platform`/`-interfaces` "state" containers. Writes
(`SetInterfaceDescription(s)`) do **not** use OpenConfig, unlike EOS/IOS-XR:
confirmed against a real 7250 IXR (TiMOS-C-24.10.R4) that editing
`openconfig-interfaces`' "config" container commits without error but is a
disconnected shadow node never applied to the actual port - `show port
description` (and `openconfig-interfaces`' own "state" container, which
does mirror real config) stayed unchanged after the edit. The writable path
is Nokia's native YANG model instead (`urn:nokia.com:sros:ns:yang:sr:conf`,
module `nokia-conf` - the same tree the classic/MD-CLI "configure" command
walks), built by `newNokiaEditPortDescriptions`. One consequence of that
split: `GetInterfacesStatus` lists subinterfaces (the shared OpenConfig
filter selects them), but `SetInterfaceDescription(s)` can only write
ports - a subinterface name reaches the device as a `nokia-conf` port-id
that doesn't exist and is rejected, rather than being routed to a nested
OpenConfig subinterface path the way EOS's/IOS-XR's
`newOcEditInterfaces` does it.

SR OS also does not support the `:writable-running` capability (confirmed
via the device's advertised NETCONF capabilities - candidate/
confirmed-commit/rollback-on-error/validate/startup/url, no
writable-running): `edit-config` against the running datastore is rejected
outright. So, like IOS-XR, this driver needs the candidate+lock+commit
workflow (`netconfEditConfigCandidate`), not `openconfig.go`'s
direct-running `netconfEditConfig`.

`RunningConfigGet`/`Exec` disable paging (`//environment no more`) and run
the requested command over SSH; `RunningConfigSave` is unsupported for the
same reason as IOS-XR - SR OS's `<commit>` lands directly in the running
datastore, with persistence across reboot a distinct, unrelated MD-CLI step
("admin save") this driver doesn't implement. `SetInterfaceVLANs` is
unsupported: SR OS has no per-interface global-VLAN concept at all - every
VLAN-tagged construct is a SAP scoped inside a service (epipe/vpls/vprn),
not a standalone interface property.

`GetDeviceConfig` fetches `show configuration json` over SSH and walks the
resulting `map[string]any` tree with best-effort accessors
(`srosMap`/`srosSlice`/`srosStr`/`srosInt`) rather than a fixed struct,
since every field is optional depending on what's actually configured. SAP
IDs (e.g. `"1/1/1:3704.*"`) are registered as `Interface`s in their own
right (`srosAddSapInterface`, `Type: "virtual"`, `Label: "SAP"`, `Parent`
the underlying physical port) since SR OS has no real subinterfaces - the
Netbox-side interface an admin creates as a SAP's stand-in is otherwise
invisible to `InterfacesByName` and gets deleted by device-sync's
orphan-cleanup.

### SR OS ELINE

Config is pushed as MD-CLI block-paste text over the same SSH transport as
`RunningConfigGet`/`Exec` (`sshRunCLIPipeline`, one write plus one idle
wait for the whole batch - not one write-and-wait per line, which used to
make an apply take on the order of a minute and a half), inside an
`edit-config exclusive` candidate session so a failed apply can be
discarded rather than half-committed - MD-CLI's equivalent of EOS's
"configure session". See `templates/sros_eline.tmpl` for the actual command
shape, confirmed against a real device's "info inheritance" output.

Three structural differences from EOS's ELINE path worth knowing:

- **Almost nothing is set explicitly.** Both the epipe (`eline-defaults`)
  and the sdp (`sdp-default`) lean on an apply-group already provisioned on
  every device for customer/service-mtu/control-word/vc-type/
  delivery-type/ldp and every nested object's own admin-state. EOS has no
  equivalent (its pseudowire sets MTU/control-word directly via
  `ELINERemotePeer.MTU`/`ControlWord`), so those two fields are simply
  unused on this platform. Overriding one of these values today requires
  changing the apply-group or adding a per-service override leaf - there's
  no field for it on `ELINEIntent`.
- **A SAP lives entirely nested inside its owning epipe** - there's no
  separate subinterface object the way EOS's dot1q subinterface is. A
  wholesale `delete <epipe>` therefore already removes every SAP/spoke-sdp
  under it in one shot, so `ELINEIntent.StaleSubinterfaces`/
  `ELINERemoval.StaleSubinterfaces` (which exist for EOS's separate-
  subinterface cleanup) are unused by this driver.
- **A cross-device ELINE rides an MPLS SDP shared by every ELINE service**
  toward that neighbor, unlike EOS's pseudowire (one per service, owned
  outright by it) - deleting/recreating it per service would tear down
  every other customer's traffic riding the same tunnel. `srosSDPID`
  derives the SDP ID deterministically from the neighbor's last IPv4 octet
  (matching the convention already in use on real devices), and
  `ApplyELINE` only ever creates/merges the SDP, never deletes it - only
  this service's own epipe/spoke-sdp is owned and torn down by its own
  apply/remove. Because the ID is derived rather than looked up,
  `srosCheckSDPConflict` refuses to proceed if the derived ID already
  exists with a different far-end - two unrelated neighbors can share a
  last octet, and an early manual test against a real device hit exactly
  this before the check existed.

The rendered command shape has been applied and committed cleanly against
a real device (lu17-lab-r4); the one prerequisite it surfaced is that a
SAP's port needs `ethernet encap-type qinq` set first (the `"port:vlan.*"`
SAP ID format is QinQ, not plain dot1q). That's deliberately not something
`ApplyELINE` configures itself - like a physical port's admin-state/mode
generally, a customer-facing port's encapsulation is base turn-up
provisioning that should already be done before any service automation
touches it.

Testing has three tiers:

- **Network-free unit tests** (`driver_nokia_sros_eline_test.go`) pin
  command construction and helpers
  (`srosELINECommands`, `srosSDPID`, `srosSDPFarEnd`, `srosFindCLIError`)
  without dialing anything. SR OS has no containerlab image (see DEV.md's
  Testing section), so there is still no fake-SSH-server tier the way EOS
  has for eAPI.
- **Live-device integration tests** (`driver_eline_integration_test.go`,
  `//go:build integration`, gated on `FACTUM_TEST_SROS_*` - and for
  cross-vendor cases also `FACTUM_TEST_EOS_*`):
  `TestIntegrationNokiaELINERoundTrip` applies/reads back/removes against
  a real box with a fake neighbor (so `srosSDPID` can't collide with a
  pre-existing SDP); `TestIntegrationELINECrossVendor` and
  `TestIntegrationELINEReprovision` push a real EOS↔SR OS pseudowire and
  exercise re-provision cleanup. These skip when the env vars are unset.
- **`srosCLIErrorMarkers`** remains a best-effort guess at MD-CLI
  commit-failure text - candidate-mode lines don't validate until commit,
  so a syntax/semantic error (like the encap-type one above) only surfaces
  on a real apply, not a discard-before-commit dry run.

## Huawei VRP (`driver_vrp.go`)

VRP has no NETCONF/eAPI-equivalent management API available in this
environment, so every operation goes over classic SSH CLI via the shared
`sshRunCLI` transport (the same fallback transport IOS-XR and SR OS use for
the operations their platforms have no structured RPC for - VRP just uses
it for everything, having no structured RPC at all).

`Version` scrapes fields out of `display version` text - there's no
structured equivalent to ask instead. `SetInterfaceDescription(s)` and
`SetInterfaceVLANs` both build a config-mode CLI command list
(`system-view` ... `quit`), the same session shape; VLAN IDs referenced by
any target interface are declared via `vlan batch <ids>` first, since VRP
rejects `port default vlan`/`port trunk allow-pass vlan` for an undeclared
VID. VRP's read side has no dot1q-tunnel/Q-in-Q support, so neither does
the write side - callers must not pass `VLANConfig.SwitchportMode ==
"dot1q-tunnel"` for this driver.

`GetDeviceConfig` parses `display current-config` via the same hierarchical
CLI parser (`config_context.go`) and `tagparse.go` engine IOS-XR uses (with
VRP's own comment character, `#`). VRP has no ELINE/ELAN/VRF/L3VPN parsing

- only interfaces and global VLANs are populated; those maps stay empty.
  VRP also has no ELINE _write_ support at all (no `driver_vrp_eline.go`) -
  `ELINEApplier`/`ELINERemover` are deliberately separate interfaces from
  `DriverClient` for exactly this reason, so a platform without ELINE support
  never needs a stub method.

## Shared infrastructure

- **`openconfig.go`** - NETCONF session/dial helpers
  (`netconfDial`/`netconfGet`/`netconfEditConfig`/
  `netconfEditConfigCandidate`) and the OpenConfig XML shapes
  (`ocInterfacesFilter`/`ocInterfacesData`/`ocComponentsFilter`/...) shared
  by EOS, IOS-XR and SR OS, since `openconfig-interfaces`/`-platform` are
  standard YANG models rather than vendor-specific ones. Also owns the SSH/
  classic-CLI fallback transport (`sshRunCLI`, `sshRunCLIBatch`,
  `sshRunCLIPipeline`) every driver's CLI-shaped operations go through.
- **`eapi.go`** - Arista eAPI (JSON-RPC "command-api") transport, used only
  by `driver_arista_eos.go`.
- **`config_context.go`** - hierarchical (indentation-based) config-text
  parser, for platforms whose running config is CLI text organized by
  indentation (IOS-XR, VRP). EOS and SR OS don't need this: both return
  running config as JSON, which already has the tree structure.
- **`tagparse.go`** - a declarative `cfg:"..."`-tag-driven engine that fills
  a struct straight from a `config_context.go` node tree, replacing
  hand-written per-platform regexp+switch parsing. Used by IOS-XR and VRP.
- **`eline_intent.go`** - `ELINEIntent`/`ELINERemoval`, the vendor-neutral
  shapes each platform's `ApplyELINE`/`RemoveELINE` renders through its own
  template, plus the `ELINEApplier`/`ELINERemover` interfaces (deliberately
  separate from `DriverClient` so a platform without ELINE support, like
  VRP, needs no stub method).
- **`eline_template.go`** - `renderELINETemplate` /
  `renderELINETemplateDefine`, shared by every platform's ELINE file to
  turn a `text/template` (e.g. `templates/eos_eline.tmpl`) plus intent
  data into a one-command-per-line slice. Platform templates own both
  cleanup (`{{define "cleanup"}}`) and desired-state CLI so re-provision
  tear-down and full remove stay in one editable place.
- **`models.go`** - `DeviceConfig` and friends (`Interface`, `VLAN`, `VRF`,
  `ELINE`, `ELAN`, `L3VPN`, `Neighbor`), the shape every driver's
  `GetDeviceConfig`/`GetNeighbors` returns, for `internal/device-sync` to
  diff against Netbox.
