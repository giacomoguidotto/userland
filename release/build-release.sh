#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=release-lib.sh
. "$script_dir/release-lib.sh"

[ "$#" -eq 3 ] || release_die "usage: $0 TAG COMMIT OUTPUT_DIRECTORY"

tag=$1
commit=$2
output=$3

release_validate_tag "$tag" || release_die "invalid SemVer tag: $tag"
[ ! -e "$output" ] || release_die "output already exists: $output"
git cat-file -e "$commit^{commit}" 2>/dev/null || release_die "unknown commit: $commit"
if git cat-file -e "$commit:bin/mise" 2>/dev/null; then
  release_die "bin/mise is release-generated and must not be tracked"
fi
command -v mise >/dev/null 2>&1 || release_die "mise is required to build a release"
command -v go >/dev/null 2>&1 || release_die "Go is required to build a release"
command -v python3 >/dev/null 2>&1 || release_die "python3 is required to build a release"

mise_version=2026.8.9
actual_mise_version=$(mise --version | awk '{ print $1 }')
[ "$actual_mise_version" = "$mise_version" ] ||
  release_die "mise $mise_version is required, found $actual_mise_version"

version=${tag#v}
archive="userland-$tag.tar.gz"
mkdir -p "$output"
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-release.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$commit")}
case "$epoch" in
  '' | *[!0-9]*) release_die "SOURCE_DATE_EPOCH must be a non-negative integer" ;;
esac

release_tree=$work/tree/userland-$version
mkdir -p "$release_tree/bin"
git archive --format=tar "$commit" LICENSE cmd completions cfg go.mod go.sum internal plan userland.go mise.lock mise.toml |
  tar -xf - -C "$release_tree"

if [ -f .gitmodules ]; then
  git config --file .gitmodules --get-regexp '^[^.]+\..*\.path$' |
    while read -r _submodule_key submodule_path; do
      expected_submodule_commit=$(git rev-parse "$commit:$submodule_path")
      [ -d "$submodule_path" ] || release_die "submodule is not initialized: $submodule_path"
      actual_submodule_commit=$(git -C "$submodule_path" rev-parse HEAD)
      [ "$actual_submodule_commit" = "$expected_submodule_commit" ] ||
        release_die "submodule checkout does not match $commit: $submodule_path"
      mkdir -p "$release_tree/$submodule_path"
      git -C "$submodule_path" archive "$expected_submodule_commit" |
        tar -xf - -C "$release_tree/$submodule_path"
    done
fi

CGO_ENABLED=0 go -C "$release_tree" build \
  -trimpath \
  -buildvcs=false \
  -ldflags='-s -w' \
  -o bin/userland \
  ./cmd/userland
rm -rf \
  "$release_tree/cmd" \
  "$release_tree/internal" \
  "$release_tree/plan" \
  "$release_tree/go.mod" \
  "$release_tree/go.sum" \
  "$release_tree/userland.go"

mise generate bootstrap --version "$mise_version" --write "$release_tree/bin/mise" >/dev/null
python3 "$script_dir/create-archive.py" "$release_tree" "$output/$archive" "$epoch"

archive_sum=$(release_sha256 "$output/$archive")
commit=$(git rev-parse "$commit^{commit}")
sed \
  -e "s|@USERLAND_TAG@|$tag|g" \
  -e "s|@USERLAND_COMMIT@|$commit|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256@|$archive_sum|g" \
  "$script_dir/bootstrap-template.sh" >"$output/bootstrap"
chmod 0755 "$output/bootstrap"

(
  cd "$output"
  bootstrap_sum=$(release_sha256 bootstrap)
  printf '%s  %s\n' "$bootstrap_sum" bootstrap
  printf '%s  %s\n' "$archive_sum" "$archive"
) >"$output/checksums.txt"
