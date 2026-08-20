#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=release-lib.sh
. "$script_dir/release-lib.sh"

[ "$#" -eq 3 ] || release_die "usage: $0 RELEASE_CACHE ROOT_TAG_OR_NONE OUTPUT_DIRECTORY"

cache=$1
root_tag=$2
output=$3

[ -d "$cache" ] || release_die "release cache does not exist: $cache"
[ ! -e "$output" ] || release_die "output already exists: $output"

mkdir -p "$output"
found=false

for release_dir in "$cache"/v*; do
  [ -d "$release_dir" ] || continue
  tag=${release_dir##*/}
  release_validate_tag "$tag" || release_die "invalid release cache directory: $tag"
  [ -f "$release_dir/bootstrap" ] || release_die "$tag has no bootstrap"
  cp "$release_dir/bootstrap" "$output/$tag"
  chmod 0644 "$output/$tag"
  found=true
done

[ "$found" = true ] || release_die "release cache contains no SemVer releases"

if [ "$root_tag" != none ]; then
  release_validate_tag "$root_tag" || release_die "invalid root tag: $root_tag"
  [ -f "$cache/$root_tag/bootstrap" ] || release_die "root release is missing: $root_tag"
  cp "$cache/$root_tag/bootstrap" "$output/index.html"
  chmod 0644 "$output/index.html"
fi

cp "$script_dir/cloudflare/_headers" "$output/_headers"
