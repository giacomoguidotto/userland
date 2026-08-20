#!/usr/bin/env bats

setup() {
  export TEST_ROOT
  TEST_ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export TEST_TMPDIR
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/userland-test.XXXXXX")
  export USERLAND_HOME=$TEST_TMPDIR/home
  export USERLAND_CACHE_DIR=$TEST_TMPDIR/cache
  export USERLAND_DATA_DIR=$TEST_TMPDIR/data
  export USERLAND_STATE_DIR=$TEST_TMPDIR/state
  export USERLAND_REPO_ROOTS=$TEST_TMPDIR/repos
  export USERLAND_REPOSITORIES=$TEST_TMPDIR/repositories.tsv
  export USERLAND_UNAME=Linux
  export USERLAND_ARCHIVE=1
  export USERLAND_TESTING=1
  export USERLAND_ASSUME_YES=1
  export MISE_CALLS=$TEST_TMPDIR/mise-calls
  mkdir -p "$USERLAND_HOME" "$USERLAND_REPO_ROOTS/example/.git" "$TEST_TMPDIR/bin"
  : >"$USERLAND_REPOSITORIES"

cat >"$TEST_TMPDIR/bin/mise" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$MISE_CALLS"
case "$*" in
  *doctor*--json*) printf '%s\n' '{"healthy":true}' ;;
  *plan*--json*) printf '%s\n' '{"changes":[]}' ;;
esac
exit 0
EOF
  chmod +x "$TEST_TMPDIR/bin/mise"
  export PATH="$TEST_TMPDIR/bin:/usr/bin:/bin:/usr/sbin:/sbin"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "the public interface exposes only plan, sync, and doctor" {
  run "$TEST_ROOT/bin/userland" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"plan"* ]]
  [[ "$output" == *"sync"* ]]
  [[ "$output" == *"doctor"* ]]
  [[ "$output" != *"resume"* ]]

  run "$TEST_ROOT/bin/userland" update
  [ "$status" -eq 64 ]
}

@test "the installed command resolves its managed symlink" {
  mkdir -p "$USERLAND_HOME/.local/bin"
  ln -s "$TEST_ROOT/bin/userland" "$USERLAND_HOME/.local/bin/userland"
  run "$USERLAND_HOME/.local/bin/userland" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"userland <command>"* ]]
}

@test "plan refreshes repository discovery and remains read-only" {
  run "$TEST_ROOT/bin/userland" plan
  [ "$status" -eq 0 ]
  [ -f "$USERLAND_CACHE_DIR/repositories.tsv" ]
  grep -F "$USERLAND_REPO_ROOTS/example" "$USERLAND_CACHE_DIR/repositories.tsv"
  [[ "$output" == *"native tools, dotfiles, and macOS state"* ]]
  [[ "$output" == *"userland adapters"* ]]
  ! grep -q -- '--force-dotfiles' "$MISE_CALLS"
  grep -q 'bootstrap packages apply --dry-run' "$MISE_CALLS"
  grep -q 'bootstrap packages upgrade --dry-run' "$MISE_CALLS"
}

@test "doctor json has a stable schema and no home paths" {
  mkdir -p "$USERLAND_STATE_DIR/receipts"
  run "$TEST_ROOT/bin/userland" doctor --json
  [ "$status" -eq 1 ]
  [[ "$output" == '{"schema_version":1,'* ]]
  [[ "$output" != *"$USERLAND_HOME"* ]]
  printf '%s' "$output" | grep -q '"name":"adapters","status":"attention"'
}

@test "sync uses the pinned interface and creates the shell cache" {
  run "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 0 ]
  [ -s "$USERLAND_CACHE_DIR/zsh/init.zsh" ]
  [[ "$output" == *"missing rolling packages"* ]]
  [[ "$output" == *"upgrading installed rolling packages"* ]]
  [[ "$output" == *"applying declared tools"* ]]
  [[ "$output" == *"verifying the result"* ]]
  grep -q 'bootstrap packages apply --yes' "$MISE_CALLS"
  grep -q 'bootstrap packages upgrade --yes' "$MISE_CALLS"
}

@test "fresh Mac paths include Homebrew before bootstrap runs" {
  export USERLAND_ROOT=$TEST_ROOT
  run sh -c '. "$USERLAND_ROOT/libexec/userland/common.sh"; case ":$PATH:" in *:/opt/homebrew/bin:/opt/homebrew/sbin:*) exit 0 ;; *) exit 1 ;; esac'
  [ "$status" -eq 0 ]
}

@test "the committed Raycast export has an encrypted envelope" {
  export USERLAND_ROOT=$TEST_ROOT
  export USERLAND_UNAME=Darwin
  run "$TEST_ROOT/libexec/userland/adapters/raycast.sh" plan
  [ "$status" -eq 0 ]
  [[ "$output" == *"encrypted configuration import"* ]]
}
