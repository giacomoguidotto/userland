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
if [ "${TEST_CHECK_BOOTSTRAP_CHECKPOINT:-0}" = 1 ]; then
  case "$*" in
    *bootstrap*plan*--json*) [ ! -e "$USERLAND_BOOTSTRAP_CONTROL/apply-started" ] || exit 91 ;;
    *bootstrap*packages*apply*)
      [ "$(cat "$USERLAND_BOOTSTRAP_CONTROL/apply-started" 2>/dev/null)" = "$USERLAND_BOOTSTRAP_TOKEN" ] || exit 92
      ;;
  esac
fi
case "$*" in
  *doctor*--json*) printf '%s\n' '{"healthy":true}' ;;
  *bootstrap*plan*--json*) printf '%s\n' '{"resources":[],"summary":{"create":0,"update":0,"remove":0,"unchanged":0,"unknown":0}}' ;;
  *bootstrap*dotfiles*status*--json*) printf '%s\n' '{"files":[],"edits":[]}' ;;
  *bootstrap*macos*defaults*status*--json*) printf '%s\n' '{"macos_defaults":{"entries":[],"available":true}}' ;;
esac
exit 0
EOF
  chmod +x "$TEST_TMPDIR/bin/mise"
  cat >"$TEST_TMPDIR/bin/failing-task" <<'EOF'
#!/bin/sh
printf '%s\n' first 'last failure'
exit 7
EOF
  chmod +x "$TEST_TMPDIR/bin/failing-task"
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

@test "direct sync refuses a checkout owned by an active bootstrap" {
  mkdir -p "$USERLAND_DATA_DIR/bootstrap.lock"
  printf '%s\n' bootstrap-owner >"$USERLAND_DATA_DIR/bootstrap.lock/owner"

  run "$TEST_ROOT/bin/userland" sync

  [ "$status" -eq 1 ]
  [[ "$output" == *"finish or cancel that run first"* ]]
  [ ! -s "$MISE_CALLS" ]
}

@test "plain output is structured, stable, and free of terminal control bytes" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=plain \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command plan "Preview declared state"; userland_ui section "Applications"; userland_log current "Home is $USERLAND_HOME"; userland_ui summary ok "Plan complete"'

  [ "$status" -eq 0 ]
  [[ "$output" == *"userland plan: Preview declared state"* ]]
  [[ "$output" == *"== Applications"* ]]
  [[ "$output" == *"[ok] Home is ~"* ]]
  [[ "$output" == *"[ok] Plan complete"* ]]
  [[ "$output" != *$'\e['* ]]
  [[ "$output" != *"$USERLAND_HOME"* ]]
}

