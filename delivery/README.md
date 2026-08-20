# Release delivery

GitHub Releases hold the canonical release assets. Cloudflare Workers Static Assets serves those same bootstrap bytes at two direct endpoints:

```sh
curl https://userland.guidotto.dev | sh
curl https://userland.guidotto.dev/v1.2.3 | sh
```

Both requests return `200` with `Content-Type: text/plain`. They do not redirect. The root has a revalidation cache policy. A version endpoint has a one-year immutable cache policy.

This design uses public GitHub Releases and Cloudflare Workers Static Assets. It needs no paid CDN or object-storage service. Normal Cloudflare account limits still apply.

## Release flow

Push a strict SemVer tag such as `v1.2.3` or `v1.3.0-rc.1`. The workflow then:

1. verifies the tag and tests the delivery scripts;
2. generates a mise v2026.8.9 runner and injects it into the source archive;
3. creates `bootstrap`, `checksums.txt`, and `userland-vX.Y.Z.tar.gz`;
4. attests the assets and publishes the GitHub Release;
5. rebuilds the Cloudflare asset directory from every published release;
6. deploys and verifies `/vX.Y.Z` without changing `/`;
7. promotes the same bootstrap bytes to `/` only for a stable release.

The workflow rejects a stable version lower than the current latest stable. A prerelease never changes `/`.

A failed Cloudflare deploy is safe to rerun. The workflow resumes a matching draft or accepts an existing immutable release only when all three release assets are byte-identical to a fresh build.

The archive builder sorts entries and replaces host metadata with the release commit time, uid and gid `0`, and empty owner names. `SOURCE_DATE_EPOCH` can replace the commit time. The release check builds twice and requires byte-identical launchers, checksums, and archives.

The bootstrap downloads its exact GitHub Release archive with redirect following enabled inside the script. It checks the embedded SHA-256 before extraction. Before sync starts, it atomically links `~/.local/bin/userland` to the verified release. It refuses to overwrite a regular file or an unmanaged symlink at that path.

The bootstrap runs the release in archive mode and then clones the public repository over HTTPS. Sync exit `2` means the machine needs attention, so checkout setup still finishes and the installer exits successfully with a clear rerun message. Any other nonzero sync result stops before the clone, while the release-backed command remains available. Homebrew's Apple Silicon paths are available during this handoff.

A successful checkout leaves local `main` at the released commit tracking `origin/main`, and the command link moves to that checkout. On a rerun, bootstrap does not fetch into or edit an existing checkout. It requires the canonical HTTPS origin, a clean work tree, `main` tracking `origin/main`, the released commit in the checkout history, and a HEAD that remains on GitHub's current `main` history. It validates those facts before trusting `mise.toml` or linking the checkout command. Later `userland sync` runs can advance a valid checkout with a fast-forward-only update.

## GitHub safeguards

The public source is `giacomoguidotto/userland`. Its repository settings:

- make every published GitHub Release immutable;
- prevent `v*` tags from being deleted or moved;
- require Giacomo's approval in the `release` environment;
- allow pushed releases from `v*` tags and recovery dispatches from `main`.

## One-time Cloudflare setup

These settings require a Cloudflare administrator:

- Add `CLOUDFLARE_ACCOUNT_ID` and a scoped `CLOUDFLARE_API_TOKEN` as `release` environment secrets. The token only needs permission to deploy this Worker and manage its custom domain.
- Confirm that `userland.guidotto.dev` belongs to the same Cloudflare account. Wrangler creates the Worker custom-domain record during the first deploy.

The release environment is the only job with write permissions. Pull-request checks receive read-only repository access. Third-party actions use full commit SHAs.

Run the local checks with:

```sh
delivery/tests/release-delivery.test.sh
npm ci --ignore-scripts --prefix delivery/cloudflare
delivery/tests/cloudflare.test.sh
```
