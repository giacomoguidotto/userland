#!/usr/bin/env bats

setup_file() {
  export TEST_ROOT
  TEST_ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export USERLAND_COMPAT_ROOT=$TEST_ROOT
  export USERLAND_ORACLE_ROOT=$BATS_FILE_TMPDIR/oracle
  mkdir -p "$USERLAND_ORACLE_ROOT"
  git -C "$TEST_ROOT" archive "$(sed -n '1p' "$TEST_ROOT/tests/compat/oracle-version")" \
    bin completions config lib mise.lock mise.toml | tar -xf - -C "$USERLAND_ORACLE_ROOT"
  export USERLAND_GO_BIN=$BATS_FILE_TMPDIR/userland-go
  go build -o "$USERLAND_GO_BIN" ./cmd/userland
  export USERLAND_GO_PLAN_BIN=$BATS_FILE_TMPDIR/userland-go-plan
  go build -o "$USERLAND_GO_PLAN_BIN" ./tests/compat/go-plan
}

prepare_shell_cache_commands() {
  mkdir -p "$USERLAND_HOME/.local/bin"
  printf '%s\n' \
    '#!/bin/sh' \
    'printf "fixture %s\\n" "$*"' >"$USERLAND_HOME/.local/bin/shell-cache-command"
  chmod +x "$USERLAND_HOME/.local/bin/shell-cache-command"
  for command in atuin carapace direnv fzf starship zoxide; do
    ln -s shell-cache-command "$USERLAND_HOME/.local/bin/$command"
  done
}

setup() {
  export TEST_TMPDIR
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/userland-compat.XXXXXX")
  export USERLAND_HOME=$TEST_TMPDIR/home
  export USERLAND_CACHE_DIR=$TEST_TMPDIR/cache
  export USERLAND_DATA_DIR=$TEST_TMPDIR/data
  export USERLAND_STATE_DIR=$TEST_TMPDIR/state
  export USERLAND_REPO_ROOTS=$TEST_TMPDIR/repos
  export USERLAND_REPOSITORIES=$TEST_TMPDIR/repositories.csv
  export USERLAND_UNAME=Linux
  export USERLAND_ARCHIVE=1
  export USERLAND_ASSUME_YES=1
  export USERLAND_ROOT=$TEST_ROOT
  export USERLAND_TESTING=1
  export USERLAND_MISE=$TEST_TMPDIR/bin/mise
  export USERLAND_CURL=$TEST_TMPDIR/bin/curl
  export MISE_CALLS=$TEST_TMPDIR/mise-calls
  export MISE_CONFIG_CALLS=$TEST_TMPDIR/mise-config-calls
  export TEST_USERLAND_RELEASE_TAG=v9.9.9
  export USERLAND_VERSION=v0.2.3
  export TEST_USERLAND_RELEASE_COMMIT
  TEST_USERLAND_RELEASE_COMMIT=$(git -C "$TEST_ROOT" rev-parse HEAD)
  printf '%s\n' "$TEST_USERLAND_RELEASE_COMMIT" >"$USERLAND_ORACLE_ROOT/.userland-release"
  mkdir -p "$USERLAND_HOME" "$USERLAND_CACHE_DIR" "$USERLAND_DATA_DIR" "$USERLAND_STATE_DIR/receipts" "$TEST_TMPDIR/bin" "$USERLAND_REPO_ROOTS/example/.git"
  : >"$USERLAND_REPOSITORIES"
  prepare_shell_cache_commands

  printf '%s\n' \
    '#!/bin/sh' \
    'printf "%s\\n" "$*" >>"$MISE_CALLS"' \
    'printf "%s|%s\\n" "${MISE_OVERRIDE_CONFIG_FILENAMES:-}" "$*" >>"$MISE_CONFIG_CALLS"' \
    'case "$*" in' \
    '  *bin-paths*) : ;;' \
    '  *env*--json*) printf "%s\\n" '\''{}'\'' ;;' \
    '  *bootstrap*packages*apply*) [ "${TEST_PACKAGES_FAIL:-0}" = 0 ] || exit 9 ;;' \
    '  *bootstrap*status*--missing*) exit 0 ;;' \
    '  *bootstrap*plan*--json*) printf "%s\\n" '\''{"resources":[],"summary":{"create":0,"update":0,"remove":0,"unchanged":0,"unknown":0}}'\'' ;;' \
    '  *bootstrap*dotfiles*status*--json*) printf "%s\\n" '\''{"files":[],"edits":[]}'\'' ;;' \
    '  *bootstrap*macos*defaults*status*--json*) printf "%s\\n" '\''{"macos_defaults":{"entries":[],"available":true}}'\'' ;;' \
    'esac' \
    'exit 0' >"$USERLAND_MISE"
  chmod +x "$USERLAND_MISE"

  printf '%s\n' \
    '#!/bin/sh' \
    'printf "%s\\n" "#!/bin/sh" "set -eu" "" "tag='\''$TEST_USERLAND_RELEASE_TAG'\''" "commit='\''$TEST_USERLAND_RELEASE_COMMIT'\''"' \
    >"$USERLAND_CURL"
  chmod +x "$USERLAND_CURL"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

