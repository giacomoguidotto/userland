---
name: t3-code-threads
description: >-
  Manage T3 Code conversations on the Mac and remote VM: create and brief threads,
  inspect history, send follow-ups, select models and effort, and control thread
  lifecycle. Use for requests about other T3 Code threads or delegating work in this app.
---

Use `~/.local/bin/t3-thread`. This controls the actual T3 Code app through its server API. It is installed for Codex and Claude on both machines. Do not open T3 Chat or invent a new API client.

Run `t3-thread servers` to see targets. Select `--server mac` or `--server vm` explicitly on every command. From the Mac, `vm` uses SSH; on the VM it uses its local server. Paths belong to the selected machine. `t3-thread --help` is the maintained command reference.

Before acting, identify the exact project/thread using `projects`, `list`, `search --query TEXT`, or `show --thread ID`. Titles are not unique. Respect the user's requested model/account; inspect `models` for actual provider instance IDs and model slugs.

## Reliable mutations

Every mutation requires `--operation KEY`. Choose a descriptive unique key ONCE per intended action and retain it in your working notes. A retry must reuse the same key and request, or run `resume --operation KEY`. Never rerun creation with a fresh key because of a timeout, disconnect or process crash. The controller journals exact command and thread IDs on the server-owning machine before sending them.

`operation --operation KEY` reports the existing result/recovery state. `complete` means the command was accepted and its effect read back; it does not mean an agent finished its task. `wait --thread ID --seconds 30` reports progress without cancelling the work at timeout. A recovery error is a reason to inspect/resume, not to delete the thread or restart T3.

## Creating a thread and handing over work

Write a self-contained briefing into a private temporary file. Include the objective, accepted decisions, unresolved questions, exact paths, verification expectations, and permitted scope. New threads do not inherit this conversation. Inspect uncommitted changes before choosing a new worktree: they will not appear there automatically.

```sh
t3-thread --server vm create --operation learning-prototype-20260908 \
  --from EXISTING_THREAD_ID --title 'Learning prototype' \
  --provider claudeAgent --model claude-fable-5-1 --stdin < /tmp/briefing.txt
```

`--from` inherits the parent project's model, modes and existing worktree unless explicitly overridden. Alternatively choose `--project ID`. A fresh project-scoped thread defaults to approval-required/default. Use `--mode full-access` when authorized by the requested work. `--worktree` selects an existing absolute path; `--new-worktree --base main --branch task-branch` asks T3 to prepare one. No setup script is run implicitly.

## Existing threads

```sh
t3-thread --server vm messages --thread ID --limit 20
t3-thread --server vm send --thread ID --operation followup-key --stdin < /tmp/message.txt
t3-thread --server vm update --thread ID --operation effort-key --effort high
t3-thread --server vm pin --thread ID --operation pin-key
t3-thread --server vm settle --thread ID --operation settle-key
```

Sending preserves model/account, effort, permission mode and interaction mode. Busy threads reject new sends; don't interrupt unless the user requested it. `interrupt` cancels a turn; `stop` ends its provider session. Pin/unpin, settle/unsettle, archive/unarchive and rename are separate actions. Only delete when the user requested removal or when cleaning up a disposable thread you created for this task.

The controller refuses account changes on existing threads because T3 0.0.40 can lose native context during those switches. Model/effort changes within the same account are supported while idle. Use an explicitly briefed new thread for a different account; do not reset the old session or mutate T3's database.

Credentials are issued locally for five minutes and revoked after each command. Configuration and code are versioned in Userland; credentials and conversation-bearing operation records stay in private runtime storage. A version-compatibility error requires testing the new server version before changing the allowed version list.
