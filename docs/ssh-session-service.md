# Persistent SSH session service for slow device logins

| Field | Value |
| ----- | ----- |
| Status | Draft |
| Author | Factum |
| Date | 2026-09-09 |
| Audience | Factum maintainers (drivers, web, device-sync, factum2-driver) |
| Related | `internal/drivers/openconfig.go`, `internal/drivers/README-DRIVERS.md`, `AGENTS.md` (Device SSH policy), `docs/cfgmgmt-tree-objects.md` (CLI push path), `docs/install/workers.md` (hub is a different thing) |

## Overview

Every SSH CLI operation in Factum today opens a fresh interactive PTY, drains the login banner with a 5-second idle window, runs the command list, and throws the session away (`sshRunCLI` / `sshRunCLIBatch` / `sshRunCLIPipeline` in `internal/drivers/openconfig.go`). On a Huawei 5100-class VRP box the handshake plus MOTD plus that idle drain dominate wall time: a `display version` that takes a second of CLI work costs on the order of 20 seconds end-to-end, and a GUI click that issues two driver calls pays it twice. `AGENTS.md` already states the intended policy — *reuse one SSH connection per device for the process lifetime* — but the helpers violate it.

This design keeps platform drivers as they are (command construction, paging preambles, output parsing, `CLISessionApplier`) and moves **session ownership** behind a process-lifetime pool. Phase 1 is an in-process pool inside `internal/drivers`, which is enough for the web GUI and for `GetDeviceConfig`+`GetNeighbors` inside one `factum2-device-sync` run. Phase 2 exposes that same pool from a long-lived `factum2-driver start` daemon over loopback HTTP (REST JSON wrapping the SSH run primitives, not gNMI and not a second `DriverClient`). Web, device-sync, and the driver CLI then share one owner when a socket or URL is configured, which is the only way to honor the AGENTS.md policy **across processes** and the only way to keep a warm session for one-shot CLI invocations.

## Background & Motivation

### Current transport: connect-per-call

`dialSSHShell` in `internal/drivers/openconfig.go` does `ssh.Dial`, requests a vt100 PTY, starts a shell, and spawns a stdout reader. Both `sshRunCLIBatch` and `sshRunCLIPipeline` call it and `defer s.Close()`. There is no pool, no keepalive, and no prompt parser. Completion is idle-based:

- `idleWindow = 5s` when the command has no end marker.
- `configEndMarkerIdle = 200ms` after a platform-supplied marker (`^return$` on VRP `display current-config`, `^end$` on XR, `^}$` on SR OS).
- `overallTimeout = 30s` hard cap.
- Login banner/MOTD is drained with `waitIdle(nil)` — always a full 5s after the last banner byte.
- `waitIdle` itself **never returns an error**. If output dribbles until `overallTimeout`, it returns the buffer and callers treat it as success (silent partial capture). A `--More--` prompt that then goes silent looks like a finished command after `idleWindow`.

Auth is password + keyboard-interactive (`sshPasswordAuthMethods`). `HostKeyCallback` is `ssh.InsecureIgnoreHostKey()`. Legacy KEX (`diffie-hellman-group-exchange-sha1`, `group1-sha1`) is already appended for old gear. Interactive PTY, not `ssh.Session.Run`. `ssh.Dial` uses the library default connection; there is no custom `net.Dialer` and therefore no TCP keepalive.

A typical VRP `Exec` (`driver_vrp.go`) therefore does **three** idle waits on a cold connection:

1. Banner drain (`waitIdle(nil)`).
2. `screen-length 0 temporary` (no marker → 5s).
3. The actual command (no marker → 5s).

Those three 5s waits are **guaranteed by the code**. Handshake duration and long MOTD on top of the banner wait are **not measured** here (non-goal: live `:8090`).

`RunningConfigGet` / `GetDeviceConfig` at least get the 200ms fast path on the dump itself (`vrpConfigEndMarker`), but still pay handshake + banner + paging-disable.

### Callers each dial independently

| Caller | Entry | Credentials | Process lifetime |
| ------ | ----- | ----------- | ---------------- |
| Web GUI | `web.Controller.newDriverForDevice` → `drivers.NewDriver` on every HTTP request (`web/handle_device_interfaces.go`, `web/handler_config.go` `apiServiceGenericPush`, `web/handler_service_eline.go`) | Per-request JSON `username`/`password`; **never persisted** | Long-lived `factum2-web` |
| `factum2-driver` CLI | `drivers.NewDriverName` (`cmd/driver/factum2-driver-cli.go`) | `--username`/`--password` / `FACTUM_DRIVER_USERNAME`/`PASSWORD` | One-shot per invocation |
| `factum2-device-sync` | `drivers.NewDriver` in `connectDevice` (`internal/device-sync/device-sync.go`) | `models.DeviceSyncAuth` fetched via `GET /api/device-sync-config` (per-device or `"default"`) | One-shot job; up to `syncWorkers=8` devices in parallel; `GetDeviceConfig` then later `GetNeighbors` on the same `DriverClient` value, but each method dials again |
| cfgmgmt / ELINE push | Same `newDriverForDevice` as the GUI, then `CLISessionApplier.ApplyCLISession` | Same per-request creds | Inside `factum2-web` |

`DriverParam` is `{Name, Port, Username, Password, Platform}`. `Name` is already the dial target (`drivers.DeviceFQDN`). `Port` is empty in every production caller and defaults to `"22"` in `dialSSHShell`.

An in-process pool in web alone does **not** help `factum2-driver exec` (process exits) and does **not** share a session with a concurrent device-sync job. That is why AGENTS.md's "per process" wording is necessary but not sufficient, and why a session owner process is the cross-caller design.

### Platforms: pain is SSH-CLI, not eAPI/NETCONF

From `internal/drivers/README-DRIVERS.md`:

| Platform | SSH CLI used for | Structured API |
| -------- | ---------------- | -------------- |
| Huawei VRP (5100 class) | **everything** | none |
| Cisco SMB | **everything** | none |
| Cisco IOS-XR | Exec, running-config, ELINE/CLI session | NETCONF for interfaces |
| Nokia SR OS | Exec, running-config, ELINE/CLI session | NETCONF for most reads/writes |
| Arista EOS | none (classic SSH) | NETCONF + eAPI. `eapiClient` in `eapi.go` is already a process-lifetime `http.Client` with a connection pool. **Zero** `sshRunCLI` call sites in `driver_arista_eos*.go` |
| Open ROADM | none | NETCONF read-only |

Phase 1 reuses SSH CLI sessions only, and only on platforms that opt in (default: `vrp`, `ciscosmb`). EOS is not on this path at all. NETCONF (`netconfDial` closes every call) is a later phase: candidate/lock state is a different hygiene problem.

### Worker hub is not this

`internal/worker` is a hub for **predefined named shell commands** with an allowlist (`worker.commands`). The primary dials out to `/hub`. A forged envelope can at most select a named command, never build a shell line. It is not a device CLI proxy, not a session multiplexer, and not a supervisor for long-lived device SSH. Overloading it would couple device PTY lifetimes to hub reconnects and punch a generic "run CLI on box X" hole through a security boundary that was deliberately not that. This design does not use the hub transport.

### Pain points

1. **Login dominates.** On a 5100, handshake + banner + `idleWindow` dwarf `display version`.
2. **N logins per operator action.** Each GUI POST constructs a new `DriverClient` and every `sshRunCLI*` dials. Refresh then update is two full logins. Device-sync is two per VRP device (`GetDeviceConfig` + `GetNeighbors`).
3. **Preamble tax.** Paging-disable is a first-class `sshCmd` and pays `idleWindow` every call even after the session exists.
4. **Stateful PTY.** Leftover `system-view`, a pager, or `screen-length` on a reused session is unsafe unless we serialize and reset. Today we dodge this by discarding the session — the expensive correct answer. Reuse makes leftover `--More--` load-bearing: `waitIdle` will treat a silent pager prompt as success.
5. **Reachability.** `factum2-web` on the primary may not have a route to every device; `factum2-driver` already exists as the "run next to the devices" binary. Session ownership should be placeable on the host that can actually dial.

## Goals & Non-Goals

### Goals

- Honor `AGENTS.md`: one SSH CLI session per device (per session key) for the owner's lifetime, not per command.
- Cut subsequent VRP single-command `Exec` / `GetInterfacesStatus` to **skip handshake + banner drain + paging-disable**. Latency target below (VRP-shaped; SMB multi-`show` is N×`idleWindow`).
- Serialize CLI on a given session so two callers cannot interleave `system-view` with a `display`.
- Keep platform drivers owning commands and parsers. Only `sshRunCLI*` change from "dial and close" to "borrow from pool".
- Opt-in per platform (and later per device). Default `vrp`/`ciscosmb` because they are SSH-CLI-only and their hygiene profiles are designed in this doc. EOS is not on `sshRunCLI`. XR/SR OS wait for a follow-up that copies their real session trailers.
- Expose enough telemetry in the same merge as the pool to prove the 5100 login cost is gone (login vs command duration, reuse flag, queue wait, reconnects).
- Make the pool usable in-process **and** from a `factum2-driver start` daemon without two implementations.

### Non-Goals