capture() {
  local implementation=$1
  local prefix=$2
  shift 2

  local status=0
  "$implementation" "$@" >"$prefix.stdout" 2>"$prefix.stderr" || status=$?
  printf '%s\n' "$status" >"$prefix.status"
}

assert_compatible() {
  local oracle=$TEST_TMPDIR/oracle
  local port=$TEST_TMPDIR/port

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$oracle" "$@"
  capture "$USERLAND_GO_BIN" "$port" "$@"

  diff -u "$oracle.stdout" "$port.stdout"
  diff -u "$oracle.stderr" "$port.stderr"
  diff -u "$oracle.status" "$port.status"
}

assert_compatible_with_realm_additions() {
  local kind=$1
  shift
  local oracle=$TEST_TMPDIR/oracle
  local port=$TEST_TMPDIR/port

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$oracle" "$@"
  capture "$USERLAND_GO_BIN" "$port" "$@"
  python3 "$TEST_ROOT/tests/compat/strip-realm.py" "$kind" "$port.stdout" >"$port.compat.stdout"
  python3 "$TEST_ROOT/tests/compat/strip-realm.py" "$kind" "$port.stderr" >"$port.compat.stderr"

  diff -u "$oracle.stdout" "$port.compat.stdout"
  diff -u "$oracle.stderr" "$port.compat.stderr"
  diff -u "$oracle.status" "$port.status"
}

reset_machine_fixture() {
  rm -rf "$USERLAND_HOME" "$USERLAND_CACHE_DIR" "$USERLAND_DATA_DIR" "$USERLAND_STATE_DIR" "$USERLAND_REPO_ROOTS"
  mkdir -p "$USERLAND_HOME" "$USERLAND_CACHE_DIR" "$USERLAND_DATA_DIR" "$USERLAND_STATE_DIR/receipts" "$USERLAND_REPO_ROOTS/example/.git"
  prepare_shell_cache_commands
  : >"$MISE_CALLS"
}

@test "the port preserves help and usage bytes" {
  export USERLAND_VERSION=v0.2.3
  export NO_COLOR=1

  USERLAND_UI_MODE=plain assert_compatible_with_realm_additions help --help
  USERLAND_UI_MODE=rich USERLAND_UNICODE=0 assert_compatible_with_realm_additions help --help
  USERLAND_UI_MODE=rich USERLAND_UNICODE=1 assert_compatible_with_realm_additions help --help
  USERLAND_UI_MODE=plain assert_compatible_with_realm_additions help unknown

  USERLAND_UI_MODE=plain "$USERLAND_GO_BIN" --help >"$TEST_TMPDIR/realm-help"
  grep -q 'realm add <repository> <path>' "$TEST_TMPDIR/realm-help"
  grep -q 'realm remove <name-or-path>' "$TEST_TMPDIR/realm-help"
}

@test "the port preserves completion bytes and errors" {
  export USERLAND_UI_MODE=plain
  export NO_COLOR=1

  for shell in bash fish nushell zsh; do
    assert_compatible_with_realm_additions completion completions "$shell"
    "$USERLAND_GO_BIN" completions "$shell" >"$TEST_TMPDIR/realm-$shell"
    grep -q realm "$TEST_TMPDIR/realm-$shell"
    grep -q add "$TEST_TMPDIR/realm-$shell"
    grep -q remove "$TEST_TMPDIR/realm-$shell"
  done
  assert_compatible completions
  assert_compatible completions powershell
}