@test "rich output uses restrained status marks and respects NO_COLOR" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    TERM=xterm-256color \
    NO_COLOR= \
    CLICOLOR_FORCE=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command doctor "Check this Mac"; userland_ui section "Security"; userland_log healthy "FileVault is on"'

  [ "$status" -eq 0 ]
  [[ "$output" == *"userland"*"doctor"* ]]
  [[ "$output" == *"▗▖ ▗▖ ▗▄▄▖"*"▝▚▄▞▘▗▄▄▞▘"* ]]
  [[ "$output" == *$'\n ┌'* ]]
  [[ "$output" == *$'\n │'* ]]
  [[ "$output" == *$'\n ◆  Security'* ]]
  [[ "$output" == *"✓"*"FileVault is on"* ]]
  [[ "$output" == *$'\e['* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    TERM=xterm-256color \
    CLICOLOR_FORCE=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_log attention "Manual approval remains"'

  [ "$status" -eq 0 ]
  [[ "$output" == *"!  Manual approval remains"* ]]
  [[ "$output" != *$'\e['* ]]
}

@test "the renderer rejects unknown events and confirmation stays default-deny" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=plain \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui sparkle "nope"'
  [ "$status" -eq 64 ]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=plain \
    USERLAND_ASSUME_YES=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui confirm "Apply this plan?"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"[info] Apply this plan? yes"* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE= \
    LC_ALL=C \
    NO_COLOR=1 \
    sh -c 'unset USERLAND_UNICODE; . "$USERLAND_ROOT/lib/common.sh"; userland_log healthy "Portable output"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"ok  Portable output"* ]]
  [[ "$output" != *"✓"* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=0 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command plan "Portable title"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"+-- USERLAND --+"* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=plain \
    USERLAND_ASSUME_YES= \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui confirm "Apply this plan?"'
  [ "$status" -eq 1 ]
  [[ "$output" == *"[error] Apply this plan? requires an interactive terminal"* ]]
}

@test "rich tasks keep successful native logs out of scrollback and render the typed plan once" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; . "$USERLAND_ROOT/lib/plan-ledger.sh"; userland_ui command plan "Preview"; userland_plan_begin; userland_ui task inspect "Inspect packages" /usr/bin/printf "native-one\nnative-two\n"; userland_plan_add apps upgrade automatic declared "2 package upgrades" "available" "test:packages"; userland_plan_render'

  [ "$status" -eq 0 ]
  [[ "$output" != *"native-one"* ]]
  [[ "$output" == *"┌"*"◇"*"◆"* ]]
  [[ "$output" == *"Plan"* ]]
  [[ "$output" == *"Application additions"* ]]
  [[ "$output" == *"2 package upgrades"* ]]
  grep -q "native-one" "$USERLAND_STATE_DIR/last-run.log"
}

@test "rich plans use one connected tree and show every option" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; . "$USERLAND_ROOT/lib/plan-ledger.sh"; userland_ui command plan "Preview"; userland_plan_begin; for item in 1 2 3 4 5 6; do userland_plan_add apps install automatic declared "App $item" "Homebrew" "test:app:$item"; done; for item in 1 2 3 4 5 6; do userland_plan_add cleanup release automatic userland "~/.stale-$item" "legacy link" "legacy-link:test:$item"; done; userland_plan_render'

  [ "$status" -eq 0 ]
  [[ "$output" != *"more automatic changes"* ]]
  [[ "$output" == *"App 1"* ]]
  [[ "$output" == *"App 6"* ]]
  [[ "$output" == *"~/.stale-1"* ]]
  [[ "$output" == *"~/.stale-6"* ]]
  [[ "$output" == *$'\n ◆  Plan\n  │\n  ├─ OS changes'* ]]
  [[ "$output" == *$'\n  ├─ OS changes'* ]]
  [[ "$output" == *$'\n  │\n  ├─ Filesystem changes'* ]]
  [[ "$output" == *$'\n  │\n  ├─ Application additions'* ]]
  [[ "$output" == *$'\n  │\n  ├─ Cleanup'* ]]
  [[ "$output" == *$'\n  │  -  ~/.stale-6'* ]]
  [[ "$output" != *"└"* ]]
  [[ "$output" != *"│  ├"* ]]
  app_detail_column=$(printf '%s\n' "$output" | awk '/App 1/ { print index($0, "Homebrew"); exit }')
  cleanup_detail_column=$(printf '%s\n' "$output" | awk '/\.stale-1/ { print index($0, "legacy link"); exit }')
  [ "$app_detail_column" -eq "$cleanup_detail_column" ]
  grep -q "App 6" "$USERLAND_STATE_DIR/last-run.log"
  grep -q "~/.stale-6" "$USERLAND_STATE_DIR/last-run.log"
}

@test "the typed plan keeps OS, filesystem, apps, and cleanup in fixed order" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_MODE=plain \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; . "$USERLAND_ROOT/lib/plan-ledger.sh"; userland_plan_begin; userland_plan_add cleanup remove automatic declared "old-service" "declared absent" "mise:service:old-service"; userland_plan_add apps install attended external "DaVinci Resolve" "manual install" ""; userland_plan_add fs link automatic declared "~/.zshrc" "managed link" "dotfile:zshrc"; userland_plan_add os set automatic declared "Finder · NewWindowTarget" "Recents to Home" "macos-default:finder"; userland_plan_render'

  [ "$status" -eq 0 ]
  os_line=$(printf '%s\n' "$output" | grep -n '== OS changes' | cut -d: -f1)
  fs_line=$(printf '%s\n' "$output" | grep -n '== Filesystem changes' | cut -d: -f1)
  apps_line=$(printf '%s\n' "$output" | grep -n '== Application additions' | cut -d: -f1)
  cleanup_line=$(printf '%s\n' "$output" | grep -n '== Cleanup' | cut -d: -f1)
  [ "$os_line" -lt "$fs_line" ]
  [ "$fs_line" -lt "$apps_line" ]
  [ "$apps_line" -lt "$cleanup_line" ]
  [[ "$output" == *"Finder · NewWindowTarget"* ]]
  [[ "$output" == *"DaVinci Resolve"* ]]
  [[ "$output" == *"old-service"* ]]
}

@test "rich task failures show a bounded excerpt and preserve the command status" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    FAILURE_TASK="$TEST_TMPDIR/bin/failing-task" \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui task apply "Install packages" "$FAILURE_TASK"'

  [ "$status" -eq 7 ]
  [[ "$output" == *"Install packages failed"* ]]
  [[ "$output" == *"last failure"* ]]
  [[ "$output" == *"Log:"*"last-run.log"* ]]
}