- gNMI, YANG mapping, or a gRPC stack. `openconfig.go`'s package comment is explicit: Factum did not add one.
- Wrapping `DriverClient` as an HTTP API in v1 (no remote `Version()` / `GetDeviceConfig()`). Drivers stay in the caller.
- Persisting device credentials in Factum for the GUI path. Per-request creds stay per-request; the pool holds them **in memory** on the session owner only (process-lifetime RAM, bounded by idle-close — an expansion of today's request-lifetime GUI secrets).
- Host-key verification overhaul. `InsecureIgnoreHostKey` is pre-existing; this design notes it and leaves a cheap `known_hosts` follow-up.
- Replacing the worker hub, Oxidized, or using OpenSSH `ControlMaster`.
- Prompt-accurate CLI parsing in v1. Idle-window detection stays; a prompt parser is an explicit follow-up because even a warm session still pays 5s per unmarked command.
- NETCONF / eAPI session reuse in v1.
- Changing cfgmgmt render/push semantics, ELINE templates, or `CLISessionApplier`.
- Live-instance (`:8090` / real `factum2` DB) verification.
- Enabling IOS-XR / SR OS pooling via YAML without a dedicated hygiene PR.
- A full `GUI()` graceful shutdown of `RemoteManager` / scheduler. The daemon closes PTYs on SIGTERM; `factum2-web` does not today (`web/web.go`: `GUI() has none for anything else either`).

## Key Decisions

1. **End state is alternative C: `factum2-driver start` as the long-lived session owner, with alternative A (in-process pool) as the library it wraps and as the zero-config fallback.** Rationale: AGENTS.md is per-process and is the right first merge; the user's cross-process problem is real (web + device-sync + one-shot CLI) and only a daemon solves it. One library, two embeddings. No new binary.

2. **API is REST JSON wrapping SSH run primitives (`batch` / `pipeline`), not gNMI and not a `DriverClient` RPC.** Rationale: gNMI is a YANG telemetry/config protocol; VRP/SMB have no YANG mapping and wrapping screen-scraped CLI in gNMI Get/Set would be a lie. Factum has no gRPC stack (deliberate, see `openconfig.go`). Echo/HTTP is the house style, but the daemon should use `net/http` like `internal/worker/local_api.go` rather than pulling Echo into `factum2-driver`. Full `DriverClient` over HTTP would move command construction into the daemon and contradict "drivers keep parsing". Jump-host for NETCONF/eAPI is a later RPC if we ever need it.

3. **Connect-on-first-use, not eager connect to inventory.** Rationale: credentials for the GUI path arrive per request and are not in the DB. There is no safe way to pre-dial. Device-sync has `DeviceSyncAuth` but is a short-lived job; eager dial of every VRP in inventory would burn VTY lines. Idle-close default **8 minutes** (≤ typical Huawei VTY `idle-timeout` of ~10 minutes of CLI input, which SSH-level keepalives do not reset) plus max sessions (default 64) bound VTY use. Device CLI idle still wins if an operator sets it lower.

4. **Session identity is `platform/username@host:port` (port default 22; `sros-md` canonicalized to `sros`).** Rationale: `ssh.Dial` uses host:port and user (`DriverParam.Name` is already `DeviceFQDN`), but hygiene/setup is per platform. Keying without platform would let a mis-set `vrp` vs `ciscosmb` reuse a PTY and run the wrong Reset. Password change (constant-time compare against the in-memory secret) drops and redials. Two factum names that FQDN to the same host **and** share platform+user share a session — desirable. `sros` and `sros-md` are the same NOS and the same CLI; they share a key once pooling exists for them.

5. **Opt-in default platforms: `vrp` and `ciscosmb` only.** Rationale: those are SSH-CLI-only (README-DRIVERS.md) and include the 5100; their setup/reset profiles are specified below. EOS never calls `sshRunCLI` and must not be dragged onto SSH. IOS-XR / SR OS are **not** enabled via `driver.platforms` in v1 — their real session trailers (`commit`/`discard`/`exit all`/`quit-config` on SR OS; `commit`/`abort` on XR) are not the same as a generic Reset, and enabling them without a hygiene PR would be wrong. Queueing is already per session key; a modern VRP and a 5100 do not share a FIFO unless they collide on `platform/user@host:port`.

6. **Do not reuse the worker hub.** Rationale: allowlisted named commands vs generic device CLI is a security property (`AGENTS.md`, `docs/install/workers.md`). Hub reconnects must not tear device sessions. `factum2-driver start` is a systemd unit of its own, placeable on the primary or on a host that can reach devices. The unit is **opt-in** (not in `install.py` `PRIMARY_UNITS`, not enabled by default) because it holds device passwords in RAM.

7. **Paging-disable is session setup; view Reset runs only after jobs that entered config mode and did not already exit.** Rationale: `waitIdle(nil)` is always 5s. Always-Reset would make warm VRP Exec ~10–12s and miss the p95 < 7s target. Read paths (`display` / `show`) stay in user-view; skip Reset. Write paths that already end with the platform exit command (`return` / `end` / `quit`) skip Reset. Only leftover config-mode (Exec of `system-view`, a batch that entered and did not exit) pays one hygiene `idleWindow`. Preamble elision still strips setup cmds on a ready session.

8. **Credentials never hit disk on the session owner. GUI secrets become process-lifetime RAM (bounded by idle-close), not request-lifetime.** Rationale: today a GUI password dies when the HTTP handler returns. After this, `factum2-web` or the daemon retains it until idle-close / process exit so it can redial. Device-sync passwords are already in Postgres (`DeviceSyncAuth.Password`, `json:"-"`). Do not add pprof/debug endpoints that heap-dump the pool. Never log passwords.

9. **Unix socket auth is filesystem ACL only (like `FactumHTTP`); TCP/HTTPS requires a bearer. Default daemon listen is unix-only.** Rationale: `internal/worker/local_api.go` + `util.FactumHTTP` send no `Authorization` on unix (`the socket ACL is the auth`). Matching that lets web probe `/run/factum2-driver/session.sock` with **zero YAML**, and lets `systemctl enable --now factum2-driver` start against existing `factum2-worker.yaml` with no `driver.token`. Loopback TCP is opt-in (`driver.listen: 127.0.0.1:8092`) and then requires a token. Residual: any process in group `factum` can POST CLI — same class as the worker socket.

## Proposed Design

### Process model

```mermaid
flowchart LR
  subgraph callers [Callers - DriverClient stays here]
    Web["factum2-web\nnewDriverForDevice"]
    Sync["factum2-device-sync\nconnectDevice"]
    CLI["factum2-driver exec/version/..."]
  end

  subgraph driversPkg ["internal/drivers"]
    VRP["VrpDriver / CiscoSMBDriver / ..."]
    RunCLI["sshRunCLI*"]
    Client["session Client"]
    LocalPool["in-process Pool"]
  end

  subgraph owner ["Session owner"]
    Daemon["factum2-driver start\nREST + same Pool"]
    Box["Huawei 5100 SSH :22"]
  end

  Web --> VRP
  Sync --> VRP
  CLI --> VRP
  VRP --> RunCLI
  RunCLI --> Client
  Client -->|"unix socket or session_url"| Daemon
  Client -->|"fallback if no daemon"| LocalPool
  Daemon --> Box
  LocalPool --> Box
```

**Recommended deployment (primary can reach devices):**

- Optionally run `factum2-driver start` on the primary (`systemctl enable --now factum2-driver`; not started by `install.py` by default). Default bind is **unix only** (`/run/factum2-driver/session.sock`); no `driver.token` required. Existing `factum2-worker.yaml` is enough.
- `factum2-web`, `factum2-device-sync`, and `factum2-driver` CLI all probe that socket via a **dedicated** helper (`drivers.SessionSocketPath`), **not** `util.FactumHTTP` / `HubSocketPath`. Hit the daemon → one owner. Socket missing / connection refused → in-process pool for that `Run` (see getPool / connect-failure fallback). HTTP 502 after the daemon ran CLI is **not** replayed. CLI one-shots still do not reuse across invocations without a live daemon.
- `factum2-device-sync` continues to call `util.WithoutHubSocket` for Factum API traffic so it does not ride `/run/factum2-worker/api.sock`. That must not disable the **session** socket probe.

**Jump-host deployment (primary cannot reach some boxes):**

- Run `factum2-driver start` on a host that can dial the devices.
- Point callers at `https://driver-host:8092` with TLS + bearer token (`driver.session_url` / `driver.session_token` in **each caller's** YAML).
- Non-loopback listen requires TLS **and** a non-empty `driver.allow_cidrs` (fail closed).
- Only SSH-CLI operations ride this path. EOS eAPI and NETCONF still dial from the caller — same as today, those callers already needed reachability.

**Zero-config:** no daemon, no YAML. Web's in-process pool still honors AGENTS.md for GUI traffic after `InitSSHPool` at `GUI()` start. This is why Phase 1 ships first and stays the fallback.

How each caller reaches the owner:

| Caller | YAML file | Path |
| ------ | --------- | ---- |
| Web GUI / cfgmgmt push / ELINE | `/etc/factum2/factum2.yaml` (`ConfigRoot`) | Unchanged `newDriverForDevice` → `NewDriver` → platform driver → `sshRunCLI*`. `GUI()` calls `InitSSHPool` from `Config.Driver` before `e.Start`. |
| `factum2-device-sync` | `/etc/factum2/factum2-worker.yaml` (`ConfigAgentRoot`) | Unchanged `NewDriver` in `connectDevice`. `WithoutHubSocket` for Factum REST only. Session socket still probed. `InitSSHPool` in `cmd/device-sync` before `Sync()`. |
| `factum2-driver` CLI | `/etc/factum2/factum2-worker.yaml` | Unchanged `NewDriverName`. `InitSSHPool` in each subcommand (or once in `main` before cobra run). With a daemon, a second `exec` skips login. |
| `factum2-driver start` | `/etc/factum2/factum2-worker.yaml` | Owns the pool; default unix-only. **Listen** unix path = `SessionSocketPath(driver.socket)` — same helper clients probe (relocated socket / `FACTUM_DRIVER_SESSION_SOCKET` must match). TCP only if `driver.listen` is set (then token; TLS+CIDR if non-loopback). **`InitSSHPool` client remote forced off** (`Socket: "none"`, empty `session_url`) so `getPool()` cannot HTTP to this process; that does **not** change the listen path. ServeMux calls `memoryPool.Run` / `localRun` only. CLI subcommands (`exec`, …) are a different process and still probe. |
| `factum2-worker start` | `/etc/factum2/factum2-worker.yaml` | Ignores `driver.*` (all fields `optional:"true"`). |

Do **not** add a `factum2-driver` entry to `worker.commands` as a way to "start the daemon". Worker commands are finite jobs (`sync`, `--job`). The session daemon is `Type=simple` systemd, like `factum2-web`.

### Library shape (Phase 1, used by Phase 2)

New file `internal/drivers/sshsession.go` (same package, to keep `sshCmd` / `sshShellSession` unexported). Tests in `sshsession_test.go` against a fake SSH server built with `golang.org/x/crypto/ssh` (no new module).

Signature change in **PR 1** — fold `context.Context` and `DriverParam` in one sweep so PR 2 does not invent a third:

```go
// before
func sshRunCLI(username, password, host, port string, cmds []sshCmd) (string, error)
func sshRunCLIBatch(username, password, host, port string, cmds []sshCmd) ([]string, error)
func sshRunCLIPipeline(username, password, host, port string, cmds []string, endMarker *regexp.Regexp) (string, error)

// after (PR 1)
func sshRunCLI(ctx context.Context, p DriverParam, cmds []sshCmd) (string, error)
func sshRunCLIBatch(ctx context.Context, p DriverParam, cmds []sshCmd) ([]string, error)
func sshRunCLIPipeline(ctx context.Context, p DriverParam, cmds []string, endMarker *regexp.Regexp) (string, error)
```

PR 1 call sites pass `context.Background()`: `driver_vrp.go`, `driver_ciscosmb.go` (`runCLI`/`runCLIBatch` helpers **and** `RunningConfigSave`, which calls `sshRunCLI` directly), `driver_iosxr.go` (including **`iosxrRunningConfig`**, which today takes loose `username, password, host` and is used by `GetDeviceConfig` — change it to `iosxrRunningConfig(ctx, p DriverParam)`), `driver_iosxr_eline.go`, `driver_nokia_sros.go`, `driver_nokia_sros_eline.go`. No `*_test.go` currently calls `sshRunCLI` directly.

Also in PR 1 (still behavior-neutral for callers):

```go
func (s *sshShellSession) waitIdle(ctx context.Context, endMarker *regexp.Regexp) error
```

- Idle-complete (quiet for `idleWindow` / marker idle) → `nil`.
- `ctx` cancelled/expired or `overallTimeout` → `errWaitTimeout`.
- The 100ms poll must `select` on `ctx.Done()` so a cancel aborts without waiting out `overallTimeout`.
- **Legacy (non-pooled) `sshRunCLI*`:** if `waitIdle` returns `errWaitTimeout`, **ignore it and return the buffer** — bit-identical to today (silent partial capture).
- **Pooled path (PR 2):** `errWaitTimeout` is a **hard error**; session Dead; do not return truncated output as success. Reusing a session after a truncated dump is how leftover pager/config-mode happens. This is a behavior change **only** for pooled platforms (`vrp`, `ciscosmb`). Impact: VRP `GetDeviceConfig` uses `vrpConfigEndMarker` and should still complete; SMB `show running-config` has **no** end marker — a dump that dribbles past 30s will start **erroring** instead of writing a truncated config into NetBox. That is the correct trade. If a real SMB dump hits it, add a marker or raise that job's timeout; do not silently truncate.

```go
type sshRunMode int

const (
    sshRunBatch    sshRunMode = iota // write, waitIdle, next (save/y, per-cmd errors)
    sshRunPipeline                   // join lines, one waitIdle (ApplyCLISession, ELINE)
)

type sshRunRequest struct {
    Param   DriverParam
    Mode    sshRunMode
    Cmds    []sshCmd          // pipeline: Cmds[i].Cmd only; EndMarker on last
    Actor   string            // audit; empty ok
    Timeout time.Duration     // 0 → overallTimeout (30s); covers command + hygiene
}

type sshRunResult struct {
    Outputs   []string      // pipeline: single combined capture
    Reused    bool
    Login     time.Duration // 0 if reused
    QueueWait time.Duration
    Command   time.Duration // command + optional hygiene, not login
}

type sshPool interface {
    Run(ctx context.Context, req sshRunRequest) (*sshRunResult, error)
    Stats() []sshSessionStat
    Close()
}
```

`sshRunCLIBatch` becomes `getPool().Run(...)` and keeps returning the last-command-only / all-outputs shapes the drivers already expect. `getPool()` is a small router:

1. If `InitSSHPool` has not been called → **panic** (`drivers: InitSSHPool was not called`). No lazy `sync.Once` defaults: that races YAML (`platforms: ["none"]`, `session_url`) applied later.
2. If `Param.Platform` is not in the enabled set **or has no v1 hygiene profile** → **legacy path** (`dialSSHShell`, `defer Close()`, `errWaitTimeout` ignored) and slog `ssh.legacy` with reason `not_enabled` / `no_profile`. YAML `platforms: [sros]` or `[ios-xr]` is a **no-op** until PR 6 adds profiles; do not run those platforms through `memoryPool` with empty setup/reset.
3. Else if a remote owner is configured (`session_url` or session socket not `none`) → try the HTTP client (Phase 2), with **connect-failure fallback** below. **`factum2-driver start` must not take this branch** (see InitSSHPool for `start`).
4. Else → process-lifetime `*memoryPool`.

**Daemon must not HTTP to itself.** Default session socket is `/run/factum2-driver/session.sock`. The daemon **binds** that path; clients **probe** it. Those two uses of `driver.socket` must not be collapsed into “`start` ignores YAML socket.”

- **Listen** (bind): `listenPath := SessionSocketPath(yaml.Socket)` — same function as clients (`driver.socket` / `FACTUM_DRIVER_SESSION_SOCKET` / default `/run/factum2-driver/session.sock` / `"none"` disables unix). A relocated socket that clients probe but `start` does not bind would make every `Run` connect-fail into the in-process pool and leave the daemon unused.
- **Client remote** (getPool HTTP): `factum2-driver start` calls `InitSSHPool` with `Socket: "none"`, `SessionURL: ""` so step 3 never fires in this process. Pool knobs (`platforms`, idle, max, …) still apply. YAML `session_url` is for CLI/web/device-sync, not for `start`.
- ServeMux handlers invoke the local `memoryPool.Run` (unexported `localRun`) **only**. They do not go through the HTTP client. Test: start the daemon, `POST /v1/cli/run`, assert no outbound HTTP to its own socket.
- `factum2-driver exec` / other CLI subcommands are separate processes; they keep the socket probe (same `SessionSocketPath`).

**Connect-failure fallback (Phase 2; specified here so `InitSSHPool` does not cache “HTTP forever”):** unlike `FactumHTTP` (probe once at client construction, no fallback after a later 502), a long-lived `factum2-web` must not fail every GUI driver POST until restart because the opt-in daemon died. Replay of a `Run` that the daemon **already executed** is worse (double `ApplyCLISession`).

- `InitSSHPool` records whether a remote is *configured* (socket path or `session_url`). It may probe once to log `ssh.remote=up|down`; it does **not** freeze the transport.
- Fallback **only** when the HTTP request **never got a response from the daemon**: socket missing, Stat/Dial fail, connection refused. Log `ssh.remote_fallback`, execute that `Run` on `memoryPool`, sticky `down`. Do **not** pay a 3s probe on every subsequent click while down.
- **If any HTTP status is received — including 502/504 (device/SSH error after the daemon wrote CLI), 408, 429, 4xx — return it. Do not replay.** Device 502 is not “daemon unavailable.” Sticky state stays `up` (a 5100 pager leftover must not open a second in-process VTY and break single-flight).
- **Do not fallback on client timeout after the request was sent.** The daemon may already have accepted and applied the batch. Return the timeout to the caller.
- While sticky `down`, Runs go straight to `memoryPool`. Re-probe the socket/URL at most once per `remoteRetry` (default **5s**) on the next `Run`; success → sticky `up` again (daemon sessions are independent; the in-process pool may hold its own VTYs until idle-close).
- `session_url` set and connect fails: same fallback to in-process (jump-host callers that cannot dial devices will then fail at `ssh.Dial` — same as today without a daemon).

`InitSSHPool(cfg)` must run once at process start, **before** any `sshRunCLI*` / `NewDriver` work:

- Second call without `ResetSSHPoolForTest` → panic.
- `web.GUI()`: first thing after config is loaded, before `e.Start`.
- `factum2-device-sync` `sync` and `factum2-driver` **CLI** subcommands: before dialing, with YAML client knobs (socket probe on).
- `factum2-driver start`: before serving, with **client remote forced off** (above). Unix **listen** still uses `SessionSocketPath(yaml.Socket)`.
- Tests that dial: `ResetSSHPoolForTest()` (installs compiled defaults) in `t.Helper` / `TestMain`. Parser tests that never dial do not need it.

Actor (empty until PR 4): `DriverClient` methods take **no** `context` (`driver.go`) and must not grow one (too large). PR 1 call sites pass `context.Background()`. Threading rule for PR 4: unexported `actor` field on `DriverParam` (not JSON, not a constructor requirement) set by `drivers.WithActor(p, name) DriverParam`. `newDriverForDevice` / `connectDevice` / CLI `NewDriverName` wrap the param **before** `NewDriver`. Every platform driver already stores `p DriverParam` and passes it to `sshRunCLI*`; those helpers copy `p.actor` onto ctx via `context.WithValue` (unexported `actorKey`). Do **not** add `ctx` to `DriverClient`. Public `DriverParam` fields stay Name/Port/Username/Password/Platform.

### Session lifecycle

```mermaid
stateDiagram-v2
    [*] --> Absent
    Absent --> Dialing: first Run
    Dialing --> Setup: Dialer + NewClientConn + PTY + banner waitIdle
    Setup --> Idle: platform setup cmds
    Idle --> Busy: acquired
    Busy --> Idle: job + optional hygiene ok
    Busy --> Dead: timeout / IO error / pager / hung PTY
    Idle --> Dead: idle timeout / keepalive fail / password change / Close
    Idle --> Dialing: first stdin write fails or conn already dead (one replay)
    Dead --> Dialing: next Run
    Dead --> [*]: pool evict
```

| Event | Behavior |
| ----- | -------- |
| Dial | Not `ssh.Dial`. Use `net.Dialer{Timeout: 10s, KeepAlive: 30s}` then `ssh.NewClientConn(conn, addr, sshClientConfig(...))` then `ssh.NewClient`. `KeepAlive` is the TCP-level backstop; it is **not** a substitute for device VTY idle. |
| First use | Dial. Banner `waitIdle(ctx, nil)` **once**. Run platform **setup** (required cmds then best-effort cmds; see Session hygiene). Required-setup error or pager → Dead. Best-effort `% Unrecognized command` is ignored. Then run the job. |
| Reuse | Skip dial, banner, setup. Run the job. |
| Stale Idle | Huawei VTY idle-timeout is commonly **10 minutes of CLI input**. SSH `keepalive@openssh.com` does not reset it. Replay **once** per `Run` iff the session was Idle at acquire **and** either (a) the connection is already dead before any command is written (`ssh.Client` wait / stdout goroutine exited — EOF while Idle should already have marked Dead; this covers a race where Dead is not yet observed), or (b) the first `stdin.Write` of this job fails (broken pipe). Do **not** wait for a stdout read; the reader is already a background goroutine (`dialSSHShell`). Do **not** replay once `waitIdle` for the job has started (commands may have executed). Then Close, redial, setup, replay the job. This is how the first GUI click after a box-side VTY timeout succeeds. |
| Keepalive | SSH-level: `client.SendRequest("keepalive@openssh.com", true, nil)` every 30s while Idle (goroutine per session). Failure → Dead (next `Run` redials; if the failure is discovered mid-Run, the stale-Idle replay above applies). No CLI-level probe in v1 (a cheap `display clock` would pay `idleWindow`). |
| Idle timeout | Default **8 minutes** without a job → Close, drop creds, evict. Next Run dials. Bounds VTY on 5100-class (often 5–8 VTY). Operators with a longer device VTY idle may raise `driver.idle_timeout`; the pool cannot extend a box-side timer. |
| Box drops the TCP | stdout reader hits EOF → Dead. In-flight `Run` that already wrote commands returns error (no replay). Next `Run` sees Dead and redials (Dead→Dialing), or stale-Idle replay if acquire still saw Idle and first write fails. |
| Dial failure | Return error to caller. Do not cache a failed session. Optional single retry if the error looks like VTY exhaustion: evict LRU idle session and dial once more. |
| Password change | Constant-time compare of the request password to the stored secret. Mismatch → Close, store new secret, redial. |
| Platform mismatch | Different `sessionKey` (platform is in the key). No reuse across platforms. |
| Max sessions | Default **64**. New key when at cap: evict LRU **idle** session. If none idle → `errPoolFull` (HTTP 429). |
| Process shutdown | **Daemon:** SIGTERM → `Pool.Close()` (closes every SSH client) → exit. systemd `TimeoutStopSec` ≥ 10s. **Web:** `GUI()` has no graceful shutdown today; SIGTERM kills the process and the kernel closes TCP. Do not pretend `Pool.Close()` runs in web without adding a shutdown path. Tests call `Pool.Close()` / `ResetSSHPoolForTest`. |

No eager connect to a configured device set. Device-sync should not "warm" 200 boxes at job start.

### Serialization, queue, timeouts

One session ⇒ one in-flight PTY job. A `chan struct{}` or `mutex` plus a FIFO of waiters.

```go
const (
    defaultQueueDepth     = 8
    defaultAcquireTimeout = 60 * time.Second
    defaultJobTimeout     = overallTimeout // 30s, existing; covers command + hygiene
)
```

- Waiters: FIFO. Fair enough; the real contention is "operator Exec vs device-sync dump on the same 5100".
- Acquire wait > `acquire_timeout` → error `errAcquireTimeout` (HTTP 408). The stuck `display current-config` keeps the slot; the waiter does **not** kill it.
- Queue length ≥ `queue_depth` → reject immediately `errQueueFull` (HTTP 429). Do not buffer unbounded GUI clicks.
- Job timeout: `context.WithTimeout` derived from `req.Timeout` (default 30s) **or** the caller's `ctx`, whichever is sooner. Passed into every `waitIdle` for the job **and** for hygiene if Reset runs. On `errWaitTimeout` / `ctx.Err()`: Close the SSH client (kills the PTY), session Dead, return error. Next caller redials (or stale-Idle replay if applicable). This is how a wedged 5100 pager does not block the device forever.
- HTTP client/server timeouts (Phase 2) must be ≥ acquire + job + slack: **120s** (60s acquire + 30s job + 5s possible hygiene + margin). Do not copy `util.FactumHTTP`'s `HubRPCTimeout = 60s`.
- `sshRunCLIPipeline` for ELINE/cfgmgmt can pass a longer timeout via context from the web handler later; v1 uses 30s, which is already `overallTimeout` today. Config dumps with end markers usually finish inside that; if a real 5100 dump exceeds 30s that is a **pre-existing** cap, now a hard error on the pooled path instead of silent truncation. Follow-up: `sshCmd` timeout override for marked dumps (60–120s).

`syncWorkers = 8` in device-sync is **across devices**, not eight jobs on one box. Per-device single-flight does not reduce that parallelism.

### Session hygiene

The PTY is stateful. Leftover `system-view`, `--More--`, or a partial line will corrupt the next job. Today we destroy the session instead.

v1 profiles (only platforms that may be enabled). Setup is split **required** vs **best-effort**; each command is sent and scanned separately:

| Platform | Required setup (fatal on CLI error / pager) | Best-effort setup (ignore unrecognized) | Config-mode entry | Exit cmds | Reset (only if needed) |
| -------- | ------------------------------------------- | --------------------------------------- | ----------------- | --------- | ---------------------- |
| `vrp` | `screen-length 0 temporary` | — | `system-view` | `return`, `quit` (last command) | `return` |
| `ciscosmb` | `terminal datadump` | `terminal width 0` | `configure` | `end` | `end` |

`smbPreamble` in `driver_ciscosmb.go` is explicit that `terminal width 0` is unrecognized on some trains (`% Unrecognized command`) and the driver **continues**. That string matches `smbCLIErrorMarkers` (`(?m)^%\s`). Treating the combined setup capture as fatal would Dead default-on `ciscosmb` sessions on those boxes. Preamble elision still strips **both** setup strings when present at the front of a job, including best-effort.

IOS-XR and SR OS have **no v1 profile**. Their real trailers are `commit`/`abort` (XR, `driver_iosxr_eline.go`) and `commit`/`discard`/`exit all`/`quit-config` (SR OS, `driver_nokia_sros_eline.go`; MD-CLI `quit-config` from a nested context fails — `/` alone does not clear exclusive-edit). Do not claim `driver.platforms: [sros]` works without a follow-up PR that copies those trailers.

**When to Reset** (after a successful or CLI-scanned-error job; never after IO failure):

```
entered := any cmd, trimmed, equals a platform Config-mode entry exactly
exited  := last non-empty trimmed cmd equals a platform Exit cmd exactly
if entered && !exited {
    write Reset; waitIdle(ctx, nil); scan pager/errors
} // else skip Reset — no extra idleWindow
```

Equality is on the **trimmed command string**, not `strings.Contains`. `Exec("system-view")` enters; `Exec("display | include system-view")` / `Exec("display system-view")` do not.

- VRP `Exec("display version")`: no `system-view` → skip Reset. Warm cost = one unmarked `idleWindow` (~5s) after preamble elision.
- VRP `SetInterfaceDescriptions`: starts `system-view`, ends `quit` → skip Reset.
- VRP `ApplyCLISession`: `vrpCLISessionCommands` ends with `return` → skip Reset.
- VRP `Exec("system-view")` (operator left config mode): entered, not exited → Reset `return` + 5s. Correct.

**Preamble elision:** when the session is Idle+ready and the incoming `sshCmd` list **starts with** the setup list (byte-for-byte command strings), strip that prefix before running. VRP `Exec` then sends only `display version`. If the session was just created, setup already ran; still strip. Non-pooled path unchanged.

**Pager leftover (required, not optional):** `waitIdle` returning successfully after 5s of silence does **not** mean the command finished — a `--More--` prompt that then goes quiet looks identical. After **every** capture (setup, each batch command, pipeline, Reset):

- If output matches a per-platform more-prompt, mark Dead, Close, return `errPagerLeftover`. Do not type `return`/`q` at the pager in v1.
- v1 regex, **line-anchored**, case-insensitive, applied to the **whole** capture: `(?im)^\\s*-{0,4}\\s*More\\s*-{2,}\\s*$` covering `--More--` and `---- More ----` (optional surrounding dashes/spaces). **Do not** use a substring `More:` — that matches English in MOTD, comments, and interface descriptions (`Need More: fiber`) and would Dead `GetDeviceConfig` on a healthy dump.
- Fake-SSH tests in PR 2 **must** cover: pager leftover kills the session; a dump whose description contains the word “more” / `More:` does **not**.

**Setup failure:** scan **required** setup commands only. If a required paging-disable output matches the platform CLI error markers already used by the drivers (`vrpCLIErrorMarkers` `Error:`, `smbCLIErrorMarkers` `^%\s`) or the pager regex, Dead, Close, return error. A session whose required setup failed must not elide preambles on the next job. **Best-effort** setup (`ciscosmb` `terminal width 0`): ignore `% Unrecognized command` (and other `%` errors); still Dead on pager. Fake-SSH test: width 0 returns `% Unrecognized command` and the session still becomes Idle/ready.

**Hung PTY:** `waitIdle` `errWaitTimeout` with no pager match → Dead (pooled path). Do not send Ctrl-C in v1.

**When to kill rather than reset:** IO error, job timeout, pager, setup error, password change, keepalive fail, hygiene timeout. Never reuse a session that failed hygiene.

Drivers must not assume a fresh user-view **during** a batch they themselves put into config mode (`SetInterfaceDescriptions` still sends `system-view` … `quit`). The pool only guarantees user-view **between** `Run` calls.

### Identity, FQDN, port

```go
func sessionKey(p DriverParam) string {
    host := p.Name // already DeviceFQDN at every production call site
    port := p.Port
    if port == "" {
        port = sshDefaultPort // "22"
    }
    // Raw IPv6 literals in Name are ambiguous with :port unless bracketed.
    // JoinHostPort wraps any host containing ":" — including already-bracketed
    // "[2001:db8::1]" → "[[2001:db8::1]]:22". Strip a matched [] pair only
    // (TrimPrefix/TrimSuffix would drop a trailing ']' from a non-bracketed name).
    if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
        host = host[1 : len(host)-1]
    }
    hostPort := net.JoinHostPort(host, port)
    platform := strings.ToLower(p.Platform)
    if platform == "sros-md" {
        platform = "sros"
    }
    return platform + "/" + p.Username + "@" + hostPort
}
```

IPv6 example: `vrp/admin@[2001:db8::1]:22`. Do not concatenate `host + ":" + port` as-is. Production callers use `DeviceFQDN` (hostnames); this is for `Name` that is a literal address.

Do not key on `models.Device.ID` or short name: the pool may run in `factum2-driver` with no Postgres, and `NewDriverName` already resolved FQDN via `GET /api/common-config`.

HTTP `DELETE /v1/sessions/{key}`: `{key}` is `url.PathEscape` of the **whole** key (escapes `@`, `:`, `/`, `[`, `]`). Clients must PathEscape; the daemon PathUnescape once.

### Opt-in

`util.ConfigDriver` is a new optional struct on both `ConfigRoot` and `ConfigAgentRoot`. **Every field** is `boa:"configonly" yaml:"..." optional:"true"` so existing configs keep loading.

```yaml
# factum2.yaml (web) and/or factum2-worker.yaml (driver/device-sync)
driver:
  # omitted key → default [vrp, ciscosmb]
  # platforms: [] or platforms: [none] → pooling off (legacy dial-and-close)
  platforms: [vrp, ciscosmb]
  idle_timeout: 8m
  max_sessions: 64
  queue_depth: 8
  acquire_timeout: 60s
  keepalive: 30s
  # socket: shared unix **listen** (start) and **probe** (web/device-sync/CLI) path
  #   SessionSocketPath(yaml) on both sides; default /run/factum2-driver/session.sock
  #   "none" disables unix listen AND client probe
  # session_url: https://jump.example:8092     # TCP/TLS client; unix probe skipped if set
  # session_token: <shared secret>             # required for session_url; unused on unix
  # Daemon (factum2-driver start only):
  # listen:                                    # omitted / empty / "none" = unix only (default)
  # listen: 127.0.0.1:8092                     # opt-in loopback TCP; requires token
  # token: <shared secret>                     # required if listen is set
  # tls_cert: /etc/factum2/driver.crt          # required if listen is non-loopback
  # tls_key: /etc/factum2/driver.key
  # allow_cidrs: ["10.0.0.0/8"]                # required if listen is non-loopback
```

`platforms` decoding: a pointer `*[]string` (or custom `UnmarshalYAML`) so we can tell omitted vs present-and-empty.

| YAML | Meaning |
| ---- | ------- |
| key omitted / `null` | default `vrp`, `ciscosmb` |
| `platforms: []` | pooling **off** |
| `platforms: [none]` | pooling **off** (documented in `examples/factum2.yaml` **and** `examples/factum2-worker.yaml`) |
| `platforms: [vrp]` | only VRP |

Per-device enablement is **not** a DB column in v1. Open question.

### How existing `DriverClient` code changes

- **Platform drivers:** keep every command string, regexp, `ApplyCLISession` wrapper, and parser. `sshRunCLI*` gain `ctx` + `DriverParam`. `iosxrRunningConfig` takes `DriverParam`.
- **`NewDriver` / `NewDriverName` / `newDriverForDevice`:** still construct in-process drivers (not an HTTP `DriverClient` RPC). PR 4: `WithActor` on `DriverParam` before `NewDriver`; `DriverClient` method set unchanged (no `ctx`).
- **`web.GUI()`, device-sync CLI, driver CLI:** `InitSSHPool` from YAML in the **same PR as the pool** (PR 2). Without that, `driver.platforms: []` in `factum2.yaml` does nothing.
- **EOS / Open ROADM / NETCONF helpers:** untouched.
- **Tests:** VRP/SMB unit tests that never dial stay green. Pool tests use a fake SSH server (reuse, serialize, password change, skip-Reset on reads, Reset on leftover config-mode, pager leftover kills session, dump containing “more” does not, `terminal width 0` `% Unrecognized command` still ready, queue full, idle eviction, stale-Idle replay on first write fail). There is **no** `FACTUM_TEST_VRP_*` in the repo (unlike `FACTUM_TEST_EOS_*` / `FACTUM_TEST_SROS_*` / `FACTUM_TEST_OPENROADM_*`); do not cite a VRP integration gate that does not exist. Canary is a real 5100 + `login_ms` logs.

### Phase 2 REST API

Daemon: `factum2-driver start` in `cmd/driver/factum2-driver-cli.go` (new subcommand). `net/http` ServeMux, not Echo.

**Bind policy:**

- Default **unix only**: listen path = `SessionSocketPath(driver.socket)` (default `/run/factum2-driver/session.sock`). Dir `0750`, socket `0660`, group `factum` — filesystem ACL **copied** from `internal/worker/local_api.go`. systemd unit copies `examples/factum2-worker.service` (`Group=factum`, `RuntimeDirectory=factum2-driver`, `RuntimeDirectoryMode=0750`, `TimeoutStopSec=10s`), **not** `factum2-web.service` (no `Group=` / `RuntimeDirectory=` — copying web would leave `/run/factum2-driver` `root:root`). Auth on unix is ACL **only** (no bearer), matching `FactumHTTP`. `systemctl enable --now factum2-driver` starts with existing worker YAML and no `driver.token`. `InitSSHPool` on `start` still forces **client** `Socket: "none"`; that does not relocate or skip this listen path.
- TCP is **opt-in**. `driver.listen` omitted, empty, or `none` → do not bind TCP. `listen: 127.0.0.1:8092` (or any address) **requires** `driver.token` (fail start if empty). Loopback HTTP without TLS is acceptable once a token is set: device passwords never leave the host. Loopback TCP without a token is easy to hit from other local uids — do not allow it.
- Non-loopback `listen` **requires** `tls_cert`/`tls_key`, `token`, and non-empty `allow_cidrs` (fail closed at start). TLS 1.2+.
- `socket: none` disables unix. At least one of unix or TCP must be on; if both disabled, `start` errors. If a **requested** bind fails (EADDRINUSE / EPERM), **fail start** — do not half-listen. Unix-only default does not attempt TCP, so EADDRINUSE on `:8092` cannot block the unit.

**Auth:**

| Listener | Auth |
| -------- | ---- |
| unix socket | filesystem ACL only; clients send **no** `Authorization` |
| TCP loopback | `Authorization: Bearer <driver.token>` (token required to start TCP) |
| TCP non-loopback | TLS + bearer + `allow_cidrs` |

Constant-time compare. Do not accept the Factum GUI JWT; this is not a user-facing API. Not on `hubAPIPatterns`.

**Timeouts and limits:**

- Client `http.Client.Timeout` = **120s**. Server: `ReadHeaderTimeout` 10s; per-request context 120s (covers 60s acquire + 30s job + hygiene + slack).
- POST body: `http.MaxBytesReader` **32 MiB** (same cap as `hubMaxMessageSize`). Oversize → 413.
- Response capture cap: same 32 MiB; larger device output is truncated with error, not streamed (no NDJSON in v1).

**`end_marker` is not a client-supplied regexp** (ReDoS). HTTP JSON sends a **named token** the daemon maps to the same compiled regexps the drivers already own:

| Token | Regexp |
| ----- | ------ |
| omitted / `""` | nil (idleWindow) |
| `vrp_config` | `vrpConfigEndMarker` (`^return$`) |
| `iosxr_config` | `iosxrConfigEndMarker` (`^end$`) |
| `sros_config` | `srosConfigEndMarker` (`^}$`) |

Unknown token → 400. In-process pool still uses `*regexp.Regexp` on `sshCmd`; only the HTTP boundary is named.

**Client mapping** (`*regexp.Regexp` → token): pointer equality against the three package vars (`vrpConfigEndMarker`, `iosxrConfigEndMarker`, `srosConfigEndMarker`). Anything else, including a locally `MustCompile`d copy of the same pattern → empty token (nil marker, idleWindow), never 400. Drivers **must** keep passing those package vars (`iosxrRunningConfig` already uses `iosxrConfigEndMarker`). Optional extra: if pointer miss, compare `Regexp.String()` to those three patterns and still emit the token — nice-to-have, not required; the rule implementers must not break is pointer equality to the package vars. XR/SR OS tokens on the wire are valid for jump-host **legacy** runs of those platforms if a caller somehow sent them; the daemon maps tokens to regexps regardless of whether the server pools that platform.

**Routes:**

```
GET  /health
GET  /v1/sessions          # stats, no secrets
DELETE /v1/sessions/{key}  # {key} = url.PathEscape(sessionKey)
POST /v1/cli/run
```

`POST /v1/cli/run` body:

```json
{
  "host": "sw1.example.com",
  "port": "22",
  "username": "admin",
  "password": "...",
  "platform": "vrp",
  "mode": "batch",
  "actor": "alice",
  "timeout_ms": 30000,
  "cmds": [
    {"cmd": "display version", "end_marker": ""}
  ]
}
```

`mode` is `batch` or `pipeline`. Response:

```json
{
  "outputs": ["..."],
  "reused": true,
  "login_ms": 0,
  "queue_wait_ms": 12,
  "command_ms": 5120
}
```

Errors: 400 validation, 401/403 auth, 408 acquire timeout, 413 body too large, 429 queue full / pool full, 502 device/SSH error **after the daemon has run (or tried) CLI** (body `{"error":"..."}`). Do not put the password in logs or error strings. Clients that receive any HTTP status **must not** replay the job in-process (see connect-failure fallback). 502 is not “daemon down.”

**Inventory allowlist:** unix and loopback TCP accept any `host` the process can route to (wider than `newDriverForDevice`, which looks up a device row first). Accepted residual for v1; every run logs `host`/`actor`/`platform`. Non-loopback **must** match `driver.allow_cidrs` (resolved IP of `host`). No "must be in Factum inventory" check in v1 (the daemon may have no Postgres).

The in-process HTTP client maps `sshRunRequest` onto this JSON (end markers as above). Callers never see it. Session socket resolution is `drivers.SessionSocketPath` (`driver.socket` / `FACTUM_DRIVER_SESSION_SOCKET` / default `/run/factum2-driver/session.sock` / `"none"`). Independent of `HubSocketPath` / `FACTUM_WORKER_API_SOCKET`. **Same helper for daemon listen and client probe.** Connect-failure fallback (no HTTP response) is in the getPool router; **`factum2-driver start` disables that client path** without changing listen.

**Not in v1:** streaming NDJSON, websocket PTY, file upload, NETCONF XML, eAPI proxy, `DriverClient` methods.

### Sequence: GUI Exec on a warm 5100

```mermaid
sequenceDiagram
  participant U as Operator
  participant W as factum2-web
  participant D as VrpDriver
  participant P as SSH pool (daemon or in-process)
  participant S as 5100 PTY

  U->>W: POST /api/device/:id/interfaces/refresh (user, pass)
  W->>D: GetInterfacesStatus()
  D->>P: Run(ctx, batch, screen-length + display interface description)
  alt cold
    P->>S: Dialer+NewClientConn + PTY + banner drain + setup
    Note over P,S: login_ms planning-only; canary validates
  else warm
    P->>P: acquire mutex (queue_wait)
  end
  P->>P: strip setup prefix
  P->>S: display interface description
  S-->>P: table + idleWindow
  P->>P: scan pager; skip Reset (no system-view)
  P-->>D: output, reused=true, login_ms=0, command_ms≈5000
  D-->>W: []NBInterface
  W-->>U: 200
```

### Quantified cost

**Planning-only.** Handshake 2–8s and banner 5–12s are **not measured** on the live instance (non-goal). Canary `login_ms` is what validates them. What the code **does** guarantee:

| Delta | Source |
| ----- | ------ |
| Each unmarked command = **+5s** (`idleWindow`) | `openconfig.go` |
| Marked dump after the marker line = **+0.2s** (`configEndMarkerIdle`) | `openconfig.go` |
| Banner drain on a new dial = **+5s** after last MOTD byte | `waitIdle(nil)` |
| Warm reuse `login_ms` = **0** | pool skips dial |
| Preamble elision = **−5s** vs sending `screen-length` every job | this design |
| Skip-Reset on user-view jobs = **−5s** vs always-Reset | this design |

Cold VRP `Exec` **code-guaranteed waits** = 5+5+5 = **15s** plus unmeasured KEX/MOTD (planning band ~17–32s if KEX is 2–8s and MOTD is short-to-long). Treat 17–32s as a planning band, not an SLA.

| Step | Cold (today, code) | Warm pool, setup done, preamble stripped, Reset skipped | Warm + prompt parser (follow-up) |
| ---- | ------------------ | ------------------------------------------------------- | -------------------------------- |
| TCP + SSH KEX | unmeasured (plan 2–8 s) | 0 | 0 |
| Banner/MOTD + `waitIdle(nil)` | 5 s after last byte (+ unmeasured MOTD) | 0 | 0 |
| `screen-length 0 temporary` + idleWindow | +5 s | 0 (setup once, then elided) | 0 |
| `display version` output | unmeasured (~0.3–2 s) | same | same |
| Trailing idleWindow (no end marker) | +5 s | +5 s | ~0 (prompt) |
| Hygiene Reset | n/a (session discarded) | **0** (read path) | 0 |
| **Total Exec (code waits)** | **15 s + KEX/MOTD/output** | **5 s + output** | **~output** |
| `display current-config` (`^return$`) | login+setup+dump+0.2 s | dump+0.2 s | dump+prompt |

**Latency targets after Phase 1/2 (no prompt parser):**

- Subsequent **VRP single-command** `Exec` / `GetInterfacesStatus` on a warm session: **p95 < 7s** (one `idleWindow` + output). Acceptance: `reused=true login_ms=0 command_ms≈5000`.
- **Cisco SMB `GetInterfacesStatus` / `Version`:** three unmarked `show` commands via `runCLIBatch` after a two-command preamble (`driver_ciscosmb.go`). Preamble elision leaves **three** `waitIdle`s → **~15s** plus output, not 7s. SMB multi-show stays N×`idleWindow` until **PR 5 (prompt parser)** or until those calls are rewritten as one pipeline. Do not lump SMB into the 7s target.
- Login count per GUI click: **0** if a session for that key is Idle; **1** on first use, after idle-close, or after a successful stale-Idle replay (replay's `login_ms` is non-zero; `reused` should be false or a separate `replayed=true` log field).
- `login_ms` on a clean reuse: **0**. That time series is the acceptance test for "the 5100 login cost is gone".

**Honesty about `idleWindow`:** a warm session does **not** make unmarked commands fast. VRP `display version` still sits ~5s after the last byte. Config dumps with end markers already avoid this. Write paths that skip Reset do not add a second 5s. The prompt-parser follow-up is what attacks the remainder; it is out of v1 because it is a behavior change in completion detection (risk of truncating slow renders — the original reason `idleWindow` went from 1s to 5s).

**Concurrency / scale:**

- GUI: a handful of operators; rarely more than one in-flight CLI job per device.
- Device-sync: 8 devices in parallel (`syncWorkers`). 8 extra sessions during a run, then they idle-close (8m).
- Session RAM: on the order of 100 KiB per PTY + capture buffer. 64 sessions is noise next to `factum2-web`.
- VTY: 64 max sessions process-wide, idle 8m, LRU eviction. A site with 200 VRPs will **not** hold 200 VTYs; only recently touched boxes will. Device-side 10m VTY idle still wins.

**Storage:** none. No schema migration.

## API / Interface Changes

### Go (Phase 1)

```go
// internal/drivers/sshsession.go
func InitSSHPool(cfg SSHPoolConfig) error // once; panic on double-init
func ResetSSHPoolForTest()                // tests only; Close + allow re-init
func SessionSocketPath(yamlOverride string) string

type SSHPoolConfig struct {
    Platforms      *[]string      // nil → default vrp,ciscosmb; empty → off
    IdleTimeout    time.Duration  // default 8m
    MaxSessions    int            // default 64
    QueueDepth     int            // default 8
    AcquireTimeout time.Duration  // default 60s
    Keepalive      time.Duration  // default 30s
    SessionURL     string         // Phase 2 client
    SessionToken   string
    Socket         string         // default /run/factum2-driver/session.sock
}

type sshSessionStat struct {
    Key           string  `json:"key"`
    Platform      string  `json:"platform"`
    State         string  `json:"state"` // idle|busy|dialing
    AgeSeconds    float64 `json:"age_seconds"`
    IdleSeconds   float64 `json:"idle_seconds"`
    Reused        uint64  `json:"reuse_count"`
    Reconnects    uint64  `json:"reconnects"`
    LastLoginMs   int64   `json:"last_login_ms"`
    LastCommandMs int64   `json:"last_command_ms"`
    LastQueueMs   int64   `json:"last_queue_wait_ms"`
    LastError     string  `json:"last_error,omitempty"`
    LastActor     string  `json:"last_actor,omitempty"`
    LastCmd       string  `json:"last_cmd,omitempty"` // first command, truncated, never password
    LastHost      string  `json:"last_host,omitempty"`
}
```

### YAML two-file matrix

| Binary | File | Struct | `driver` fields used |
| ------ | ---- | ------ | -------------------- |
| `factum2-web` | `/etc/factum2/factum2.yaml` | `ConfigRoot` | client: `platforms`, pool knobs, `socket`, `session_url`, `session_token` |
| `factum2-device-sync` | `/etc/factum2/factum2-worker.yaml` | `ConfigAgentRoot` | same client fields |
| `factum2-driver` CLI | `/etc/factum2/factum2-worker.yaml` | `ConfigAgentRoot` | same client fields |
| `factum2-driver start` | `/etc/factum2/factum2-worker.yaml` | `ConfigAgentRoot` | pool knobs + `listen`, `token`, `tls_*`, `socket`, `allow_cidrs` |
| `factum2-worker start` | `/etc/factum2/factum2-worker.yaml` | `ConfigAgentRoot` | none (ignore) |

There is **no** shared secret required for the recommended unix-socket deployment. TCP/jump-host: put the same value in daemon `driver.token` and caller `driver.session_token`. Web does not read worker YAML; if you use TCP from web you must duplicate the token into `factum2.yaml`. Prefer unix on the primary so web needs no token.

`ConfigDriver` on both roots:

```go
type ConfigDriver struct {
    Platforms      *[]string `boa:"configonly" yaml:"platforms" optional:"true"`
    IdleTimeout    string    `boa:"configonly" yaml:"idle_timeout" optional:"true"`
    MaxSessions    int       `boa:"configonly" yaml:"max_sessions" optional:"true"`
    QueueDepth     int       `boa:"configonly" yaml:"queue_depth" optional:"true"`
    AcquireTimeout string    `boa:"configonly" yaml:"acquire_timeout" optional:"true"`
    Keepalive      string    `boa:"configonly" yaml:"keepalive" optional:"true"`
    SessionURL     string    `boa:"configonly" yaml:"session_url" optional:"true"`
    SessionToken   string    `boa:"configonly" yaml:"session_token" optional:"true"`
    Socket         string    `boa:"configonly" yaml:"socket" optional:"true"`
    Listen         string    `boa:"configonly" yaml:"listen" optional:"true"`
    Token          string    `boa:"configonly" yaml:"token" optional:"true"`
    TLSCert        string    `boa:"configonly" yaml:"tls_cert" optional:"true"`
    TLSKey         string    `boa:"configonly" yaml:"tls_key" optional:"true"`
    AllowCIDRs     []string  `boa:"configonly" yaml:"allow_cidrs" optional:"true"`
}

type ConfigRoot struct {
    // ...
    Driver ConfigDriver `yaml:"driver" optional:"true"`
}
type ConfigAgentRoot struct {
    Factum ConfigFactum `yaml:"factum"`
    Worker ConfigWorker `yaml:"worker"`
    Driver ConfigDriver `yaml:"driver" optional:"true"`
}
```

### HTTP (Phase 2 only)

See routes above. Not registered on `factum2-web`. Not added to `hubAPIPatterns`.

### Callers

No public REST change on `factum2-web`. GUI still POSTs `{username,password}` to `/api/device/:id/interfaces/refresh` etc.

## Data Model Changes

None in Postgres. No `DeviceSyncAuth` change. No new Settings columns in v1 (YAML is host-local like `worker.listen`).

If we later want per-device opt-in from the admin UI, that would be a `devices` column or a tag — listed under Open Questions.

## Alternatives Considered

### A. In-process pool only (`openconfig.go`, honor AGENTS.md literally)

**Pros:** Smallest change; no new process, YAML, or auth story; web GUI clicks on a 5100 get the full win; device-sync `GetDeviceConfig`+`GetNeighbors` share a session within the job; independently testable.

**Cons:** Three processes still mean three logins (web, device-sync job, CLI). CLI never reuses across invocations. Jump-host still cannot help web. Idle sessions die when device-sync exits.

**Verdict:** **Ship this first** as the library. Not sufficient as the end state.

### B. Brand-new internal HTTP/gRPC session proxy binary

**Pros:** Matches the user's "internal service" wording; clear process boundary.

**Cons:** A seventh `cmd/*` binary for something `factum2-driver` already is: the tool that runs next to devices with Factum API access and no Postgres. Operators would have to learn a new unit. gRPC adds a stack Factum explicitly avoided.

**Verdict:** Reject a new binary. Put the server on `factum2-driver start`.

### C. `factum2-driver start` daemon exposing the pool over REST (recommended end state)

**Pros:** One owner across web, device-sync, CLI; placeable on a jump host; same binary operators already install; REST matches house style; unix-socket **filesystem ACL** copies `local_api.go`; unix-without-bearer copies `FactumHTTP` so web needs zero extra YAML.

**Cons:** Another systemd unit on the primary (hence **opt-in**, not `PRIMARY_UNITS`); token/TLS/CIDR ops for jump-host; failure mode if the daemon process is gone (mitigation: in-process fallback on **connect failure only**, sticky until re-probe — not `FactumHTTP`, and **not** replay on HTTP 502). Default unix-only matches the worker local API; loopback TCP is opt-in. `start` must disable the HTTP client so it cannot POST to its own socket.

**Verdict:** **Recommend.** Phase 1 library + Phase 2 this daemon.

### D. gNMI gateway / gNMI-to-CLI translator

**Pros:** Industry protocol; streaming; would look like "real" device mgmt.

**Cons:** No YANG for VRP/SMB CLI. Factum has no gNMI/gRPC client stack (`openconfig.go` comment). Wrapping `display version` as a gNMI path is a category error. OpenConfig NETCONF already covers the platforms that actually have YANG.

**Verdict:** Reject.

### E. OpenSSH `ControlMaster` / multiplexing at the OS level

**Pros:** Zero Go pool code; `ControlPersist` is a known hammer.

**Cons:** Drivers use `golang.org/x/crypto/ssh` PTY, not the OpenSSH client. Switching to `exec.Command("ssh")` throws away idle detection, end markers, keyboard-interactive handling, and legacy KEX already tuned in `sshClientConfig`. ControlMaster still does not serialize two Go callers writing to the same muxed session. Host keys / `known_hosts` become a new operational surface. Does not help if we are not spawning `ssh(1)`.

**Verdict:** Reject.

### F. Overload `factum2-worker` as a sidecar SSH multiplexer

**Pros:** Process already runs on remote hosts; primary already dials it.

**Cons:** Wrong security model (allowlisted named commands). Hub writer goroutine would block on large CLI captures (`hubRPCWriteWait` 60s, 32 MiB cap). Device session lifetime would follow hub reconnects. `worker.commands` cannot express "hold 64 PTYs". Would require new envelope types and allowlist holes.

**Verdict:** Reject.

### G. Remote `DriverClient` HTTP (full method-for-method proxy)

**Pros:** Web without device reachability would get EOS/NETCONF too; CLI could be a thin client.

**Cons:** Duplicates every driver method and DTO; version skew between web and daemon builds (worker already has a version-gate for this class of problem); moves parsing out of the caller, against the stated preference. SSH-CLI is the 5100 bug. Revisit only if jump-host-for-all-platforms becomes a product requirement.

**Verdict:** Not v1.

## Security & Privacy Considerations

| Threat | Severity | Mitigation |
| ------ | -------- | ---------- |
| Device passwords in the session owner's RAM | Medium | Never log; never persist; drop on idle-close / process exit. GUI secrets become **process-lifetime RAM** (was request-lifetime). No pprof/debug heap dump of the pool. Device-sync passwords are already in Postgres (`DeviceSyncAuth.Password`, `json:"-"`). |
| Session daemon binds on a reachable interface without TLS | High | Default listen is **unix only**. TCP is opt-in; non-loopback requires TLS cert/key + token + `allow_cidrs` at start. |
| Unauthenticated `POST /v1/cli/run` is arbitrary CLI on any dialable host | High | Unix: group `factum` ACL. TCP: bearer fail-closed if empty. Not on the hub allowlist; not on `factum2-web`. |
| Authenticated generic CLI proxy (any `host`, not just inventory) | Medium (accepted on loopback) | Log `host`/`actor`/`platform` every run. Non-loopback: `allow_cidrs` required. No Factum inventory check in v1. Wider than `newDriverForDevice`. |
| Audit gap: who ran what | Medium | `actor` field (later PR from web user / `device-sync` / CLI `$USER`); slog `cmd` truncated; do not log full cfgmgmt blobs. |
| Leftover config mode / pager from a reused session | High (correctness) | Skip-Reset only when the job never entered or already exited; line-anchored pager regex → Dead (not substring `More:`); **required** setup-error → Dead (`terminal width 0` `%` is best-effort); pooled `errWaitTimeout` → Dead; serialization. |
| First click after device VTY idle fails | Medium | 8m pool idle-close; one stale-Idle Close+redial+replay. |
| VTY exhaustion on a 5100 knocking out human operators | Medium | `max_sessions`, 8m idle-close, LRU, no eager dial. |
| MITM (pre-existing `InsecureIgnoreHostKey`) | Medium (existing) | Out of scope. Cheap follow-up: optional `driver.known_hosts`. Do not silently keep ignoring keys if a file is configured. |
| Token theft on loopback TCP | Low | Token required to enable TCP. Prefer unix for local. Same class as `factum.token`. |
| Queue DoS (many 429/408 on one box) | Low | Depth 8; acquire 60s; does not amplify to other devices. |
| ReDoS via HTTP `end_marker` | High if raw regexp | Named tokens only. |
| Second always-on process holding GUI passwords | Medium | `factum2-driver.service` **opt-in**, not `PRIMARY_UNITS`, not enabled by `install.py` by default. |

The daemon does **not** accept GUI JWTs and does **not** speak the worker hub protocol. Compromising `factum2-web` already implies the attacker can POST device creds to **inventory** driver endpoints; a jump-host daemon without `allow_cidrs` would additionally allow scanning arbitrary addresses — that is why non-loopback fail-closes without CIDRs.

## Observability

No Prometheus client in the Factum binaries today (Prometheus is a **downstream sync target**, not a self-scrape). Do not add a new metrics stack in v1. Use structured `slog` plus `GET /v1/sessions` (and `Pool.Stats()` in-process). **Slog/stats ship in the same PR as the pool** so a canary has `login_ms` on day one. There is no `FACTUM_TEST_VRP_*`.

**Logs** (`slog.Info`, one line per `Run`):

```
msg=ssh.run key=vrp/admin@sw1.example.com:22 host=sw1.example.com platform=vrp reused=true replayed=false login_ms=0 queue_wait_ms=12 command_ms=5120 cmds=1 actor=alice cmd="display interface description"
msg=ssh.dial key=... login_ms=14320 err=
msg=ssh.replay key=... reason=stale_idle
msg=ssh.reconnect key=... reason=keepalive
msg=ssh.evict key=... reason=idle_timeout
msg=ssh.pager key=... err=pager leftover
```

Never log passwords. Truncate `cmd` to 80 bytes.

**Session stats** (JSON, ops/debug): as `sshSessionStat` above.

**How we prove the 5100 win:** after a warm-up refresh, a second refresh's log line has `reused=true login_ms=0 command_ms≈5000`. If `login_ms` is still 10k+, the pool is not hitting. If `command_ms` is 5–7s, that is idleWindow, not login — prompt-parser follow-up. If `command_ms` is ~10s on a VRP read, Reset is not being skipped (bug).

**Alerting (ops, not code in v1):** `journalctl`. Watch reconnect/replay rate, `errQueueFull`, sessions at `max_sessions`, pager kills, hygiene failures.

## Rollout Plan

1. **Phase 1 library, default-on for `vrp`/`ciscosmb`, slog included.** Kill switch: `driver.platforms: []` or `[none]` in the YAML that binary actually reads (`factum2.yaml` for web, `factum2-worker.yaml` for device-sync/driver). Document in `examples/factum2.yaml` **and** `examples/factum2-worker.yaml` comments **and** the PR/release notes. Fake-SSH tests for hygiene/pager/replay. Canary a real 5100; there is no in-repo VRP integration tag.
2. **Canary:** one 5100, watch `login_ms` / `reused` / VTY (`display users`). Confirm a second GUI refresh is `login_ms=0` and `command_ms≈5s`, not 10s.
3. **Phase 2 daemon** opt-in on the primary (`systemctl enable --now factum2-driver`). Unix-only default; no web YAML and no `driver.token` required. If the daemon process is gone, the next `Run` fails Stat/Dial, falls back to the in-process pool for that call, and stays on it until a 5s-throttled re-probe succeeds — `factum2-web` does **not** need a restart. HTTP 502/4xx/timeout after the request was sent are **not** fallback (no double apply).
4. **Jump-host** only when a site needs it (TLS + token + `allow_cidrs` + `session_url` in each caller file).
5. **Rollback:** `platforms: []` in the relevant YAML, or stop the daemon. No DB migration. In-flight PTYs close on process stop.
6. **Feature flag:** YAML platforms list. No Settings UI in v1.

Do not add `ios-xr` / `sros` to `platforms` until a hygiene follow-up copies their real trailers. EOS stays off `sshRunCLI`.

## Open Questions

1. **Per-device opt-in in the GUI?** v1 is platform-wide. A 5100 vs a modern CE switch both as `vrp` may want different behavior. Candidate: tag or `devices.ssh_pool` column. Needs a product call; not required to ship the 5100 fix.
2. **Device-sync auth vs GUI auth on the same box.** If the GUI user is `admin` and device-sync `"default"` is `netbox-sync`, they are **two sessions** (different keys). Confirm that is wanted (probably yes: different privilege). If both use the same TACACS user, they share.
3. **Job timeout for huge `display current-config` / SMB `show running-config`.** Pooled path now hard-errors at 30s instead of silent truncate. Raise only if a real dump exceeds it; SMB may need a marker.
4. **Self-scrape Prometheus later?** Only if we add a general Factum metrics story. Do not special-case this service.
5. **`known_hosts` follow-up owner.** Separate small PR; not a blocker.
6. **Should `GUI()` gain SIGTERM → `e.Shutdown` + `Pool.Close()`?** Out of v1; `GUI()` has no shutdown for `RemoteManager` either. Daemon does close PTYs.

## References

- `internal/drivers/openconfig.go` — `dialSSHShell`, `sshRunCLI*`, `waitIdle` (no error, `idleWindow`/`overallTimeout`), `sshClientConfig`
- `internal/drivers/driver.go` — `DriverClient`, `DriverParam`, `NewDriver`, `DeviceFQDN`
- `internal/drivers/driver_vrp.go` — `Exec`/`ApplyCLISession`/`vrpCLISessionCommands`
- `internal/drivers/driver_ciscosmb.go` — `smbPreamble`, `GetInterfacesStatus` (3× `show`), `runCLIBatch`
- `internal/drivers/driver_iosxr.go` — `iosxrRunningConfig`
- `internal/drivers/driver_iosxr_eline.go` — `commit`/`abort` trailer
- `internal/drivers/driver_nokia_sros_eline.go` — `commit`/`discard`/`exit all`/`quit-config` trailer
- `internal/drivers/eapi.go` — existing HTTP client pool (EOS, do not SSH-queue)
- `internal/drivers/README-DRIVERS.md` — per-platform transport table
- `web/handle_device_interfaces.go` — `newDriverForDevice`, per-request creds
- `web/handler_config.go` — `apiServiceGenericPush` / `ApplyCLISession`
- `web/web.go` — `GUI()` has no graceful shutdown
- `internal/device-sync/device-sync.go` — `connectDevice`, `syncWorkers=8`, `GetNeighbors`
- `cmd/device-sync/factum2-device-sync-cli.go` — `util.WithoutHubSocket` for Factum API only
- `cmd/driver/factum2-driver-cli.go` — one-shot CLI, `ParamsAgent`
- `internal/util/remoteconfig.go` / `transport.go` — `FactumHTTP` unix sends no bearer; `HubRPCTimeout=60s`; `WithoutHubSocket`
- `internal/worker/local_api.go` — dir `0750` / socket `0660`, no bearer
- `internal/worker/hub.go` — `hubMaxMessageSize` 32 MiB
- `install.py` — `PRIMARY_UNITS = ("factum2-web.service", "factum2-worker.service")`
- `internal/worker/hub_allowlist.go`, `docs/install/workers.md` — why not the hub
- `AGENTS.md` — "Device SSH: reuse one SSH connection per device for the process lifetime"
- `docs/cfgmgmt-tree-objects.md` — CLI objects still execute via `CLISessionApplier`

## PR Plan

Incremental, each PR independently reviewable and mergeable. No schema migrations.

### PR 1 — Refactor `sshRunCLI*` onto `ctx` + `DriverParam`; make `waitIdle` cancelable

- **Title:** `drivers: pass context and DriverParam into sshRunCLI helpers`
- **Files:** `internal/drivers/openconfig.go`, `driver_vrp.go`, `driver_ciscosmb.go`, `driver_iosxr.go` (including **`iosxrRunningConfig`**), `driver_iosxr_eline.go`, `driver_nokia_sros.go`, `driver_nokia_sros_eline.go`
- **Depends on:** none
- **Description:** Change `sshRunCLI` / `sshRunCLIBatch` / `sshRunCLIPipeline` to `sshRunCLI(ctx, p DriverParam, cmds)`. Call sites pass `context.Background()` and `driver.p`. `iosxrRunningConfig(ctx, p)`. `waitIdle(ctx, endMarker) error` with `errWaitTimeout`; **legacy callers ignore timeout and return the buffer** (today's silent partial). Needed so the pool can see `Platform` and so PR 2 can actually cancel the 100ms poll. Pure refactor for production behavior; no pool yet.

### PR 2 — In-process SSH session pool + slog + `InitSSHPool` from config

- **Title:** `drivers: reuse one SSH CLI session per device`
- **Files:** `internal/drivers/sshsession.go`, `sshsession_test.go`, `openconfig.go` (route pooled platforms through the pool; legacy path unchanged), `internal/util/config.go` (`ConfigDriver` fully optional on `ConfigRoot` and `ConfigAgentRoot`), `web/web.go` (`InitSSHPool` at start of `GUI()`), `cmd/device-sync/factum2-device-sync-cli.go`, `cmd/driver/factum2-driver-cli.go`, `examples/factum2.yaml` **and** `examples/factum2-worker.yaml` (platforms default + kill switch `[]` / `[none]`), `AGENTS.md` (point at the pool)
- **Depends on:** PR 1
- **Description:** `memoryPool` with connect-on-first-use, per-key mutex/FIFO, **vrp and ciscosmb profiles only** (YAML `sros`/`ios-xr` → legacy + slog until PR 6), preamble elision, **Reset only if entered config mode and did not exit**, required-setup-error → Dead, best-effort `terminal width 0` `% Unrecognized command` → still ready, line-anchored pager leftover → Dead (`More:` substring does not), pooled `errWaitTimeout` → hard error + Dead, 8m idle-close, max 64, keepalive via `net.Dialer{KeepAlive}` + `ssh.NewClientConn` + SSH keepalive requests, one stale-Idle redial+replay on first **stdin write** fail or conn already dead (not a stdout wait), password-change redial, platform in `sessionKey` via `net.JoinHostPort` after stripping `[]`, `InitSSHPool` required (panic if missing or double). Structured `ssh.run` / `ssh.dial` / `ssh.replay` / `ssh.pager` logs with `login_ms`, `command_ms`, `queue_wait_ms`, `reused`, `host`, truncated `cmd`. `Pool.Stats()`. Default-on for `vrp`/`ciscosmb`; kill switch in **both** example YAML files + release notes. Fake SSH tests as in “How existing DriverClient code changes.” No `FACTUM_TEST_VRP_*`.

### PR 3 — `factum2-driver start` REST session daemon (Phase 2)

- **Title:** `factum2-driver: start command serving the SSH session pool`
- **Files:** `cmd/driver/factum2-driver-cli.go`, `internal/drivers/sshsession_http.go` (server + client), `internal/drivers/sshsession.go` (`SessionSocketPath` / `session_url` / remote failover; must not use `HubSocketPath`), `examples/factum2-driver.service` (new), `examples/factum2-worker.yaml` (listen/token comments; platforms comment already in PR 2), `install.py` (copy the unit, **do not** add to `PRIMARY_UNITS` or auto-enable), `docs/install/` (opt-in enable steps)
- **Depends on:** PR 2
- **Description:** `factum2-driver start` binds **unix only** by default (no token). Unix **listen** path = `SessionSocketPath(driver.socket)` (shared with clients). TCP only if `driver.listen` is set, then token required; non-loopback TLS + token + `allow_cidrs`. Fail start if a requested bind fails; at least one listener required. Unix ACL-only (no bearer). Unit file templates **`examples/factum2-worker.service`**: `Group=factum`, `RuntimeDirectory=factum2-driver`, `RuntimeDirectoryMode=0750`, `TimeoutStopSec=10s` (not `factum2-web.service`). **`InitSSHPool` client remote off** (`Socket: "none"`, empty `session_url`) — does not ignore YAML socket for listen; handlers call `localRun`/`memoryPool.Run` only — no HTTP to the process’s own socket. `POST /v1/cli/run` with named `end_marker` tokens (**client maps `*regexp.Regexp` by pointer equality** to the three package vars; else empty token), 32 MiB body cap, 120s client/server timeout. `GET /v1/sessions`, `DELETE /v1/sessions/{url.PathEscape(key)}`, `GET /health`. SIGTERM → `Pool.Close()`. Client (web/CLI/device-sync): probe session socket even when `WithoutHubSocket` disabled the hub socket; **fallback only on connect failure** (no HTTP response); any status including 502 is returned, not replayed; client timeout after send is not fallback. Tests: httptest round-trip; start succeeds with empty `driver.token` when listen unset; TCP listen without token fails start; TLS+CIDR required for non-loopback; 401 on TCP without token; unix without Authorization succeeds; 429 on full queue; 413 oversize; unknown end_marker 400; **POST /v1/cli/run against a started daemon makes no outbound HTTP to its own socket**; unlinking the socket / connection refused → in-process Run succeeds; HTTP 502 from daemon is returned without a second local apply. Installer copies `factum2-driver.service` but does not enable it (`systemctl enable --now factum2-driver` works with stock worker YAML).

### PR 4 — Pass actor from web / device-sync / CLI

- **Title:** `drivers: record SSH session actor from callers`
- **Files:** `internal/drivers/driver.go` (`WithActor` on unexported `DriverParam.actor`), `internal/drivers/sshsession.go` (`sshRunCLI*` copies `p.actor` onto ctx), `web/handle_device_interfaces.go` (`newDriverForDevice`), `web/handler_config.go`, `web/handler_service_eline.go`, `internal/device-sync/device-sync.go` (`connectDevice`), `cmd/driver/factum2-driver-cli.go` (`NewDriverName`)
- **Depends on:** PR 2
- **Description:** Thread actor without adding `ctx` to `DriverClient` (methods have none; do not expand that interface). `drivers.WithActor(p, name) DriverParam` sets an unexported field. `newDriverForDevice` and `connectDevice` wrap the param **before** `NewDriver`. `NewDriverName` builds `DriverParam` itself — apply `WithActor` there (optional last arg or `$USER` inside the CLI helper); do not add methods on `DriverClient`. `sshRunCLI*` copies `p.actor` onto ctx for slog / HTTP JSON `actor`. Empty actor until this PR remains fine. No GUI/API contract change. Independent of the daemon.

### PR 5 (follow-up, not required for the 5100 login win) — Prompt-based idle detection

- **Title:** `drivers: detect CLI prompt to cut idleWindow on warm sessions`
- **Files:** `internal/drivers/openconfig.go`, `sshsession.go`, fake-SSH tests
- **Depends on:** PR 2 (ideally after a real 5100 canary)
- **Description:** Optional prompt regexp per platform (`<[-A-Za-z0-9_.]+>` style for VRP user-view). When matched, use `configEndMarkerIdle` instead of `idleWindow`. Keep `idleWindow` as fallback. Path from ~5s VRP Exec / ~15s SMB status to ~1s. Separate because a wrong prompt regexp truncates output (historical 1s idleWindow bug). Also the path to make SMB multi-`show` acceptable.

### PR 6 (follow-up) — XR / SR OS SSH pool profiles

- **Title:** `drivers: SSH session hygiene for IOS-XR and SR OS`
- **Files:** `internal/drivers/sshsession.go`, `driver_iosxr_eline.go` / `driver_nokia_sros_eline.go` (reference trailers only)
- **Depends on:** PR 2
- **Description:** Copy real session teardown (`commit`/`abort`; `commit`/`discard`/`exit all`/`quit-config`) into pool profiles before anyone sets `platforms: [ios-xr, sros]`. Until this PR, those platforms stay on dial-and-close even if listed in YAML (unknown profile → refuse to pool, log, legacy path).

### PR 7 (follow-up, optional) — NETCONF session reuse

- **Title:** `drivers: reuse NETCONF sessions for XR/SR OS/EOS/Open ROADM`
- **Files:** `internal/drivers/openconfig.go` (`netconfDial`/`netconfGet`/`netconfEditConfig*`)
- **Depends on:** PR 2 conceptually; do **not** block on it
- **Description:** Only if login-to-port-830 shows up in ops. Must drop sessions after candidate lock/edit failures. Independent of the SSH CLI pool (different port, different state machine).