@test "the port preserves command argument validation" {
  export USERLAND_UI_MODE=plain
  export NO_COLOR=1

  assert_compatible plan extra
  assert_compatible sync extra
  assert_compatible doctor --bad
  assert_compatible doctor --json extra
}

@test "the port preserves the doctor JSON contract" {
  export USERLAND_UI_MODE=plain
  export NO_COLOR=1

  assert_compatible doctor --json
}

@test "the typed plan preserves ordering, de-duplication, and rendering bytes" {
  export NO_COLOR=1

  for mode in plain rich; do
    for unicode in 0 1; do
      USERLAND_UI_MODE=$mode USERLAND_UNICODE=$unicode \
        "$TEST_ROOT/tests/compat/plan-oracle.sh" >"$TEST_TMPDIR/oracle.plan"
      USERLAND_UI_MODE=$mode USERLAND_UNICODE=$unicode \
        "$USERLAND_GO_PLAN_BIN" >"$TEST_TMPDIR/port.plan"
      python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.plan" --legacy-ui >"$TEST_TMPDIR/oracle.expected.plan"
      diff -u "$TEST_TMPDIR/oracle.expected.plan" "$TEST_TMPDIR/port.plan"
    done
  done
}

@test "the port preserves PTY auto-mode, Unicode, and color bytes" {
  python3 "$TEST_ROOT/tests/compat/pty.py" "$USERLAND_ORACLE_ROOT/bin/userland" "$USERLAND_GO_BIN"
}

@test "the port preserves the complete read-only plan" {
  export NO_COLOR=1

  for mode in plain rich; do
    for unicode in 0 1; do
      export USERLAND_UI_MODE=$mode USERLAND_UNICODE=$unicode
      rm -f "$USERLAND_CACHE_DIR/repositories.csv" "$USERLAND_CACHE_DIR/repositories.meta"
      capture "$USERLAND_ORACLE_ROOT/bin/userland" "$TEST_TMPDIR/oracle" plan
      rm -f "$USERLAND_CACHE_DIR/repositories.csv" "$USERLAND_CACHE_DIR/repositories.meta"
      capture "$USERLAND_GO_BIN" "$TEST_TMPDIR/port" plan
      python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.stdout" --legacy-ui >"$TEST_TMPDIR/oracle.normalized"
      python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/port.stdout" >"$TEST_TMPDIR/port.normalized"
      diff -u "$TEST_TMPDIR/oracle.normalized" "$TEST_TMPDIR/port.normalized"
      diff -u "$TEST_TMPDIR/oracle.stderr" "$TEST_TMPDIR/port.stderr"
      diff -u "$TEST_TMPDIR/oracle.status" "$TEST_TMPDIR/port.status"
    done
  done
}

@test "the compatibility boundary preserves human doctor output" {
  export USERLAND_UI_MODE=plain
  export NO_COLOR=1

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$TEST_TMPDIR/oracle" doctor
  capture "$USERLAND_GO_BIN" "$TEST_TMPDIR/port" doctor
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.stdout" --legacy-ui >"$TEST_TMPDIR/oracle.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/port.stdout" >"$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.normalized" "$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.stderr" "$TEST_TMPDIR/port.stderr"
  diff -u "$TEST_TMPDIR/oracle.status" "$TEST_TMPDIR/port.status"
}

@test "the port isolates personal Mise configuration" {
  export USERLAND_UI_MODE=plain NO_COLOR=1
  : >"$MISE_CONFIG_CALLS"

  "$USERLAND_GO_BIN" plan >/dev/null

  [ -s "$MISE_CONFIG_CALLS" ]
  while IFS='|' read -r config arguments; do
    [ "$config" = mise.toml ]
    case "$arguments" in
      "-C $TEST_ROOT/cfg "*) ;;
      *) false ;;
    esac
  done <"$MISE_CONFIG_CALLS"
}

@test "sync preserves bootstrap lock refusal" {
  mkdir -p "$USERLAND_DATA_DIR/bootstrap.lock"
  printf '%s\n' bootstrap-owner >"$USERLAND_DATA_DIR/bootstrap.lock/owner"
  export USERLAND_UI_MODE=plain NO_COLOR=1

  assert_compatible sync
}

