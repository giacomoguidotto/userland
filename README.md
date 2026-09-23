# userland

A small, reproducible personal machine baseline. The same release command works on Apple silicon macOS, Linux x64/arm64, and Termux's Linux arm64 environment.

[![Checks](https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml/badge.svg)](https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml)
[![Latest release](https://img.shields.io/github/v/release/giacomoguidotto/userland)](https://github.com/giacomoguidotto/userland/releases/latest)

## Install

```sh
curl -fsSL https://userland.guidotto.dev | sh
```

The bootstrap selects the matching release archive, verifies its SHA-256 checksum, recovers unfinished transactions, and then runs the same plan and sync flow every time. It keeps a failed transaction's journal so a rerun can finish or roll it back. Set `USERLAND_PLATFORM=darwin-arm64`, `linux-arm64`, or `linux-x64` only when testing a different target.

`userland nuke --dry-run` previews the complete contents of the configured home folder. `userland nuke` removes every entry under that folder, including `~/dev`, hidden files, credentials, caches, and application data, then leaves the empty home directory in place. It requires an interactive confirmation; use `--yes` only after an external backup has completed. The command does not erase `/Applications`, `/opt/homebrew`, or other paths outside the home folder. Because Userland itself lives under the home folder, bootstrap it again after logging in before running `userland sync` to restore the declared state.

On Termux, install `git`, `curl`, `tar`, `zsh`, and the current `mise` package from Termux first. Termux uses the Linux arm64 archive. Mise and some upstream binary releases still depend on the Termux package set, so project-specific toolchains belong in that project's own `mise.toml`.

## What the default sync installs

Mise is the one installer and version source for the portable toolchain. On Linux and Termux it installs pinned versions of Node, 1Password CLI, Codex CLI, Atuin, bat, btop, eza, fd, fzf, GitHub CLI, delta, jq, Neovim, ripgrep, Starship, and zoxide. btop and eza do not publish macOS release archives, so the macOS Mise package declarations install their Homebrew formulas. Git itself comes from the host package manager on Linux and from the pinned Mise Homebrew package on macOS.

macOS applications are limited to 1Password, Ghostty, Helium, Raycast, Shottr, Spotify, T3 Code, Wispr Flow, and JetBrains Mono Nerd Font. Raycast, Shottr, and Wispr Flow remain login items. Browser extension prompts are limited to 1Password and Raycast Companion in Helium. No Chrome, Zed, Docker, Colima, Kubernetes, Android SDK, Java, Gradle, mobile SDK, DaVinci Resolve, OpenScreen, or hardware utility is part of the default.

The macOS baseline hides desktop files, mounted volumes, and desktop widgets while keeping widgets available in Notification Center. It shows Finder hidden files and uses the dark appearance. Liquid Glass's clear/tinted choice and the exact Notification Center widget order are private macOS UI state; Userland does not edit those undocumented records.

Hardware integrations are deliberately absent from the public baseline. Add a device-specific declaration to a private or local Mise profile when the device is attached. Mobile development belongs in `cfg/profiles/mobile.toml`, which is never loaded by the default sync.

The default repository catalog is empty. Sync does not clone personal repositories or overwrite existing work. Add a repository declaration locally when a machine should own a canonical checkout.

The default realm catalog is empty. If an older Userland state file contains Danfoss or Trellis attachments, the next sync removes their generated `.envrc`, direnv authorization, Git projection, SSH projection, and attachment records. It leaves the checkout directory in place for review instead of deleting source code.

Browser profiles, cookies, saved sessions, browser extensions, and non-XDG application settings are not copied. Sync checks the two declared Helium extension IDs and opens their Chrome Web Store pages for attended installation. Raycast is the one exception: sync opens Raycast, waits for onboarding to finish, then opens the tracked encrypted `.rayconfig` and asks for confirmation in the Userland TUI after you enter its passphrase in Raycast. A receipt records the confirmed import so subsequent runs skip it. Its declared login item starts Raycast automatically at login with that configuration. Wispr Flow is also started immediately when its login item is reconciled. macOS notification and privacy permissions remain attended system settings; Userland does not claim to grant protected TCC permissions silently. Ghostty, Git, Mise, Neovim, Atuin, bat, btop, Starship, and the shell use the declared XDG files.

## Credentials

The repository contains no token, private key, browser cookie, or exported session. Codex is configured with `cli_auth_credentials_store = "keyring"` and `mcp_oauth_credentials_store = "keyring"`, so login fails rather than writing `~/.codex/auth.json` when a system keyring is unavailable. See the [Codex authentication settings](https://learn.chatgpt.com/docs/auth).

The personal authentication wizard runs inside the Userland TUI and follows the human through 1Password SSH-agent setup, the GitHub SSH-key page, GitHub CLI's browser login, and Codex's browser login. It is safe to stop and rerun. For non-interactive work, keep only `op://...` references in an untracked local file and run the command with `op run`; 1Password resolves the value in memory. Do not put `OP_SERVICE_ACCOUNT_TOKEN`, `GH_TOKEN`, API keys, or private keys in this repository or in shell startup files. See [1Password's secret environment guide](https://www.1password.dev/cli/secrets-environment-variables/).

## Use

| Command | Purpose |
| --- | --- |
| `userland plan` | Show declared changes without applying them. |
| `userland sync` | Apply the plan, run attended authentication steps, and reconcile files. Rerun after any interruption. |
| `userland nuke --dry-run` | Preview removal of every file under the home folder, including `~/dev`. |
| `userland nuke` | Remove the home folder contents after an explicit TUI confirmation. Use `--yes` only after an external backup has completed. |
| `userland doctor` | Report drift without changing the machine. |
| `userland completions <shell>` | Print Bash, Fish, Nushell, or Zsh completions. |

Userland writes a static Zsh cache containing direct paths for only the tools in `cfg/mise.toml`. It does not put the shared Mise shim directory on the global path. Project toolchains stay inside their repository.

## Repository map

| Folder | Contents |
| --- | --- |
| `.mise/` | Tools and tasks used to test this repository. |
| `cfg/` | Personal applications, Mise declarations, dotfiles, credential policy, and optional profiles. |
| `completions/` | Static shell completion definitions. |
| `internal/` | Planning, adapters, recovery transactions, health checks, and terminal rendering. |
| `release/` | Reproducible archives, platform selection, checksum verification, and public bootstrap delivery. |
| `tests/` | Go, shell, release, and compatibility tests. |

Sync only changes paths declared by this checkout. It stops on unmanaged dotfile conflicts, never stashes an existing repository, and removes only the explicitly listed optional application bundles under `/Applications` (GarageBand, iMovie, iWork, Chess, Photo Booth, and Stickies). It never scans for or deletes applications under `/System/Applications`.

## Development

```sh
mise run test
```

Release builds are reproducible and publish one bootstrap plus Darwin arm64, Linux arm64, and Linux x64 archives. The bootstrap chooses the archive from `uname`, verifies it before extraction, and promotes a release only after the sync apply checkpoint succeeds.

MIT licensed.
