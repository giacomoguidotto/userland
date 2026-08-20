#!/bin/sh
set -eu

test_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
delivery_dir=$(CDPATH='' cd -- "$test_dir/.." && pwd)
port=${USERLAND_DELIVERY_TEST_PORT:-18787}
origin="http://127.0.0.1:$port"
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-cloudflare-test.XXXXXX")
server_pid=

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$work/cache/v1.2.3"
printf '#!/bin/sh\nprintf userland-test\\n\n' >"$work/cache/v1.2.3/bootstrap"
"$delivery_dir/scripts/assemble-site.sh" "$work/cache" v1.2.3 "$work/site"

npm exec --prefix "$delivery_dir/cloudflare" -- wrangler dev \
  --config "$delivery_dir/cloudflare/wrangler.jsonc" \
  --assets "$work/site" \
  --ip 127.0.0.1 \
  --port "$port" \
  --local \
  --persist-to "$work/wrangler-state" \
  --log-level error \
  --show-interactive-dev-session=false \
  >"$work/wrangler.log" 2>&1 &
server_pid=$!

attempt=1
until curl --silent --fail "$origin/v1.2.3" >/dev/null 2>&1; do
  if [ "$attempt" -eq 40 ]; then
    cat "$work/wrangler.log" >&2
    printf 'FAIL: Wrangler did not start\n' >&2
    exit 1
  fi
  sleep 0.25
  attempt=$((attempt + 1))
done

"$delivery_dir/scripts/verify-endpoint.sh" "$origin" "$work/cache/v1.2.3/bootstrap" root
"$delivery_dir/scripts/verify-endpoint.sh" "$origin/v1.2.3" "$work/cache/v1.2.3/bootstrap" version

unknown_status=$(curl --silent --output /dev/null --write-out '%{http_code}' "$origin/v9.9.9")
[ "$unknown_status" = 404 ] || {
  printf 'FAIL: unknown version returned HTTP %s\n' "$unknown_status" >&2
  exit 1
}

slash_status=$(curl --silent --output /dev/null --write-out '%{http_code}' "$origin/v1.2.3/")
[ "$slash_status" = 404 ] || {
  printf 'FAIL: version path redirected or returned HTTP %s\n' "$slash_status" >&2
  exit 1
}

printf 'Cloudflare endpoint tests passed\n'
