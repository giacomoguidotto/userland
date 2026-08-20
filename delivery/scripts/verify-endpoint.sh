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
attempt_limit=${USERLAND_VERIFY_ATTEMPTS:-60}
retry_sleep=${USERLAND_VERIFY_SLEEP:-5}
case "$attempt_limit" in
  '' | *[!0-9]* | 0) release_die "USERLAND_VERIFY_ATTEMPTS must be a positive integer" ;;
esac
case "$retry_sleep" in
  '' | *[!0-9]*) release_die "USERLAND_VERIFY_SLEEP must be a non-negative integer" ;;
esac
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-verify.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

attempt=1
while [ "$attempt" -le "$attempt_limit" ]; do
  status=$(curl --silent --show-error \
    --connect-timeout 5 \
    --max-time 15 \
    --output "$work/body" \
    --dump-header "$work/headers" \
    --write-out '%{http_code}' \
    "$url") || status=000

  if [ "$status" = 200 ] && cmp -s "$expected_file" "$work/body"; then
    break
  fi

  if [ "$attempt" -eq "$attempt_limit" ]; then
    release_die "$url did not return the expected bytes with HTTP 200"
  fi
  sleep "$retry_sleep"
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
