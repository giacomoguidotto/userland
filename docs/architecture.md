# Architecture

## Interface

The public interface is `plan`, `sync`, and `doctor`. `doctor --json` is the only machine-readable variant. Internal modules may grow without adding public commands.

The command module delegates native resources to mise and app-specific state to adapters. Every adapter accepts `plan`, `apply`, or `doctor`. It must keep planning and diagnosis read-only, make application idempotent, and return exit code 2 when human attention or drift remains.

## Ownership

Each resource has one owner:

- mise owns locked tools, rolling CLI packages, dotfile links, and supported macOS defaults;
- Homebrew Bundle owns native application presence and Xcode;
- Git owns the userland checkout and explicit personal repositories;
- application updaters may own applications whose vendor requires them;
- userland adapters own only state that mise cannot express;
- the person owns credentials, account sessions, TCC approvals, licenses, browser profiles, histories, caches, and dirty repository state.

Corporate state is out of scope. The declaration must not install, remove, diagnose, or report corporate and MDM-owned software.

## Recovery

Each operation detects current state before changing it. `sync` presents the same unforced operations it will apply and requires one confirmation before mutation. It is safe to rerun after interruption. Receipts record only non-secret hashes and, for attended imports, the user's acknowledgement. Irreversible or attended actions stop at a clear prompt. Userland has no automatic cleanup or global rollback because package managers, App Store installations, and vendor applications do not share a transaction.

`state/schema-version` prevents a release from applying a state model it does not understand. A schema change requires a migration and a userland release that supports it.

## Performance budgets

- command dispatch before external work: under 100 ms;
- default local doctor: under 2 seconds, excluding mise's own network-independent inventory cost;
- warm interactive Zsh startup: target under 100 ms;
- repository discovery: never on the warm path and no more than once per 24 hours;
- package mutation: at most four jobs on this 16 GB machine;
- background polling: none added by userland.

Zsh sources one generated cache. `sync` builds that cache from Atuin, Carapace, direnv, fzf, Starship, mise completion, Pitchfork, and Zoxide. Shell startup does not execute those generators or `brew shellenv`.

## Release trust

GitHub Releases are the source of truth. A release contains `bootstrap`, `checksums.txt`, and `userland-vX.Y.Z.tar.gz`. Exact-version assets are immutable. The root promotes only the same bytes from the latest stable release.

The bootstrap's embedded archive hash detects corruption and mismatched release assets. It does not defend against simultaneous compromise of the release workflow and delivery account. GitHub and Cloudflare therefore require hardware-backed MFA, least-privilege credentials, protected tags, immutable releases, a protected release environment, pinned actions, and build attestations.

The Homebrew adapter downloads the official installer from a pinned Git commit and verifies its SHA-256 before execution. Updating that pin requires reviewing and testing the new installer.
