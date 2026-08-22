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
  export TEST_USERLAND_RELEASE_TAG=v9.9.9
  export TEST_USERLAND_RELEASE_COMMIT
  TEST_USERLAND_RELEASE_COMMIT=$(git -C "$TEST_ROOT" rev-parse HEAD)
  export USERLAND_CURL=$TEST_TMPDIR/bin/curl
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
  *bootstrap*status*--missing*)
    if [ "${TEST_MACHINE_STATE_DRIFT:-0}" = 1 ]; then
      printf '%s\n' \
        'dotfiles  ~/.agents/skills  symlink-each  differs (~/.agents/skills/ask-matt: exists but is not a symlink)' \
        'tools     npm:@openai/codex@0.148.0  installed' \
        'tools     npm:eas-cli@22.0.0  installed' \
        'tools     npm:tree-sitter-cli@0.26.12  installed' \
        'tools     pitchfork@2.22.0  installed' \
        'tools     uv@0.12.5  installed' \
        'tools     python@3.14.7  installed'
      exit 1
    fi
    ;;
  *doctor*--json*) printf '%s\n' '{"healthy":true}' ;;
  *bootstrap*plan*--json*) printf '%s\n' '{"resources":[],"summary":{"create":0,"update":0,"remove":0,"unchanged":0,"unknown":0}}' ;;
  *bootstrap*dotfiles*status*--json*)
    if [ "${TEST_DOTFILES_PENDING:-0}" = 1 ] && [ ! -e "$TEST_TMPDIR/dotfiles-applied" ]; then
      printf '%s\n' '{"files":[{"state":"differs","source":"~/.userland/config/home/zshrc","target":"~/.zshrc","mode":"symlink"}],"edits":[]}'
    else
      printf '%s\n' '{"files":[],"edits":[]}'
    fi
    ;;
  *bootstrap*dotfiles*apply*) : >"$TEST_TMPDIR/dotfiles-applied" ;;
  *bootstrap*macos*defaults*status*--json*) printf '%s\n' '{"macos_defaults":{"entries":[],"available":true}}' ;;
esac
exit 0
EOF
  chmod +x "$TEST_TMPDIR/bin/mise"
  cat >"$TEST_TMPDIR/bin/curl" <<'EOF'
#!/bin/sh
[ "${TEST_USERLAND_RELEASE_UNAVAILABLE:-0}" = 0 ] || exit 22
printf '%s\n' \
  '#!/bin/sh' \
  'set -eu' \
  '' \
  "tag='$TEST_USERLAND_RELEASE_TAG'" \
  "commit='$TEST_USERLAND_RELEASE_COMMIT'"
EOF
  chmod +x "$TEST_TMPDIR/bin/curl"
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

@test "the public interface exposes plan, sync, doctor, and completions" {
  run "$TEST_ROOT/bin/userland" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"plan"* ]]
  [[ "$output" == *"sync"* ]]
  [[ "$output" == *"doctor"* ]]
  [[ "$output" == *"completions"* ]]
  [[ "$output" != *"resume"* ]]

  run "$TEST_ROOT/bin/userland" update
  [ "$status" -eq 64 ]
}

@test "completion scripts cover Bash, Zsh, Fish, and Nushell" {
  for shell in bash zsh fish nushell; do
    run "$TEST_ROOT/bin/userland" completions "$shell"
    [ "$status" -eq 0 ]
    [[ "$output" == *"userland"* ]]
    [[ "$output" == *"completions"* ]]
  done

  run "$TEST_ROOT/bin/userland" completions powershell
  [ "$status" -eq 64 ]
  [[ "$output" == *"supports bash, fish, nushell, or zsh"* ]]
}

