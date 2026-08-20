#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=release-lib.sh
. "$script_dir/release-lib.sh"

[ "$#" -eq 3 ] || release_die "usage: $0 URL EXPECTED_FILE root|version"

url=$1
expected_file=$2
kind=$3
case "$kind" in
  root | version) ;;
  *) release_die "endpoint kind must be root or version" ;;
esac

[ -f "$expected_file" ] || release_die "expected file does not exist: $expected_file"
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-verify.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

attempt=1
while [ "$attempt" -le 12 ]; do
  status=$(curl --silent --show-error \
    --output "$work/body" \
    --dump-header "$work/headers" \
    --write-out '%{http_code}' \
    "$url") || status=000

  if [ "$status" = 200 ] && cmp -s "$expected_file" "$work/body"; then
    break
  fi

  if [ "$attempt" -eq 12 ]; then
    release_die "$url did not return the expected bytes with HTTP 200"
  fi
  sleep 5
  attempt=$((attempt + 1))
done

tr -d '\r' <"$work/headers" | grep -Eiq '^content-type:[[:space:]]*text/plain([;[:space:]]|$)' ||
  release_die "$url did not return text/plain"

if [ "$kind" = root ]; then
  cache_pattern='^cache-control:.*max-age=0.*must-revalidate'
else
  cache_pattern='^cache-control:.*max-age=31536000.*immutable'
fi

tr -d '\r' <"$work/headers" | grep -Eiq "$cache_pattern" ||
  release_die "$url returned the wrong Cache-Control policy"

printf 'verified %s\n' "$url"