@test "plain tasks stream every native line without terminal controls" {
  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_MODE=plain \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui task inspect "Inspect packages" /usr/bin/printf "native-one\nnative-two\n"'

  [ "$status" -eq 0 ]
  [[ "$output" == *"native-one"* ]]
  [[ "$output" == *"native-two"* ]]
  [[ "$output" != *$'\e['* ]]
}

@test "the installed command resolves its managed symlink" {
  mkdir -p "$USERLAND_HOME/.local/bin"
  ln -s "$TEST_ROOT/bin/userland" "$USERLAND_HOME/.local/bin/userland"
  run "$USERLAND_HOME/.local/bin/userland" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"userland <command>"* ]]
}

@test "an upgraded checkout reuses mise from an installed release" {
  mkdir -p "$USERLAND_DATA_DIR/releases/v0.1.3/bin"
  cat >"$USERLAND_DATA_DIR/releases/v0.1.3/bin/mise" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$PINNED_MISE_CALLS"
exit 0
EOF
  chmod +x "$USERLAND_DATA_DIR/releases/v0.1.3/bin/mise"
  export PINNED_MISE_CALLS=$TEST_TMPDIR/pinned-mise-calls
  export USERLAND_MISE=$TEST_TMPDIR/bin/mise

  run "$TEST_ROOT/bin/userland" doctor --json
  [ "$status" -eq 1 ]
  [ -s "$PINNED_MISE_CALLS" ]
  [ ! -s "$MISE_CALLS" ]
}

@test "plan refreshes repository discovery and remains read-only" {
  export USERLAND_UNAME=Darwin
  run "$TEST_ROOT/bin/userland" plan
  [ "$status" -eq 0 ]
  [ -f "$USERLAND_CACHE_DIR/repositories.tsv" ]
  grep -F "$USERLAND_REPO_ROOTS/example" "$USERLAND_CACHE_DIR/repositories.tsv"
  [[ "$output" == *"encrypted configuration import"* ]]
  ! grep -q -- '--force-dotfiles' "$MISE_CALLS"
  grep -q 'bootstrap plan --json' "$MISE_CALLS"
  ! grep -q 'bootstrap packages upgrade --dry-run' "$MISE_CALLS"
  grep -q 'bootstrap dotfiles status --json' "$MISE_CALLS"
}

@test "plan refuses unreadable machine-state JSON before consent" {
  printf '%s\n' '#!/bin/sh' \
    'case "$*" in' \
    '  *bootstrap*plan*--json*) printf "%s\\n" "not-json" ;;' \
    '  *bootstrap*dotfiles*status*--json*) printf "%s\\n" "{\"files\":[],\"edits\":[]}" ;;' \
    'esac' \
    'exit 0' >"$TEST_TMPDIR/bin/mise"
  chmod +x "$TEST_TMPDIR/bin/mise"

  run "$TEST_ROOT/bin/userland" plan
  [ "$status" -ne 0 ]
  [[ "$output" == *"unreadable plan"* ]]
  [[ "$output" == *"no approval was requested"* ]]
}

@test "plan classifies exact mise and macOS resources by effect" {
  fake_nix_bin="$TEST_TMPDIR/etc/profiles/per-user/giacomo/bin"
  mkdir -p "$fake_nix_bin"
  cat >"$fake_nix_bin/eza" <<'EOF'
#!/bin/sh
exit 0
EOF
  cat >"$fake_nix_bin/git" <<'EOF'
#!/bin/sh
exec /usr/bin/git "$@"
EOF
  chmod +x "$fake_nix_bin/eza" "$fake_nix_bin/git"
  export PATH="$fake_nix_bin:$PATH"

  cat >"$TEST_TMPDIR/bin/mise" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$MISE_CALLS"
case "$*" in
  *bootstrap*plan*--json*)
    printf '%s\n' '{"resources":[{"id":{"kind":"package","name":"brew:eza"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"package","name":"brew:git"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"package","name":"brew:unknown-tool"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"file","name":"~/.config/example"},"current":"old","desired":"managed","action":"update"},{"id":{"kind":"service","name":"retired-agent"},"current":"running","desired":"absent","action":"remove"}],"summary":{"create":3,"update":1,"remove":1,"unchanged":0,"unknown":0}}'
    ;;
  *bootstrap*dotfiles*status*--json*)
    printf '%s\n' '{"files":[{"state":"differs","source":"~/userland/config/home/zshrc","target":"~/.zshrc","mode":"symlink"}],"edits":[]}'
    ;;
  *bootstrap*macos*defaults*status*--json*)
    printf '%s\n' '{"macos_defaults":{"entries":[{"value":true,"key":"AppleShowAllFiles","current":"false","domain":"com.apple.finder","state":"differs"}],"available":true}}'
    ;;
