# Compatibility receipt — 2026-09-08

Target: T3 Code 0.0.40 on macOS arm64 and Linux x64, using `t3code-cli` 0.15.0's RPC runtime with locked Effect beta.103 dependencies. The controller deliberately does not invoke the upstream CLI's create/send command implementations.

Automated: 18 controller tests passed, including lost acknowledgements, crash recovery, operation locking, exact-request replay, partial creation, settings/account guards and archive-aware deletion verification. Userland's Go suite and all 13 Bats compatibility tests passed; launcher ShellCheck and manifest parsing passed.

Live on both servers: one disposable thread was created, the same creation and resume requests returned the original receipt, and exactly one initial user message existed. A second turn correctly recalled a marker from the first turn. Model selection, options, permission mode and interaction mode were unchanged. Rename, effort update, pin/unpin, settle/unsettle, archive/unarchive and provider-session stop were accepted and independently read back. Both tests used `codex_beta` with `gpt-6-astra`.

A separate Mac test created and attached a worktree through T3's native bootstrap command. Its disposable thread, clean worktree and branch were removed afterward. Test-created conversations are removed after checks; persistent journals retain the original operation IDs for audit.

The first archive test exposed that T3's normal thread HTTP endpoint returns 404 after archival. The controller now consults the archived shell snapshot and successfully reconciled that original operation. Deletion checks both active and archived inventories.

Provider limitation observed: the Mac `claudeAgent` instance accepted creation but its model turn reported an expired OAuth session that could not be refreshed. This is separate from controller transport and was not repaired or switched implicitly.

Scope: tested configuration is Mac-local, VM-local, and Mac-to-VM through the existing SSH alias. No reverse SSH route, T3 restart, provider migration or native database mutation is part of installation. Account switching on existing conversations is intentionally refused until native context continuity can be verified on a fixed T3 version.
