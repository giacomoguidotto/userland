<h1 align="center">userland</h1>

<p align="center">
  <strong>A new Mac. My machine, one command later.</strong><br>
  <sub>macOS setup &middot; mise &middot; Homebrew &middot; immutable releases</sub>
</p>

<p align="center">
  <a href="https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml"><img src="https://github.com/giacomoguidotto/userland/actions/workflows/checks.yml/badge.svg" alt="Checks"></a>
  <a href="https://github.com/giacomoguidotto/userland/releases/latest"><img src="https://img.shields.io/github/v/release/giacomoguidotto/userland" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/giacomoguidotto/userland" alt="MIT license"></a>
</p>

<br>

A new Mac should feel new, not unfamiliar.

I built `userland` to turn the blank-machine ritual into one command, then three quiet verbs: plan, sync, doctor. It brings back the tools, applications, preferences, and small decisions that make macOS feel like mine. It also knows what not to touch.

> The machine changes. The way it feels does not.

<p align="center">
  <a href="https://github.com/giacomoguidotto/userland/releases/latest"><strong>See the latest release &rarr;</strong></a>
</p>

## Make it mine

Install the latest stable release:

```sh
curl -fsSL https://userland.guidotto.dev | sh
```

Or pin the first public release:

```sh
curl -fsSL https://userland.guidotto.dev/v0.1.0 | sh
```

The installer downloads a checksum-verified release, runs it, then creates a managed checkout at `~/.local/share/userland/repo`. Later runs use that checkout. They move `main` forward only when the repository is clean and the update is a fast-forward.

## What comes home

- Developer tools and personal applications
- Shell, terminal, editor, Git, SSH, and agent configuration
- Selected macOS defaults, file handlers, Dock items, and login items
- A declared set of Chrome extensions, installed with human approval
- An encrypted Raycast export, imported in Raycast with human approval
- Personal repositories, cloned only when they are missing

Corporate software stays out. So do credentials, browser profiles, histories, caches, application databases, and machine-local authentication.

## Three verbs

| Command | What it does |
| --- | --- |
| `userland plan` | Shows one read-only plan with sources, expected duration, restart implications, and rollback boundaries. Repository discovery is cached for 24 hours. |
| `userland sync` | Shows the same plan, asks once, updates rolling tools and applications, applies declared state, handles attended steps, then runs the doctor. Re-run it after an interruption. |
| `userland doctor` | Checks declared state, attended imports, file handlers, security posture, free disk space, and repository discovery without changing the machine. |

`userland doctor --json` returns the same health report for other tools. There is no separate resume command because `sync` is safe to run again.

## What stays yours

- `sync` never stashes, resets, cleans, or edits another repository.
- Dotfile conflicts stop the run. Automatic migration is limited to links created by the old repository or a verified userland release.
- Directory migrations preserve undeclared children, including local authentication and history.
- Unmanaged packages, applications, files, Dock items, caches, and login items are never pruned.
- Personal repositories listed in `state/repositories.tsv` clone only when missing. Existing checkouts, including dirty ones, are never refreshed or rewritten.
- Browser extensions remain an attended Chrome Web Store step. Userland never copies a browser profile.
- App Store authentication, macOS privacy approvals, licenses, DaVinci Resolve, OpenScreen, Raycast Beta, and Raycast's encrypted import may still need attention.
- Xcode comes from the App Store. Android development uses command-line tools and emulators without Android Studio.

## Under the hood

| Layer | Responsibility |
| --- | --- |
| mise 2026.8.9 | CLI packages, dotfiles, macOS defaults, locked developer tools, and status |
| Homebrew Bundle | Native applications and Xcode records that mise 2026.8.9 cannot currently parse |
| POSIX shell modules | Safe checkout refresh, migration, repository caching, health checks, file handlers, and attended application state |
| GitHub Releases | Immutable, SemVer-tagged release assets |
| Cloudflare Workers Static Assets | The root and exact-version bootstrap endpoints, served without redirects or paid storage |

The design lives in [docs/architecture.md](docs/architecture.md). Release and endpoint details live in [delivery/README.md](delivery/README.md).

## Still becoming

`v0.1.0` is live. A real fresh-Mac parity run is still the final proof.

This is personal configuration in public, not a universal dotfiles framework. Fork what helps. Expect the defaults to be opinionated.

MIT licensed.