@test "Zsh adds ul without replacing an existing alias or function" {
  mkdir -p "$TEST_TMPDIR/zsh-home"
  run env HOME="$TEST_TMPDIR/zsh-home" XDG_CACHE_HOME="$TEST_TMPDIR/zsh-cache" /bin/zsh -fic "source '$TEST_ROOT/config/home/zshrc'; alias ul"
  [ "$status" -eq 0 ]
  [ "$output" = 'ul=userland' ]

  run env HOME="$TEST_TMPDIR/zsh-home" XDG_CACHE_HOME="$TEST_TMPDIR/zsh-cache" /bin/zsh -fic "alias ul='printf existing'; source '$TEST_ROOT/config/home/zshrc'; alias ul"
  [ "$status" -eq 0 ]
  [ "$output" = "ul='printf existing'" ]

  run env HOME="$TEST_TMPDIR/zsh-home" XDG_CACHE_HOME="$TEST_TMPDIR/zsh-cache" /bin/zsh -fic "ul() { printf existing; }; source '$TEST_ROOT/config/home/zshrc'; whence -w ul"
  [ "$status" -eq 0 ]
  [ "$output" = 'ul: function' ]
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
    USERLAND_VERSION=v1.2.3 \
    TERM=xterm-256color \
    NO_COLOR= \
    CLICOLOR_FORCE=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command doctor "Check this Mac"; userland_ui section "Security"; userland_log healthy "FileVault is on"'

  [ "$status" -eq 0 ]
  [[ "$output" == *"doctor"*"Check this Mac"* ]]
  [[ "$output" == *"▗▖ ▗▖ ▗▄▄▖"*"▝▚▄▞▘▗▄▄▞▘"* ]]
  [[ "$output" == *$'▐▙▄▄▀\e[0m  \e[2mv1.2.3\e[0m\n ┌────────────────────────────────────────────────\n │'* ]]
  [[ "$output" == *$'\n '*"┌"* ]]
  [[ "$output" == *$'\e[0m\n ┌'* ]]
  [[ "$output" != *$'\e[37m┌'* ]]
  [[ "$output" != *$'\e[36m┌'* ]]
  [[ "$output" == *$'\n │'* ]]
  [[ "$output" == *$'\n '*"◆"*"Security"* ]]
  [[ "$output" == *"✓"*"FileVault is on"* ]]
  [[ "$output" == *$'\e['* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    TERM=xterm-256color \
    NO_COLOR= \
    CLICOLOR_FORCE=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command sync "Preview"; userland_ui summary cancelled "Cancelled. No changes were applied."'

  [ "$status" -eq 0 ]
  [[ "$output" == *$'\n │\n └\e[0m  Cancelled. No changes were applied.'* ]]
  [[ "$output" != *$'\e[2m└'* ]]

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

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui_spinner_tick=0; userland_ui_spinner_frame; printf "%s\n" "$userland_ui_spinner_glyph"'
  [ "$status" -eq 0 ]
  [ "$output" = '⠋' ]
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
    USERLAND_UNICODE=1 \
    USERLAND_ASSUME_YES= \
    USERLAND_UI_TEST_CONFIRMATION= \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui confirm "Apply this plan?"'
  [ "$status" -eq 3 ]
  [[ "$output" == *$'?  Apply this plan? [y/N] › N'* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    USERLAND_ASSUME_YES= \
    USERLAND_UI_TEST_CONFIRMATION=yes \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui confirm "Apply this plan?"'
  [ "$status" -eq 0 ]
  [[ "$output" == *$'?  Apply this plan? [y/N] › Y'* ]]
  [ "$(grep -Fc 'stty -echo' "$TEST_ROOT/lib/ui.sh")" -eq 0 ]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    USERLAND_TESTING=1 \
    USERLAND_UI_TEST_ACKNOWLEDGEMENT=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command sync "Preview"; userland_ui acknowledge "Press Enter after Raycast reports a successful import."'
  [ "$status" -eq 0 ]
  [[ "$output" == *$'?  Press Enter after Raycast reports a successful import. › \n │'* ]]

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
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui command sync "Preview"; userland_ui_signal 130'
  [ "$status" -eq 130 ]
  [[ "$output" == *$'\n │  sync\n │  Preview\n'* ]]
  [[ "$output" == *"Cancelled."* ]]

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
  [[ "$output" == *$'◇  Inspect packages\n │\n ◆  Plan'* ]]
  grep -q "native-one" "$USERLAND_STATE_DIR/last-run.log"
}

@test "package spinners expose elapsed time, direct progress, target, and phase" {
  plan_file=$TEST_TMPDIR/package-plan
  task_log=$TEST_TMPDIR/package-task
  mkdir -p "$USERLAND_CACHE_DIR"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    apps install automatic declared bat Homebrew mise:package:brew:bat \
    apps install automatic declared ffmpeg Homebrew mise:package:brew:ffmpeg \
    apps install automatic declared ghostty Homebrew brewfile:Cask:ghostty >"$plan_file"
  printf '%s\n' \
    'mise brew:dependency  ✓ 1.0.0' \
    'mise brew:bat         ✓ 0.26.0' \
    'mise brew:ffmpeg      download ffmpeg.tar.gz' >"$task_log"

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_PLAN_FILE="$plan_file" \
    USERLAND_UI_PROGRESS=mise-install \
    TASK_LOG="$task_log" \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui_task_log=$TASK_LOG; userland_ui_progress_prepare; userland_ui_spinner_started_at=$(($(date +%s) - 65)); userland_ui_progress_refresh; printf "%s\n" "$userland_ui_spinner_detail"'

  [ "$status" -eq 0 ]
  [ "$output" = '1m 5s · 1/2 · ffmpeg · download' ]
}

@test "Homebrew application spinners expose direct progress and the current package" {
  plan_file=$TEST_TMPDIR/homebrew-plan
  task_log=$TEST_TMPDIR/homebrew-task
  mkdir -p "$USERLAND_CACHE_DIR"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    apps install automatic declared nikitabobko/tap Homebrew brewfile:Tap:nikitabobko/tap \
    apps install automatic declared ghostty Homebrew brewfile:Cask:ghostty \
    apps install automatic declared zed Homebrew brewfile:Cask:zed >"$plan_file"
  printf '%s\n' \
    'Fetching ghostty, zed' \
    'Tapping nikitabobko/tap' \
    'Installing ghostty' >"$task_log"

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_PLAN_FILE="$plan_file" \
    USERLAND_UI_PROGRESS=homebrew-install \
    TASK_LOG="$task_log" \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui_task_log=$TASK_LOG; userland_ui_progress_prepare; userland_ui_spinner_started_at=$(($(date +%s) - 23)); userland_ui_progress_refresh; printf "%s\n" "$userland_ui_spinner_detail"'

  [ "$status" -eq 0 ]
  [ "$output" = '23s · 2/3 · ghostty' ]
}

@test "cleanup preserves output from an interrupted task in the private run log" {
  task_log=$TEST_TMPDIR/interrupted-task
  mkdir -p "$USERLAND_STATE_DIR"
  : >"$USERLAND_STATE_DIR/last-run.log"
  printf '%s\n' 'mise brew:ffmpeg download ffmpeg.tar.gz' >"$task_log"

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_HOME="$USERLAND_HOME" \
    USERLAND_CACHE_DIR="$USERLAND_CACHE_DIR" \
    USERLAND_STATE_DIR="$USERLAND_STATE_DIR" \
    USERLAND_UI_RUN_LOG="$USERLAND_STATE_DIR/last-run.log" \
    TASK_LOG="$task_log" \
    sh -c '. "$USERLAND_ROOT/lib/common.sh"; userland_ui_task_label="Install missing rolling packages"; userland_ui_task_log=$TASK_LOG; userland_ui_cleanup; cat "$USERLAND_UI_RUN_LOG"'

  [ "$status" -eq 0 ]
  [[ "$output" == *'## Install missing rolling packages (interrupted)'* ]]
  [[ "$output" == *'mise brew:ffmpeg download ffmpeg.tar.gz'* ]]
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
  [[ "$output" == *$'\n ◆  Plan\n │\n ├─ OS changes'* ]]
  [[ "$output" == *$'\n ├─ OS changes'* ]]
  [[ "$output" == *$'\n │\n ├─ Filesystem changes'* ]]
  [[ "$output" == *$'\n │\n ├─ Application additions'* ]]
  [[ "$output" == *$'\n │\n ├─ Cleanup'* ]]
  [[ "$output" == *$'\n │  -  ~/.stale-6'* ]]
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
  [[ "$output" == *$'\n │  last failure'* ]]
  [[ "$output" != *$'\n    | last failure'* ]]
  [[ "$output" == *"Log:"*"last-run.log"* ]]
}

@test "doctor surfaces actionable machine drift instead of healthy inventory" {
  run env \
    TEST_MACHINE_STATE_DRIFT=1 \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    "$TEST_ROOT/bin/userland" doctor

  [ "$status" -eq 1 ]
  [[ "$output" == *"Machine state"* ]]
  [[ "$output" == *"dotfiles"*"differs"* ]]
  [[ "$output" != *"python@3.14.7"* ]]
  grep -q 'python@3.14.7' "$USERLAND_STATE_DIR/last-run.log"
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

@test "the shell cache refreshes when its recipe is stale" {
  mkdir -p "$USERLAND_CACHE_DIR/zsh"
  printf '%s\n' '# stale cache' >"$USERLAND_CACHE_DIR/zsh/init.zsh"

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_MISE="$TEST_TMPDIR/bin/mise" \
    USERLAND_UI_MODE=plain \
    sh "$TEST_ROOT/lib/adapters/shell-cache.sh" plan
  [ "$status" -eq 0 ]
  [[ "$output" == *"will be generated"* ]]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_MISE="$TEST_TMPDIR/bin/mise" \
    USERLAND_UI_MODE=plain \
    sh "$TEST_ROOT/lib/adapters/shell-cache.sh" apply
  [ "$status" -eq 0 ]

  run env \
    USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_MISE="$TEST_TMPDIR/bin/mise" \
    USERLAND_UI_MODE=plain \
    sh "$TEST_ROOT/lib/adapters/shell-cache.sh" doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"is current"* ]]
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
  run env USERLAND_UI_MODE=rich USERLAND_UNICODE=1 NO_COLOR=1 "$TEST_ROOT/bin/userland" plan
  [ "$status" -eq 0 ]
  [ -f "$USERLAND_CACHE_DIR/repositories.tsv" ]
  grep -F "$USERLAND_REPO_ROOTS/example" "$USERLAND_CACHE_DIR/repositories.tsv"
  [[ "$output" == *$'│  Preview declared state without applying it.\n │\n'* ]]
  [[ "$output" == *"encrypted configuration import"* ]]
  ! grep -q -- '--force-dotfiles' "$MISE_CALLS"
  grep -q 'bootstrap plan --json' "$MISE_CALLS"
  ! grep -q 'bootstrap packages upgrade --dry-run' "$MISE_CALLS"
  grep -q 'bootstrap dotfiles status --json' "$MISE_CALLS"
}

@test "a blocked refreshed sync leaves legacy links untouched" {
  legacy_source=$TEST_TMPDIR/workspace/cfg/home/zshrc
  mkdir -p "${legacy_source%/*}"
  printf '%s\n' '# legacy zshrc' >"$legacy_source"
  ln -s "$legacy_source" "$USERLAND_HOME/.zshrc"

  cat >"$TEST_TMPDIR/bin/mise" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$MISE_CALLS"
case "$*" in
  *bootstrap*plan*--json*)
    printf '%s\n' '{"resources":[{"id":{"kind":"package","name":"brew:bat"},"current":"installed","desired":"installed","action":"future-action"}],"summary":{"create":0,"update":0,"remove":0,"unchanged":0,"unknown":1}}'
    ;;
  *bootstrap*dotfiles*status*--json*) printf '%s\n' '{"files":[],"edits":[]}' ;;
  *bootstrap*macos*defaults*status*--json*) printf '%s\n' '{"macos_defaults":{"entries":[],"available":false}}' ;;
esac
exit 0
EOF
  chmod +x "$TEST_TMPDIR/bin/mise"

  run env USERLAND_REFRESHED=1 USERLAND_UI_MODE=plain "$TEST_ROOT/bin/userland" sync

  [ "$status" -eq 2 ]
  [ -L "$USERLAND_HOME/.zshrc" ]
  [ "$(readlink "$USERLAND_HOME/.zshrc")" = "$legacy_source" ]
  [[ "$output" != *"released legacy workspace ownership"* ]]
  ! grep -q 'bootstrap packages apply' "$MISE_CALLS"
  ! grep -q 'bootstrap dotfiles apply' "$MISE_CALLS"
}

