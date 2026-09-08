# T3 thread management

`t3-thread` is an owned controller over the pinned `t3code-cli` 0.15.0 RPC runtime. It avoids that CLI's two-step retry and settings-reset behavior. The package lock also pins Effect and its platform packages to matching versions.

Source, credential-free machine profiles, launcher, installer and agent skill are versioned in Userland. Auth sessions, operation messages and local runtime files are not tracked. Native T3 databases are never edited.

## Install

On the Mac, from `~/.userland`:

```sh
mise run install-t3-threads
```

For the VM, copy `cfg/t3-thread` (excluding `node_modules`) and `cfg/agents/skills/t3-code-threads` into a private staging directory preserving their relative paths, then run `python3 cfg/t3-thread/install.py --profile vm` as the VM user. The installer locates Node 24+, installs the locked package without lifecycle scripts, runs the core tests, and atomically selects a content-addressed release. Rerunning unchanged is a no-op.

The executable is `~/.local/bin/t3-thread`. Releases are under `~/.local/share/t3-thread/releases`; `current` selects the active release. Skills are linked into existing Codex and Claude homes, including account-specific skill directories. Existing unrelated files are not replaced.

Mac configuration: `~/.config/t3-thread/config.json` points to the versioned Mac profile. VM configuration points to the installed VM profile. The Mac profile supports `mac` locally and `vm` through `ssh trellis-remote-dev`. The VM profile supports its local `vm` server. VM-to-Mac access requires an independently configured reachable SSH route; none is opened implicitly.

## Operation safety

Use `--server` explicitly. Discovery checks the expected T3 environment UUID before obtaining credentials or dispatching commands. The controller uses an official short-lived T3 CLI auth session, revokes it in `finally`, and uses loopback on the owning machine. SSH forwards a JSON request on stdin; credentials do not cross machines or enter the versioned profiles.

Every mutation requires `--operation KEY`. Before dispatch, immutable command IDs, target IDs and request identity are saved in a private journal. Retry the identical request with the same key or use `resume --operation KEY`. A SQLite OS lock prevents concurrent execution and releases automatically after a crash. Journal writes use atomic rename and fsync independently of that lock. T3's own command receipts deduplicate replays after uncertain network outcomes.

Completed operations return their receipt without repeating side effects. Changed requests cannot reuse the same key. Failures retain their original IDs and return recovery information. No error handler deletes a partially created thread.

Follow-ups preserve model/account/options and both permission and interaction modes. Busy sends/settings changes are rejected. Existing-thread account switches are refused because native context continuity is not reliable in T3 0.0.40. `--from` supports explicit handoff to a new thread with inherited settings and worktree.

Only T3 0.0.40 is enabled for mutations initially. Re-run the recovery tests and isolated live acceptance checks before adding a new version to a profile's `compatibleVersions` array. Request deadlines do not prove server rejection; always reconcile the same operation key.

## Verification

```sh
mise run test-t3-threads
t3-thread servers
t3-thread --server mac doctor
t3-thread --server vm doctor
```

Tests cover lost acknowledgements, failure between creation and first message, process death, concurrent invocation, changed request rejection, environment separation, readback recovery, settings preservation, busy/account-switch refusal and new-worktree command construction. Run live checks only in disposable threads/projects; never turn existing user conversations into test fixtures.

The opt-in `test/live-acceptance.py --run --server TARGET --project ID --provider INSTANCE --model MODEL` creates its own disposable conversation, checks repeated creation, native context across a follow-up, settings preservation and lifecycle controls, then removes it. It spends two small model turns. To test a source checkout, provide `--command-json '["node","/absolute/path/to/cli.mjs","--config","/absolute/path/to/profile.json"]'`.

Archived threads are included in `list` and support lifecycle operations. T3 0.0.40 hides their transcripts from the HTTP endpoint, so `messages` explains that an explicit unarchive is required.

`complete` in an operation receipt means accepted and read back, not model-task completion. Use `wait` or `show` to inspect the agent. See the installed `t3-code-threads` skill and `t3-thread --help` for the operating workflow.
