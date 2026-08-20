# userland

`userland` keeps Giacomo's personal macOS environment consistent across machines. It installs developer tools and personal applications, links declarative configuration, applies selected user preferences, and reports drift. Corporate software, credentials, browser profiles, histories, caches, and application databases stay outside the repository.

## Install

Install the latest stable release:

```sh
curl -fsSL https://userland.guidotto.dev | sh
```

Install an exact release:

```sh
curl -fsSL https://userland.guidotto.dev/v1.2.3 | sh
```

The first command installs a checksum-verified release, runs it, then creates a managed Git checkout at `~/.local/share/userland/repo`. Later runs use that checkout and update `main` only by fast-forward when it has no local changes.

## Commands

```text
userland plan
userland sync
userland doctor
userland doctor --json
```

`plan` is read-only. It refreshes repository discovery when its 24-hour snapshot has expired, then combines missing packages, rolling upgrades, mise-managed state, and app-specific checks. It also states the source, expected duration, restart implications, and rollback boundary.

`sync` always shows that combined plan and asks once before changing the machine. It then refreshes a clean userland checkout, updates rolling packages and applications, applies declared state, handles attended imports, and finishes with `doctor`. Re-run it after an interruption. There is no separate resume command.

`doctor` is read-only. It checks mise, declared state, app-specific receipts, file handlers, security posture, free disk space, and repository discovery.

## Safety

- `sync` never stashes, resets, cleans, or edits another repository.
- Dotfile conflicts stop the run. Only symlinks created by the old repository or a verified userland release qualify for automatic migration.
- Directory migrations preserve children that are not declared in the new source. This keeps authentication and history local.
- Userland does not prune unmanaged packages, applications, files, Dock items, caches, or login items.
- Personal repositories declared in `state/repositories.tsv` clone only when missing. Existing and dirty checkouts are never refreshed or rewritten.
- Browser extension inventory is declarative, but installation stays attended through the Chrome Web Store. Userland never copies a browser profile.
- App Store authentication, macOS privacy approvals, licenses, DaVinci Resolve, OpenScreen, Raycast Beta, and Raycast's encrypted import may require attention.
- Xcode is declared through the App Store. Android uses command-line tools and emulators without Android Studio.

## Implementation

mise 2026.8.9 owns CLI packages, dotfiles, macOS defaults, locked developer tools, and status. A narrow Homebrew Bundle adapter owns native applications and Xcode because mise 2026.8.9 cannot parse several current cask records. The small POSIX shell modules under `libexec/userland/` also add safe checkout refresh, repository caching, migration, combined health reporting, file handlers, static shell initialization, and attended application state.

GitHub Releases hold immutable SemVer assets. Cloudflare Workers Static Assets serves the same bootstrap bytes at the root and exact-version paths without a redirect or paid storage service. See [delivery/README.md](delivery/README.md) and [docs/architecture.md](docs/architecture.md).

Nix and Dotbot remain temporarily as migration references. Do not run the legacy manifests. Remove them only after a real fresh-Mac parity check.