@test "sync refreshes itself before rendering and hides successful Git output" {
  refresh_root=$TEST_TMPDIR/self-update-root
  mkdir -p "$refresh_root/bin" "$refresh_root/.git"
  cp "$TEST_ROOT/bin/userland" "$refresh_root/bin/userland"
  ln -s "$TEST_ROOT/lib" "$refresh_root/lib"
  ln -s "$TEST_ROOT/config" "$refresh_root/config"
  ln -s "$TEST_ROOT/mise.toml" "$refresh_root/mise.toml"
  mkdir -p "$USERLAND_HOME/.local/bin"
  export TEST_GIT_CALLS=$TEST_TMPDIR/self-update-git-calls
  cat >"$USERLAND_HOME/.local/bin/git" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$TEST_GIT_CALLS"
case "$*" in
  *' status --porcelain') ;;
  *' branch --show-current') printf '%s\n' main ;;
  *' fetch --quiet --tags origin main') printf '%s\n' 'git transfer progress' >&2 ;;
  *' rev-parse HEAD') printf '%s\n' 1111111111111111111111111111111111111111 ;;
  *' rev-parse origin/main') printf '%s\n' 2222222222222222222222222222222222222222 ;;
  *' merge-base --is-ancestor HEAD origin/main') ;;
  *' merge --ff-only --quiet origin/main') ;;
  *' submodule sync --quiet --recursive') ;;
  *' submodule update --quiet --init --recursive') ;;
