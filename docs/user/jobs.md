---
title: Jobs
order: 50
---

# Jobs

Syncs and maintenance tasks run as **jobs**. A job is one trigger (a
single target, or **Sync all**) made of one **task** per target. The
primary dispatches each task to a connected worker whose roles include
that target — except housekeeping, which runs in-process on the primary.

## Job overview

Tiles are grouped:

- **Sources** — BECS, Lime, NetBox (update Factum)
- **Destinations** — DNS, Icinga, LibreNMS, Oxidized, Prometheus (generated
  from Factum)
- **Maintenance** — housekeeping (trim old job history)

**Sync** on a tile queues that target. **Sync all** runs every enabled
*sync* target in source-then-destination order, one at a time, so a
destination never writes from stale Factum data. Housekeeping is **not**
included in Sync all; run it from its tile or a schedule.

If no connected worker handles a target, the job is created and marked
failed immediately — it still shows up in history.

## Job status

**Job status** shows which worker nodes are configured vs actually
connected, plus recent jobs. Open a job to see per-target tasks, exit
code, and (when the worker command was given `--job`) structured
info/warning/error lines.

## Scheduler

**Scheduler** stores cron entries. Each row has a name, a target (`all`
or one job name, including `housekeeping` and `device-sync`), an enabled
flag, and last/next run. Presets cover common intervals; **Custom** is a
five-field cron expression.

Nothing starts housekeeping on its own. If you want old jobs pruned,
create a schedule for it (or click **Run** on the overview tile).

## Device deletions

LibreNMS delayed delete is a separate queue: devices LibreNMS still has
that Factum no longer wants monitored. **Device deletions** lists them
with a reason (no matching Factum device, disabled, not monitored).
Operators can wait for the grace period or delete on the next sync.
Enable the delay and its day count under Admin → Settings → Destinations
→ LibreNMS.
