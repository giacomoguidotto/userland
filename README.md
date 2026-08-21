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
curl -fsSL https://userland.guidotto.dev/v0.1.11 | sh
```

The installer verifies the release checksum, runs the first sync, and creates a managed checkout at `~/.local/share/userland/repo`.

## Use

| Command | Purpose |
| --- | --- |
| `userland plan` | Show what would change without changing the machine. |
| `userland sync` | Apply declared state, guide attended steps, then run the doctor. Safe to rerun after an interruption. |
| `userland doctor` | Report drift and machine health without changing anything. Use `--json` for structured output. |

## Repository map

| Folder | Contents |
| --- | --- |
| `bin/` | The public `userland` command. |
| `config/` | Personal machine state, including dotfiles, applications, repositories, and agent assets. |
| `lib/` | Command implementation and external-system adapters. |
| `release/` | Checksum-verified release and bootstrap delivery. |
| `tests/` | Behavior checks at the command, migration, release, and HTTP interfaces. |

## Ownership

Userland owns only the personal state declared here. Corporate software, work accounts, credentials, browser profiles, histories, caches, application databases, and machine-local authentication stay out.

Sync never stashes, resets, cleans, or edits another repository. It does not prune unmanaged packages, applications, files, Dock items, login items, or browser extensions. Dotfile conflicts stop the run; supported legacy migrations preserve undeclared children.

## Manual gates

macOS and application security still require a person for some steps: App Store authentication, privacy approvals, licenses, browser extensions, Android SDK licenses, vendor-only installers, and Raycast's encrypted import. `userland sync` opens or explains each attended step and remains safe to rerun.

## Release integrity

Strict SemVer tags produce immutable GitHub Release assets. The public bootstrap verifies the archive checksum before extraction. `userland.guidotto.dev` serves the same bootstrap bytes at the root and exact-version endpoints without redirects.

A real fresh-Mac parity run remains the final proof. This is personal configuration in public, not a general-purpose dotfiles framework.

MIT licensed.