@test "sync preserves explicit no consent without applying" {
  export USERLAND_UI_MODE=plain NO_COLOR=1
  export USERLAND_ASSUME_YES=
  export USERLAND_UI_TEST_CONFIRMATION=no

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$TEST_TMPDIR/oracle" sync
  reset_machine_fixture
  capture "$USERLAND_GO_BIN" "$TEST_TMPDIR/port" sync
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.stdout" --legacy-ui >"$TEST_TMPDIR/oracle.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/port.stdout" >"$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.normalized" "$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.stderr" "$TEST_TMPDIR/port.stderr"
  diff -u "$TEST_TMPDIR/oracle.status" "$TEST_TMPDIR/port.status"
  ! grep -q 'bootstrap packages apply' "$MISE_CALLS"
}

@test "sync preserves apply ordering and successful convergence" {
  export USERLAND_UI_MODE=plain NO_COLOR=1
  export USERLAND_ASSUME_YES=1

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$TEST_TMPDIR/oracle" sync
  cp "$MISE_CALLS" "$TEST_TMPDIR/oracle.calls"
  reset_machine_fixture
  capture "$USERLAND_GO_BIN" "$TEST_TMPDIR/port" sync
  cp "$MISE_CALLS" "$TEST_TMPDIR/port.calls"

  sed "s|$USERLAND_ORACLE_ROOT|<root>|g" "$TEST_TMPDIR/oracle.calls" >"$TEST_TMPDIR/oracle.calls.normalized"
  sed \
    -e "s|$TEST_ROOT/cfg|<root>|g" \
    -e "s|$TEST_ROOT|<root>|g" \
    -e 's/^-C <root> doctor$/doctor/' \
    -e 's/^-C <root> --version$/--version/' \
    -e '/^-C <root> bin-paths$/d' \
    -e '/^-C <root> env --json$/d' \
    "$TEST_TMPDIR/port.calls" >"$TEST_TMPDIR/port.calls.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.stdout" --legacy-ui >"$TEST_TMPDIR/oracle.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/port.stdout" >"$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.normalized" "$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.stderr" "$TEST_TMPDIR/port.stderr"
  diff -u "$TEST_TMPDIR/oracle.status" "$TEST_TMPDIR/port.status"
  diff -u "$TEST_TMPDIR/oracle.calls.normalized" "$TEST_TMPDIR/port.calls.normalized"
}

@test "sync preserves package failure status and stops subsequent work" {
  export USERLAND_UI_MODE=plain NO_COLOR=1
  export USERLAND_ASSUME_YES=1
  export TEST_PACKAGES_FAIL=1

  capture "$USERLAND_ORACLE_ROOT/bin/userland" "$TEST_TMPDIR/oracle" sync
  cp "$MISE_CALLS" "$TEST_TMPDIR/oracle.calls"
  reset_machine_fixture
  capture "$USERLAND_GO_BIN" "$TEST_TMPDIR/port" sync
  cp "$MISE_CALLS" "$TEST_TMPDIR/port.calls"

  sed "s|$USERLAND_ORACLE_ROOT|<root>|g" "$TEST_TMPDIR/oracle.calls" >"$TEST_TMPDIR/oracle.calls.normalized"
  sed \
    -e "s|$TEST_ROOT/cfg|<root>|g" \
    -e "s|$TEST_ROOT|<root>|g" \
    "$TEST_TMPDIR/port.calls" >"$TEST_TMPDIR/port.calls.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/oracle.stdout" --legacy-ui >"$TEST_TMPDIR/oracle.normalized"
  python3 "$TEST_ROOT/tests/compat/normalize.py" "$TEST_TMPDIR/port.stdout" >"$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.normalized" "$TEST_TMPDIR/port.normalized"
  diff -u "$TEST_TMPDIR/oracle.stderr" "$TEST_TMPDIR/port.stderr"
  diff -u "$TEST_TMPDIR/oracle.status" "$TEST_TMPDIR/port.status"
  diff -u "$TEST_TMPDIR/oracle.calls.normalized" "$TEST_TMPDIR/port.calls.normalized"
  ! grep -q 'bootstrap --yes --only tools' "$TEST_TMPDIR/port.calls"
}