esac
exit 0
EOF
  chmod +x "$USERLAND_HOME/.local/bin/git"

  run env \
    USERLAND_ARCHIVE= \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    USERLAND_ASSUME_YES= \
    USERLAND_UI_TEST_CONFIRMATION= \
    NO_COLOR=1 \
    "$refresh_root/bin/userland" sync

  [ "$status" -eq 3 ]
  grep -Fq 'fetch --quiet --tags origin main' "$TEST_GIT_CALLS"
  [ "$(printf '%s\n' "$output" | grep -Fc '▗▖ ▗▖ ▗▄▄▖')" -eq 1 ]
  [[ "$output" != *"git transfer progress"* ]]
  [[ "$output" != *"advanced userland to origin/main"* ]]
}

@test "sync exits without approval or apply work when the plan is empty" {
  USERLAND_ROOT="$TEST_ROOT" \
    USERLAND_MISE="$TEST_TMPDIR/bin/mise" \
    USERLAND_UI_MODE=plain \
    sh "$TEST_ROOT/lib/adapters/shell-cache.sh" apply >/dev/null
  : >"$MISE_CALLS"

  run env \
    USERLAND_REFRESHED=1 \
    USERLAND_UI_MODE=plain \
    USERLAND_ASSUME_YES= \
    USERLAND_UI_TEST_CONFIRMATION= \
    "$TEST_ROOT/bin/userland" sync

  [ "$status" -eq 0 ]
  [[ "$output" == *"0 automatic; 0 attended; 0 cleanup; 0 blocked"* ]]
  [[ "$output" == *'Done. This Mac matches userland. Run `userland doctor` to check the machine state'* ]]
  [[ "$output" != *"Apply this plan?"* ]]
  [[ "$output" != *"Apply packages"* ]]
  ! grep -q 'bootstrap packages apply' "$MISE_CALLS"
  ! grep -q 'bootstrap packages upgrade' "$MISE_CALLS"
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
    printf '%s\n' '{"resources":[{"id":{"kind":"package","name":"brew:bat"},"current":"installed","desired":"installed","action":"noop"},{"id":{"kind":"package","name":"brew:eza"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"package","name":"brew:git"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"package","name":"brew:unknown-tool"},"current":"missing","desired":"installed","action":"create"},{"id":{"kind":"file","name":"~/.config/example"},"current":"old","desired":"managed","action":"update"},{"id":{"kind":"service","name":"retired-agent"},"current":"running","desired":"absent","action":"remove"}],"summary":{"create":3,"update":1,"remove":1,"unchanged":0,"noop":1,"unknown":0}}'
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
  [[ "$output" != *"brew:bat"* ]]
  [[ "$output" != *"unknown action: noop"* ]]
}

