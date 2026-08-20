#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=release-lib.sh
. "$script_dir/release-lib.sh"

[ "$#" -eq 2 ] || release_die "usage: $0 OWNER/REPOSITORY OUTPUT_DIRECTORY"

repository=$1
output=$2
[ ! -e "$output" ] || release_die "output already exists: $output"
command -v gh >/dev/null 2>&1 || release_die "gh is required"

mkdir -p "$output"
release_list="$output/releases.json"
gh api --paginate --slurp "repos/$repository/releases?per_page=100" >"$release_list"

jq -r '.[][] | select(.draft == false) | .tag_name' "$release_list" |
  while IFS= read -r tag; do
    release_validate_tag "$tag" || continue
    destination="$output/$tag"
    mkdir -p "$destination"
    gh release download "$tag" \
      --repo "$repository" \
      --pattern bootstrap \
      --pattern checksums.txt \
      --dir "$destination"

    expected=$(awk '$2 == "bootstrap" { print $1 }' "$destination/checksums.txt")
    [ -n "$expected" ] || release_die "$tag checksums omit bootstrap"
    actual=$(release_sha256 "$destination/bootstrap")
    [ "$actual" = "$expected" ] || release_die "$tag bootstrap checksum mismatch"
  done

rm "$release_list"
