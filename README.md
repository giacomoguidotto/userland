# userland

My personal macOS configuration. It restores the tools, applications, preferences, and files that make a new Mac feel like mine.

[![Checks](https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml/badge.svg)](https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml)
[![Latest release](https://img.shields.io/github/v/release/giacomoguidotto/userland)](https://github.com/giacomoguidotto/userland/releases/latest)

## Install

Latest stable release:

```sh
curl -fsSL https://userland.guidotto.dev | sh
```

Pin an exact release:

```sh
curl -fsSL https://userland.guidotto.dev/v0.1.30 | sh
```

The installer verifies the release checksum and prepares `~/.userland` before showing the first plan. Cancelling removes a checkout created by that run. Once apply starts, the same path is retained and becomes the managed Git checkout.

## Use

| Command | Purpose |
| --- | --- |
| `userland plan` | Show what would change without changing the machine. |
| `userland sync` | Apply declared state, guide attended steps, then run the doctor. Safe to rerun after an interruption. |
| `userland doctor` | Report drift and machine health without changing anything. Use `--json` for structured output. |
| `userland realm add <repository> <path>` | Attach optional private configuration to a directory tree on this Mac. |
| `userland realm remove <name-or-path>` | Detach a realm without deleting its checkout or portable declaration. |
| `userland completions <shell>` | Print completions for Bash, Fish, Nushell, or Zsh. Zsh is wired in automatically by sync. |

## Realms

A realm applies a private operational identity below one directory. The
portable, optional catalog lives in `cfg/realms.csv`; the attachment map for
the current Mac lives in Userland state and is not committed. A new Mac can
therefore discover a realm without cloning or activating it until `realm add`
is run explicitly.

`realm add` clones a missing checkout or adopts an existing checkout whose raw
origin matches the declaration. It never pulls, resets, switches, stages, or
cleans an existing repository. Userland then creates an excluded `.envrc` that
loads the realm's `mise.toml` once through the existing direnv hook. An optional
`.userland/envrc` can provide additional private exports. If the realm contains
`.gitconfig`, Userland generates a native Git `includeIf` for repositories below
the mounted path.

A realm can declare its repository taxonomy in
`.userland/repositories.csv` with `repository,path,branch` columns. Plan and
doctor validate each checkout, its raw origin, and its declared canonical
branch. Sync clones missing checkouts and resets existing primary checkouts to
the declared remote branch while preserving ignored files. Feature work belongs
in linked worktrees. Userland also maintains local Git exclusions in the realm
control repository and never deletes an undeclared child checkout.

`realm remove` revokes direnv authorization and removes only Userland-generated
activation. The checkout, private files, and optional catalog entry remain.
Passwords, tokens, and private keys should still live in a credential store,
not merely in a private Git repository.

## Repository map

| Folder | Contents |
| --- | --- |
| `.mise/` | Fork-owned development tools, lockfile, lint, and test tasks. |
| `cmd/` | The thin Go command entry point. |
| `cfg/` | Personal machine state, including its isolated Mise declaration and lockfile, dotfiles, applications, repositories, optional realm catalog, and agent assets. |
| `completions/` | Static shell completion definitions. |
| `internal/` | Go orchestration, typed planning, adapters, recovery transactions, health checks, and terminal rendering. |
| `release/` | Checksum-verified release and bootstrap delivery. |
| `tests/` | Frozen-v0.2.3 compatibility, release, and HTTP interface checks. |

The release command is a statically linked Go binary. Its public interface is
`userland.Run`, while machine effects stay behind internal collectors. Personal
declarations never live in the library and are owned only by `cfg/`.
Userland invokes Mise only through `cfg/mise.toml`; parent development tooling
cannot leak into machine synchronization. A fork can replace `cfg/` without
editing the library or its development environment.
Tabular declarations use header-validated CSV files so values can be quoted
without changing their schema.
The compatibility suite materializes v0.2.3 from Git as a shell oracle, then
compares terminal bytes, exit status, command traces, and sync behavior against
the Go implementation.

## Ownership

Userland owns only the personal state declared here. Private realm contents, credentials, browser profiles, histories, caches, application databases, and machine-local authentication stay out of the public repository.

Sync never stashes or edits an undeclared repository. Declared primary checkouts are canonical remote-branch mirrors: sync discards their tracked changes and untracked, non-ignored files while preserving ignored local state such as dotenv files. It does not prune unmanaged packages, applications, files, Dock items, login items, or browser extensions. Dotfile conflicts stop the run; supported legacy migrations preserve undeclared children.

## Manual gates

macOS and application security still require a person for some steps: App Store authentication, privacy approvals, licenses, browser extensions, Android SDK licenses, vendor-only installers, and Raycast's encrypted import. `userland sync` opens or explains each attended step and remains safe to rerun.

## Release integrity

Strict SemVer tags produce immutable GitHub Release assets. The public bootstrap verifies the archive checksum before extraction. `userland.guidotto.dev` serves the same bootstrap bytes at the root and exact-version endpoints without redirects.

A real fresh-Mac parity run remains the final proof. This is personal configuration in public, not a general-purpose dotfiles framework.

MIT licensed.
