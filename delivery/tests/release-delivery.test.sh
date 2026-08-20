#!/bin/sh
set -eu

test_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
delivery_dir=$(CDPATH='' cd -- "$test_dir/.." && pwd)
release_workflow="$delivery_dir/../.github/workflows/release.yml"
# shellcheck source=../scripts/release-lib.sh
. "$delivery_dir/scripts/release-lib.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

if grep -Fq 'immutable-releases' "$release_workflow"; then
  fail "release workflow calls the admin-only immutable-release settings endpoint"
fi
grep -Fq 'isImmutable' "$release_workflow" || fail "published release immutability check missing"

for tag in v0.0.0 v1.2.3 v1.3.0-rc.1 v10.20.30-alpha-1; do
  release_validate_tag "$tag" || fail "valid tag rejected: $tag"
done

for tag in 1.2.3 v1 v1.2 v01.2.3 v1.02.3 v1.2.03 v1.2.3-01 v1.2.3+build v1.2.3- v1.2.3-rc..1; do
  if release_validate_tag "$tag"; then
    fail "invalid tag accepted: $tag"
  fi
done

work=$(mktemp -d "${TMPDIR:-/tmp}/userland-delivery-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

mkdir -p "$work/verify-bin"
cat >"$work/verify-bin/curl" <<'EOF'
#!/bin/sh
set -eu
output=
headers=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift 2 ;;
    --dump-header) headers=$2; shift 2 ;;
    --write-out) shift 2 ;;
    *) shift ;;
  esac
done
attempt=$(cat "$VERIFY_COUNTER")
attempt=$((attempt + 1))
printf '%s\n' "$attempt" >"$VERIFY_COUNTER"
if [ "$attempt" -lt 13 ]; then
  printf 'certificate pending\n' >"$output"
  : >"$headers"
  printf '000'
  exit 0
fi
cp "$VERIFY_EXPECTED" "$output"
printf 'HTTP/2 200\nContent-Type: text/plain\nCache-Control: public, max-age=31536000, immutable\n' >"$headers"
printf '200'
EOF
chmod +x "$work/verify-bin/curl"
printf '0\n' >"$work/verify-counter"
printf 'bootstrap bytes\n' >"$work/verify-expected"
PATH="$work/verify-bin:$PATH" \
  VERIFY_COUNTER="$work/verify-counter" \
  VERIFY_EXPECTED="$work/verify-expected" \
  USERLAND_VERIFY_ATTEMPTS=13 \
  USERLAND_VERIFY_SLEEP=0 \
  "$delivery_dir/scripts/verify-endpoint.sh" \
  https://userland.example/v0.1.0 \
  "$work/verify-expected" \
  version || fail "endpoint verifier did not outlast certificate provisioning"
[ "$(cat "$work/verify-counter")" = 13 ] || fail "endpoint verifier did not use the configured retry budget"

mkdir -p "$work/cache/v1.0.0" "$work/cache/v1.1.0-rc.1" "$work/cache/v1.1.0"
printf '#!/bin/sh\nprintf stable-one\\n\n' >"$work/cache/v1.0.0/bootstrap"
printf '#!/bin/sh\nprintf candidate\\n\n' >"$work/cache/v1.1.0-rc.1/bootstrap"
printf '#!/bin/sh\nprintf stable-two\\n\n' >"$work/cache/v1.1.0/bootstrap"

"$delivery_dir/scripts/assemble-site.sh" "$work/cache" v1.0.0 "$work/exact-site"
cmp -s "$work/cache/v1.0.0/bootstrap" "$work/exact-site/index.html" || fail "prior stable root changed"
cmp -s "$work/cache/v1.1.0/bootstrap" "$work/exact-site/v1.1.0" || fail "exact stable endpoint missing"
cmp -s "$work/cache/v1.1.0-rc.1/bootstrap" "$work/exact-site/v1.1.0-rc.1" || fail "prerelease endpoint missing"

"$delivery_dir/scripts/assemble-site.sh" "$work/cache" v1.1.0 "$work/promoted-site"
cmp -s "$work/cache/v1.1.0/bootstrap" "$work/promoted-site/index.html" || fail "stable root was not promoted"

grep -Fq 'max-age=0, must-revalidate' "$work/promoted-site/_headers" || fail "root cache policy missing"
grep -Fq 'max-age=31536000, immutable' "$work/promoted-site/_headers" || fail "version cache policy missing"

for script in "$delivery_dir"/scripts/*.sh "$test_dir"/*.sh; do
  sh -n "$script"
done
sh -n "$delivery_dir/bootstrap/bootstrap.sh.in"
python3 "$test_dir/archive-reproducibility.test.py"
"$test_dir/bootstrap.test.sh"

printf 'release delivery tests passed\n'
