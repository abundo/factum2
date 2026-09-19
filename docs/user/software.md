---
title: Software
order: 55
---

# Software

The **Software** page (DCIM menu) is a repository of router and switch
images. Upload, rename, move, and delete files and folders, then copy an
image onto a device.

The feature is off until an administrator enables **Software repository**
under Admin → Settings → Factum → Software. The compose lab seeds it on
(`http://factum-storage:8088`, SFTP `factum` / `lab`).

## Where files live

Files are **not** in Postgres. `factum2-storage start` keeps them in the
repository directory on the **storage host** — the primary, or a remote
server that already runs `factum2-worker`.

The GUI talks to that host through the worker hub (or a unix socket when
storage runs on the same machine as `factum2-web`). Device HTTP/TFTP/SFTP
is served on the storage host so boxes can pull images without going
through the primary.

## Copy to a device

Pick a file → copy. Protocols:

| Protocol | Direction | What happens |
| --- | --- | --- |
| HTTP | Device pulls | Factum SSHes to the device and runs a platform `copy http://…` command. Set **HTTP URL (devices)** to an origin the box can reach. |
| TFTP | Device pulls | Same, with `tftp://`. Set **TFTP host**. |
| SCP / SFTP | Storage pushes | The storage host opens SSH to the device (device-sync credentials) and writes the file to the destination path you enter. |

Copy progress is in the log panel. The worker must have a
`worker.commands.storage` entry that runs `factum2-storage` so the copy
command can be dispatched.

On-device destination is platform-specific (`flash:EOS.swi`,
`cf3:/image`, `/mnt/flash/EOS.swi`, …).

## Related

- [Admin settings](settings.md) — enable the feature and set listen
  addresses
- [Worker nodes](../install/workers.md) — hub transport
- [Software storage daemon](../install/storage.md) — `factum2-storage start`
