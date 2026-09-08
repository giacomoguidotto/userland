# Handover verification — 2026-09-08

The remote environment had two T3 0.0.40 servers sharing `~/.t3`: the old SSH-launched worker on 3774 and `t3code.service` on 37437. The exact failing conversation's native writer lock belonged to a Codex child of the old worker. The legacy guard recognized only the npx command format and continued routing the desktop proxy to the old server.

Saved all provider bindings and thread metadata privately under `~/.t3/userdata/recovery/server-handover-20260908` before handover. The stale `dx/shadow-ready` native turn was explicitly interrupted through the old server before retirement. The old worker then exited on TERM and released its Codex writers. The managed service was kept running, with its existing PID and mobile authorization.

Verified all 141 pre-existing bindings retained their account and native conversation identity. A Claude resume checkpoint advanced normally without changing its session identity. The failing thread's kernel writer lock became acquirable. No native database writes or lock-file deletion were used. The old server removed the shared runtime-discovery file on shutdown; restored the managed worker's verified record from the pre-handover snapshot.

The new guard's five tests passed, covering both launch formats, separate state directories, managed ownership, ambiguous ownership refusal and idempotent routing. Live `--check` identified the duplicate before retirement and reported none afterward. The existing timer is active. Ports 3773 and 37437 report the same environment UUID; port 3774 is closed. T3 Connect reports desired, authenticated and linked. A disposable Codex turn returned `HANDOVER_OK`, then its thread was deleted.

The guard intentionally reports future unexpected duplicates rather than guessing which active conversations are safe to kill. SSH discovery is marked external and routed through the managed service's stable proxy; future updates can change its backend port without creating a second SSH-owned server.
