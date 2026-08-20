#!/usr/bin/env bats

setup() {
  export TEST_ROOT
  TEST_ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export TEST_TMPDIR
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/userland-homebrew-test.XXXXXX")
  export USERLAND_ROOT=$TEST_ROOT
  export USERLAND_HOME=$TEST_TMPDIR/home
  export USERLAND_CACHE_DIR=$TEST_TMPDIR/cache
  export USERLAND_DATA_DIR=$TEST_TMPDIR/data
  export USERLAND_STATE_DIR=$TEST_TMPDIR/state
  export USERLAND_UNAME=Darwin
  export USERLAND_BREW=$TEST_TMPDIR/brew
  export BREW_CALLS=$TEST_TMPDIR/calls

  cat >"$USERLAND_BREW" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$BREW_CALLS"
case "$*" in
  --version) printf '%s\n' 'Homebrew 5.0.0' ;;
esac
exit 0
EOF
  chmod +x "$USERLAND_BREW"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "Homebrew plan and doctor never update metadata" {
  run "$TEST_ROOT/libexec/userland/adapters/homebrew-apps.sh" plan
  [ "$status" -eq 0 ]

  run "$TEST_ROOT/libexec/userland/adapters/homebrew-apps.sh" doctor
  [ "$status" -eq 0 ]

  ! grep -q '^update$' "$BREW_CALLS"
  grep -q 'bundle check.*--no-upgrade' "$BREW_CALLS"
}

@test "Homebrew apply updates and converges without cleanup" {
  run "$TEST_ROOT/libexec/userland/adapters/homebrew-apps.sh" apply
  [ "$status" -eq 0 ]
  grep -q '^update$' "$BREW_CALLS"
  grep -q '^bundle --file ' "$BREW_CALLS"
  ! grep -q 'cleanup' "$BREW_CALLS"
}