@test "Homebrew plans every missing application and sync avoids unplanned upgrades" {
  export USERLAND_UNAME=Darwin
  export USERLAND_BREW=$TEST_TMPDIR/bin/brew
  export BREW_CALLS=$TEST_TMPDIR/brew-calls
  export ANDROID_HOME=$TEST_TMPDIR/android-sdk

  cat >"$USERLAND_BREW" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$BREW_CALLS"
case "$*" in
  --version)
    [ "${BREW_PRESENT:-1}" = 1 ] || exit 1
    printf '%s\n' 'Homebrew 5.0.0'
    ;;
  *'bundle check'*--verbose*)
    printf '%s\n' \
      '→ Tap nikitabobko/tap needs to be tapped.' \
      '→ Cask 1password needs to be installed.' \
      '→ App Xcode needs to be installed.'
    exit 1
    ;;
  *'bundle --file'*) [ "${BREW_APPLY_FAIL:-0}" = 0 ] || exit 9 ;;
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
  [[ "$output" == *"nikitabobko/tap"*"add Homebrew tap"* ]]
  [[ "$output" == *"1password"*"install or adopt Homebrew cask"* ]]
  [[ "$output" == *"Xcode"*"install from Mac App Store"* ]]
  [[ "$output" != *"Homebrew will install or adopt missing personal applications"* ]]

  run env BREW_PRESENT=0 "$TEST_ROOT/bin/userland" plan
  [ "$status" -eq 0 ]
  [[ "$output" == *"Homebrew"*"install from the pinned Homebrew installer"* ]]
  [[ "$output" == *"helium-browser"*"install or adopt Homebrew cask"* ]]
  [[ "$output" == *"bazecor"*"install or replace with the current Homebrew cask"* ]]
  [[ "$output" == *"yubico-authenticator"*"install or replace with the current Homebrew cask"* ]]
  [[ "$output" == *"Xcode"*"install from Mac App Store"* ]]

  : >"$BREW_CALLS"
  run "$TEST_ROOT/bin/userland" doctor
  grep -q 'bundle check.*--no-upgrade' "$BREW_CALLS"
  ! grep -q '^update$' "$BREW_CALLS"

  : >"$BREW_CALLS"
  run "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 2 ]
  ! grep -q '^update$' "$BREW_CALLS"
  grep -Fq "bundle --file $TEST_ROOT/config/brewfile --no-upgrade" "$BREW_CALLS"
  ! grep -q 'cleanup' "$BREW_CALLS"
  grep -Fq 'tap "nikitabobko/tap", trusted: true' "$TEST_ROOT/config/brewfile"
  grep -Fq 'cask "bazecor", args: { force: true }' "$TEST_ROOT/config/brewfile"
  grep -Fq 'cask "yubico-authenticator", args: { force: true }' "$TEST_ROOT/config/brewfile"

  : >"$BREW_CALLS"
  run env BREW_APPLY_FAIL=1 USERLAND_UI_MODE=plain "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 9 ]
  [[ "$output" == *"Homebrew applications failed (exit 9)"* ]]
  [[ "$output" == *"Stopped at the failed step. Fix it, then rerun sync."* ]]
  [[ "$output" != *"Raycast configuration import"* ]]
}

