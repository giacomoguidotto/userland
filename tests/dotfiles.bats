#!/usr/bin/env bats

setup() {
  export TEST_ROOT
  TEST_ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export TEST_TMPDIR
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/userland-dotfiles.XXXXXX")
  export USERLAND_ROOT=$TEST_ROOT
  export USERLAND_HOME=$TEST_TMPDIR/home
  export USERLAND_CACHE_DIR=$TEST_TMPDIR/cache
  export USERLAND_DATA_DIR=$TEST_TMPDIR/data
  export USERLAND_STATE_DIR=$TEST_TMPDIR/state
  mkdir -p "$USERLAND_HOME/.config" "$TEST_TMPDIR/workspace/cfg/xdg/gh"
  printf '%s\n' 'hosts stay local' >"$TEST_TMPDIR/workspace/cfg/xdg/gh/hosts.yml"
  printf '%s\n' 'old tracked config' >"$TEST_TMPDIR/workspace/cfg/xdg/gh/config.yml"
  ln -s "$TEST_TMPDIR/workspace/cfg/xdg/gh" "$USERLAND_HOME/.config/gh"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "legacy directory migration preserves only unmanaged children" {
  run sh -c '. "$USERLAND_ROOT/libexec/userland/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -d "$USERLAND_HOME/.config/gh" ]
  [ ! -L "$USERLAND_HOME/.config/gh" ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  grep -q 'hosts stay local' "$USERLAND_HOME/.config/gh/hosts.yml"
}