esac
exit 0
EOF
  chmod +x "$TEST_TMPDIR/bin/mise"
  export USERLAND_UNAME=Darwin

  run env USERLAND_UI_MODE=plain "$TEST_ROOT/bin/userland" plan

  [ "$status" -eq 0 ]
  [[ "$output" == *"Finder · AppleShowAllFiles: false to true"* ]]
  [[ "$output" == *"~/.config/example: old to managed"* ]]
  [[ "$output" == *"eza: migrate from Nix to Homebrew"* ]]
  [[ "$output" == *"git: migrate from Nix to Homebrew"* ]]
  [[ "$output" == *"unknown-tool: migrate to Homebrew"* ]]
  [[ "$output" == *"retired-agent: running to absent"* ]]
}

@test "public commands keep Homebrew inspection read-only and sync never cleans" {
  export USERLAND_UNAME=Darwin
  export USERLAND_BREW=$TEST_TMPDIR/bin/brew
  export BREW_CALLS=$TEST_TMPDIR/brew-calls
  export ANDROID_HOME=$TEST_TMPDIR/android-sdk

  cat >"$USERLAND_BREW" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$BREW_CALLS"
case "$*" in
  --version) printf '%s\n' 'Homebrew 5.0.0' ;;
esac
exit 0
EOF
  cat >"$TEST_TMPDIR/bin/duti" <<'EOF'
#!/bin/sh
exit 0
EOF
  cat >"$TEST_TMPDIR/bin/open" <<'EOF'
#!/bin/sh
printf 'unexpected open: %s\n' "$*" >&2
exit 99
EOF
  chmod +x "$USERLAND_BREW" "$TEST_TMPDIR/bin/duti" "$TEST_TMPDIR/bin/open"

  mkdir -p \
    "$ANDROID_HOME/platform-tools" \
    "$ANDROID_HOME/emulator" \
    "$ANDROID_HOME/platforms/android-36" \
    "$ANDROID_HOME/build-tools/36.0.0"
  : >"$ANDROID_HOME/platform-tools/adb"
  : >"$ANDROID_HOME/emulator/emulator"
  chmod +x "$ANDROID_HOME/platform-tools/adb" "$ANDROID_HOME/emulator/emulator"

  while IFS="$(printf '\t')" read -r browser extension_id _name; do
    case "$browser" in
      '' | '#'*) continue ;;
      helium) browser_root="$USERLAND_HOME/Library/Application Support/net.imput.helium" ;;
      chrome) browser_root="$USERLAND_HOME/Library/Application Support/Google/Chrome" ;;
    esac
    mkdir -p "$browser_root/Extensions/$extension_id"
  done <"$TEST_ROOT/config/browser-extensions.tsv"

  run "$TEST_ROOT/bin/userland" plan
  [ "$status" -eq 0 ]
  grep -q 'bundle check.*--no-upgrade' "$BREW_CALLS"
  ! grep -q '^update$' "$BREW_CALLS"

  : >"$BREW_CALLS"
  run "$TEST_ROOT/bin/userland" doctor
  grep -q 'bundle check.*--no-upgrade' "$BREW_CALLS"
  ! grep -q '^update$' "$BREW_CALLS"

  : >"$BREW_CALLS"
  run "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 2 ]
  grep -q '^update$' "$BREW_CALLS"
  grep -Fq "bundle --file $TEST_ROOT/config/brewfile" "$BREW_CALLS"
  ! grep -q 'cleanup' "$BREW_CALLS"
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
  export USERLAND_BOOTSTRAP_CONTROL=$TEST_TMPDIR/bootstrap-control
  export USERLAND_BOOTSTRAP_TOKEN=bootstrap-test-token
  export TEST_CHECK_BOOTSTRAP_CHECKPOINT=1
  mkdir -m 700 "$USERLAND_BOOTSTRAP_CONTROL"
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$USERLAND_BOOTSTRAP_CONTROL/owner"
  mkdir -p "$USERLAND_DATA_DIR/bootstrap.lock"
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$USERLAND_DATA_DIR/bootstrap.lock/owner"

  run "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 0 ]
  [ "$(cat "$USERLAND_BOOTSTRAP_CONTROL/apply-started")" = "$USERLAND_BOOTSTRAP_TOKEN" ]
  [ -z "$(find "$USERLAND_BOOTSTRAP_CONTROL" -name '.apply-started.*' -print)" ]
  [ -s "$USERLAND_CACHE_DIR/zsh/init.zsh" ]
  grep -q 'bootstrap packages apply --yes' "$MISE_CALLS"
  grep -q 'bootstrap packages upgrade --yes' "$MISE_CALLS"
}