@test "doctor json has a stable schema and no home paths" {
  mkdir -p "$USERLAND_STATE_DIR/receipts"
  run "$TEST_ROOT/bin/userland" doctor --json
  [ "$status" -eq 1 ]
  [[ "$output" == '{"schema_version":1,'* ]]
  [[ "$output" != *"$USERLAND_HOME"* ]]
  printf '%s' "$output" | grep -q '"name":"adapters","status":"attention"'
  printf '%s' "$output" | grep -q '"name":"userland","status":"current","version":"v9.9.9"'
}

@test "doctor reports current, outdated, and unavailable Userland releases" {
  run env \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    "$TEST_ROOT/bin/userland" doctor
  [[ "$output" == *"◆  Userland"* ]]
  [[ "$output" == *"✓  v9.9.9 is current"* ]]

  local ancestor_commit
  ancestor_commit=$(git -C "$TEST_ROOT" rev-parse HEAD^)
  run env \
    TEST_USERLAND_RELEASE_COMMIT="$ancestor_commit" \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    "$TEST_ROOT/bin/userland" doctor
  [[ "$output" == *"includes changes after v9.9.9"* ]]
  [[ "$output" != *"Userland is outdated"* ]]

  run env TEST_USERLAND_RELEASE_COMMIT="$ancestor_commit" "$TEST_ROOT/bin/userland" doctor --json
  printf '%s' "$output" | grep -q '"name":"userland","status":"ahead","version":"v9.9.9"'

  run env \
    TEST_USERLAND_RELEASE_COMMIT=0000000000000000000000000000000000000000 \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    "$TEST_ROOT/bin/userland" doctor
  [ "$status" -eq 1 ]
  [[ "$output" == *"!  Userland is outdated; run userland sync"* ]]

  run env TEST_USERLAND_RELEASE_UNAVAILABLE=1 "$TEST_ROOT/bin/userland" doctor --json
  printf '%s' "$output" | grep -q '"name":"userland","status":"unknown","version":null'
}

@test "sync uses the pinned interface and creates the shell cache" {
  export USERLAND_BOOTSTRAP_CONTROL=$TEST_TMPDIR/bootstrap-control
  export USERLAND_BOOTSTRAP_TOKEN=bootstrap-test-token
  export TEST_CHECK_BOOTSTRAP_CHECKPOINT=1
  export TEST_DOTFILES_PENDING=1
  mkdir -m 700 "$USERLAND_BOOTSTRAP_CONTROL"
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$USERLAND_BOOTSTRAP_CONTROL/owner"
  mkdir -p "$USERLAND_DATA_DIR/bootstrap.lock"
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$USERLAND_DATA_DIR/bootstrap.lock/owner"

  run env \
    USERLAND_BOOTSTRAP_CREATED=1 \
    USERLAND_BOOTSTRAP_REPOSITORY_PREPARED=1 \
    USERLAND_UI_MODE=rich \
    USERLAND_UNICODE=1 \
    NO_COLOR=1 \
    "$TEST_ROOT/bin/userland" sync
  [ "$status" -eq 0 ]
  [[ "$output" == *$'◆  Preflight\n │\n ✓  Creating ~/.userland'* ]]
  [[ "$output" == *$'✓  Cloning giacomoguidotto/userland into ~/.userland'* ]]
  [[ "$output" == *$'✓  Userland matches the declaration\n │\n └  Done. This Mac matches userland. Run `userland doctor` to check the machine state\n    '* ]]
  [ "$(cat "$USERLAND_BOOTSTRAP_CONTROL/apply-started")" = "$USERLAND_BOOTSTRAP_TOKEN" ]
  [ -z "$(find "$USERLAND_BOOTSTRAP_CONTROL" -name '.apply-started.*' -print)" ]
  [ -s "$USERLAND_CACHE_DIR/zsh/init.zsh" ]
  grep -q '^# userland-shell-cache: [0-9a-f]\{64\}$' "$USERLAND_CACHE_DIR/zsh/init.zsh"
  grep -q 'compinit -C' "$USERLAND_CACHE_DIR/zsh/init.zsh"
  grep -q 'compdef _userland userland' "$USERLAND_CACHE_DIR/zsh/init.zsh"
  ! grep -q '^complete ' "$USERLAND_CACHE_DIR/zsh/init.zsh"
  grep -q 'bootstrap packages apply --yes' "$MISE_CALLS"
  grep -q 'bootstrap packages upgrade --yes' "$MISE_CALLS"
  grep -q 'bootstrap --yes --only tools' "$MISE_CALLS"
  grep -q 'bootstrap macos defaults apply --yes' "$MISE_CALLS"
  grep -q 'bootstrap dotfiles apply --yes --force' "$MISE_CALLS"
  ! grep -q 'bootstrap --yes --skip packages' "$MISE_CALLS"
  tools_line=$(grep -n 'bootstrap --yes --only tools' "$MISE_CALLS" | cut -d: -f1)
  macos_line=$(grep -n 'bootstrap macos defaults apply --yes' "$MISE_CALLS" | cut -d: -f1)
  dotfiles_line=$(grep -n 'bootstrap dotfiles apply --yes' "$MISE_CALLS" | cut -d: -f1)
  [ "$tools_line" -lt "$macos_line" ]
  [ "$macos_line" -lt "$dotfiles_line" ]
}
