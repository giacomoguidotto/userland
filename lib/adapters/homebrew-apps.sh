#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_brewfile=$USERLAND_ROOT/config/brewfile
userland_homebrew_commit=cced90146ea6d3057c03a636b668fef177415eb3
userland_homebrew_installer_sha256=12479a24be3f5307eecac7cde670fad7118640f031229e964f544b1367b52a41
userland_homebrew_installer_url=https://raw.githubusercontent.com/Homebrew/install/$userland_homebrew_commit/install.sh

userland_brew() {
  if [ -n "${USERLAND_BREW:-}" ] && [ -x "$USERLAND_BREW" ]; then
    "$USERLAND_BREW" "$@"
  elif [ -x /opt/homebrew/bin/brew ]; then
    /opt/homebrew/bin/brew "$@"
  elif command -v brew >/dev/null 2>&1; then
    brew "$@"
  else
    return 127
  fi
}

userland_homebrew_env() {
  HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 HOMEBREW_NO_COLOR=1 \
    userland_brew "$@"
}

userland_install_homebrew() {
  userland_homebrew_tmp=$(mktemp -d "${TMPDIR:-/tmp}/userland-homebrew.XXXXXX")
  trap 'rm -rf "$userland_homebrew_tmp"' EXIT HUP INT TERM
  userland_homebrew_installer=$userland_homebrew_tmp/install.sh
  curl --proto '=https' --tlsv1.2 -fsSL "$userland_homebrew_installer_url" -o "$userland_homebrew_installer"
  [ "$(userland_sha256 "$userland_homebrew_installer")" = "$userland_homebrew_installer_sha256" ] ||
    userland_die 'Homebrew installer checksum mismatch'
  userland_log changed "installing Homebrew from pinned commit $userland_homebrew_commit"
  NONINTERACTIVE=1 /bin/bash "$userland_homebrew_installer"
}

userland_homebrew_declarations() {
  while IFS= read -r userland_homebrew_line || [ -n "$userland_homebrew_line" ]; do
    case "$userland_homebrew_line" in
      '' | \#*) continue ;;
      tap\ \"* | brew\ \"* | cask\ \"* | mas\ \"*)
        userland_homebrew_kind=${userland_homebrew_line%% *}
        userland_homebrew_name=${userland_homebrew_line#*\"}
        userland_homebrew_name=${userland_homebrew_name%%\"*}
        printf '%s\t%s\n' "$userland_homebrew_kind" "$userland_homebrew_name"
        ;;
      *) return 2 ;;
    esac
  done <"$userland_brewfile"
}

userland_homebrew_declares_tap() {
  grep -Fq "tap \"$1\"" "$userland_brewfile"
}

userland_homebrew_cask_adoptable() {
  command -v jq >/dev/null 2>&1 || return 1
  userland_homebrew_cask_json=$(userland_homebrew_env info --json=v2 --cask "$1" 2>/dev/null) || return 1
  userland_homebrew_cask_target=$(printf '%s' "$userland_homebrew_cask_json" | jq -r '
    .casks[0].artifacts[]? | select(.app != null) |
    (.target // ("/Applications/" + .app[0]))
  ' 2>/dev/null | sed -n '1p')
  [ -n "$userland_homebrew_cask_target" ] && [ -d "$userland_homebrew_cask_target" ]
}

userland_homebrew_collect_missing() {
  userland_homebrew_missing_output=$(mktemp "$USERLAND_CACHE_DIR/homebrew-missing.XXXXXX")
  if userland_homebrew_env bundle check --file "$userland_brewfile" --no-upgrade --verbose \
    >"$userland_homebrew_missing_output" 2>&1; then
    rm -f "$userland_homebrew_missing_output"
    return 0
  fi
  while IFS= read -r userland_homebrew_line || [ -n "$userland_homebrew_line" ]; do
    case "$userland_homebrew_line" in
      '→ '*) userland_homebrew_entry=${userland_homebrew_line#'→ '} ;;
      '-> '*) userland_homebrew_entry=${userland_homebrew_line#'-> '} ;;
      *) continue ;;
    esac
    case "$userland_homebrew_entry" in
      *' needs to be installed.') userland_homebrew_entry=${userland_homebrew_entry%' needs to be installed.'} ;;
      *' needs to be tapped.') userland_homebrew_entry=${userland_homebrew_entry%' needs to be tapped.'} ;;
      *) continue ;;
    esac
    userland_homebrew_kind=${userland_homebrew_entry%% *}
    userland_homebrew_name=${userland_homebrew_entry#* }
    case "$userland_homebrew_kind" in
      Cask)
        if userland_homebrew_cask_adoptable "$userland_homebrew_name"; then
          printf 'adoptable\tCask\t%s\n' "$userland_homebrew_name"
        else
          printf 'missing\tCask\t%s\n' "$userland_homebrew_name"
        fi
        ;;
      Tap | Formula | App) printf 'missing\t%s\t%s\n' "$userland_homebrew_kind" "$userland_homebrew_name" ;;
    esac
  done <"$userland_homebrew_missing_output"
  rm -f "$userland_homebrew_missing_output"
}

userland_homebrew_collect_outdated_kind() {
  userland_homebrew_decl_kind=$1
  userland_homebrew_flag=$2
  command -v jq >/dev/null 2>&1 || return 0
  userland_homebrew_outdated_json=$(userland_homebrew_env outdated "$userland_homebrew_flag" --json=v2 2>/dev/null) || return 0
  [ -n "$userland_homebrew_outdated_json" ] || return 0
  case "$userland_homebrew_decl_kind" in brew) userland_homebrew_json_key=formulae ;; cask) userland_homebrew_json_key=casks ;; esac
  printf '%s' "$userland_homebrew_outdated_json" |
    jq -r --arg kind "$userland_homebrew_decl_kind" ".${userland_homebrew_json_key}[]? | [\"outdated\", \$kind, (.full_name // .name)] | @tsv" 2>/dev/null
}

userland_homebrew_collect_stale_untrusted_taps() {
  command -v jq >/dev/null 2>&1 || return 0
  userland_homebrew_trust_json=$(userland_homebrew_env trust --json=v1 2>/dev/null) || return 0
  userland_homebrew_installed_formulae=$(userland_homebrew_env list --formula --full-name 2>/dev/null) || return 0
  userland_homebrew_installed_casks=$(userland_homebrew_env list --cask --full-name 2>/dev/null) || return 0
  userland_homebrew_taps=$(userland_homebrew_env tap 2>/dev/null) || return 0

  userland_homebrew_declarations | while IFS="$(printf '\t')" read -r userland_homebrew_kind userland_homebrew_tap; do
    [ "$userland_homebrew_kind" = tap ] || continue
    printf '%s\n' "$userland_homebrew_taps" | grep -Fxq "$userland_homebrew_tap" || continue
    printf '%s' "$userland_homebrew_trust_json" | jq -e --arg tap "$userland_homebrew_tap" '.taps[]? | select(. == $tap)' >/dev/null 2>&1 && continue
    printf 'untrusted-declared\tTap\t%s\n' "$userland_homebrew_tap"
  done

  printf '%s\n' "$userland_homebrew_taps" | while IFS= read -r userland_homebrew_tap; do
    [ -n "$userland_homebrew_tap" ] || continue
    userland_homebrew_declares_tap "$userland_homebrew_tap" && continue
    printf '%s' "$userland_homebrew_trust_json" | jq -e --arg tap "$userland_homebrew_tap" '.taps[]? | select(. == $tap)' >/dev/null 2>&1 && continue
    printf '%s\n' "$userland_homebrew_installed_formulae" | grep -Fq "$userland_homebrew_tap/" && continue
    printf '%s\n' "$userland_homebrew_installed_casks" | grep -Fq "$userland_homebrew_tap/" && continue
    printf 'untrusted-unused\tTap\t%s\n' "$userland_homebrew_tap"
  done
}

userland_homebrew_collect_health() {
  userland_homebrew_collect_dir=$(mktemp -d "$USERLAND_CACHE_DIR/homebrew-collect.XXXXXX")
  (userland_homebrew_collect_missing >"$userland_homebrew_collect_dir/missing") &
  userland_homebrew_missing_pid=$!
  (userland_homebrew_collect_outdated_kind brew --formula >"$userland_homebrew_collect_dir/formula") &
  userland_homebrew_formula_pid=$!
  (userland_homebrew_collect_outdated_kind cask --cask >"$userland_homebrew_collect_dir/cask") &
  userland_homebrew_cask_pid=$!
  (userland_homebrew_collect_stale_untrusted_taps >"$userland_homebrew_collect_dir/taps") &
  userland_homebrew_taps_pid=$!
  for userland_homebrew_collect_pid in \
    "$userland_homebrew_missing_pid" \
    "$userland_homebrew_formula_pid" \
    "$userland_homebrew_cask_pid" \
    "$userland_homebrew_taps_pid"; do
    wait "$userland_homebrew_collect_pid" || return 1
  done
  cat "$userland_homebrew_collect_dir/missing" \
    "$userland_homebrew_collect_dir/formula" \
    "$userland_homebrew_collect_dir/cask" \
    "$userland_homebrew_collect_dir/taps"
  rm -f "$userland_homebrew_collect_dir/missing" \
    "$userland_homebrew_collect_dir/formula" \
    "$userland_homebrew_collect_dir/cask" \
    "$userland_homebrew_collect_dir/taps"
  rmdir "$userland_homebrew_collect_dir"
}

userland_homebrew_plan_record() {
  userland_homebrew_state=$1
  userland_homebrew_kind=$2
  userland_homebrew_name=$3
  case "$userland_homebrew_state:$userland_homebrew_kind" in
    missing:Tap)
      userland_homebrew_action=install
      userland_homebrew_detail='add and trust the declared Homebrew tap'
      ;;
    missing:Formula)
      userland_homebrew_action=install
      userland_homebrew_detail='install with Homebrew'
      ;;
    missing:Cask)
      userland_homebrew_action=install
      userland_homebrew_detail='install the missing Homebrew cask'
      ;;
    missing:App)
      userland_homebrew_action=install
      userland_homebrew_detail='install from Mac App Store'
      ;;
    adoptable:Cask)
      userland_homebrew_action=install
      userland_homebrew_detail='adopt the existing application into Homebrew ownership'
      ;;
    outdated:brew)
      userland_homebrew_action=upgrade
      userland_homebrew_detail='upgrade the outdated installed Homebrew formula'
      ;;
    outdated:cask)
      userland_homebrew_action=upgrade
      userland_homebrew_detail='upgrade the outdated installed Homebrew cask'
      ;;
    untrusted-declared:Tap)
      userland_homebrew_action=configure
      userland_homebrew_detail='trust the declared Homebrew tap before loading its casks'
      ;;
    untrusted-unused:Tap)
      if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ]; then
        userland_plan_add cleanup remove automatic userland "$userland_homebrew_name" \
          'untap after proving no installed formula or cask depends on it' "homebrew:unused-untrusted-tap:$userland_homebrew_name"
      else
        userland_log change "$userland_homebrew_name: remove unused untrusted tap"
      fi
      return 0
      ;;
    *) return 64 ;;
  esac
  if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ]; then
    userland_plan_add apps "$userland_homebrew_action" automatic declared "$userland_homebrew_name" \
      "$userland_homebrew_detail" "homebrew:$userland_homebrew_state:$userland_homebrew_kind:$userland_homebrew_name"
  else
    userland_log change "$userland_homebrew_name: $userland_homebrew_detail"
  fi
}

userland_homebrew_render_plan() {
  userland_homebrew_health=$(mktemp "$USERLAND_CACHE_DIR/homebrew-health.XXXXXX")
  userland_homebrew_collect_health >"$userland_homebrew_health"
  if [ ! -s "$userland_homebrew_health" ]; then
    rm -f "$userland_homebrew_health"
    userland_log current 'declared Homebrew applications are installed and current'
    return 0
  fi
  while IFS="$(printf '\t')" read -r userland_homebrew_state userland_homebrew_kind userland_homebrew_name; do
    userland_homebrew_plan_record "$userland_homebrew_state" "$userland_homebrew_kind" "$userland_homebrew_name"
  done <"$userland_homebrew_health"
  rm -f "$userland_homebrew_health"
}

userland_homebrew_apply_health() {
  userland_homebrew_health=$(mktemp "$USERLAND_CACHE_DIR/homebrew-health.XXXXXX")
  userland_homebrew_collect_health >"$userland_homebrew_health"
  userland_homebrew_env bundle --file "$userland_brewfile" --no-upgrade
  userland_homebrew_formula_upgrades=
  userland_homebrew_cask_upgrades=
  while IFS="$(printf '\t')" read -r userland_homebrew_state userland_homebrew_kind userland_homebrew_name; do
    case "$userland_homebrew_state:$userland_homebrew_kind" in
      outdated:brew) userland_homebrew_formula_upgrades="$userland_homebrew_formula_upgrades $userland_homebrew_name" ;;
      outdated:cask) userland_homebrew_cask_upgrades="$userland_homebrew_cask_upgrades $userland_homebrew_name" ;;
      untrusted-unused:Tap)
        userland_homebrew_collect_stale_untrusted_taps | grep -Fqx "untrusted-unused$(printf '\t')Tap$(printf '\t')$userland_homebrew_name" ||
          userland_die "refusing to untap $userland_homebrew_name because its ownership changed"
        userland_homebrew_env untap "$userland_homebrew_name"
        ;;
    esac
  done <"$userland_homebrew_health"
  if [ -n "$userland_homebrew_formula_upgrades" ]; then
    # Homebrew names cannot contain whitespace; split the approved captured set.
    # shellcheck disable=SC2086
    userland_homebrew_env upgrade $userland_homebrew_formula_upgrades
  fi
  if [ -n "$userland_homebrew_cask_upgrades" ]; then
    # shellcheck disable=SC2086
    userland_homebrew_env upgrade --cask $userland_homebrew_cask_upgrades
  fi
  rm -f "$userland_homebrew_health"
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ]; then
        userland_plan_add apps install automatic declared Homebrew 'install from the pinned Homebrew installer' 'homebrew:missing:Homebrew'
      else
        userland_log change 'Homebrew: install from the pinned Homebrew installer'
      fi
      userland_homebrew_declarations | while IFS="$(printf '\t')" read -r userland_homebrew_kind userland_homebrew_name; do
        case "$userland_homebrew_kind" in tap) userland_homebrew_kind=Tap ;; brew) userland_homebrew_kind=Formula ;; cask) userland_homebrew_kind=Cask ;; mas) userland_homebrew_kind=App ;; esac
        userland_homebrew_plan_record missing "$userland_homebrew_kind" "$userland_homebrew_name"
      done
    else
      userland_homebrew_render_plan
    fi
    ;;
  apply)
    userland_is_macos || exit 0
    userland_brew --version >/dev/null 2>&1 || userland_install_homebrew
    userland_homebrew_apply_health
    ;;
  doctor)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_log attention 'Homebrew is missing'
      exit 2
    fi
    userland_homebrew_health=$(mktemp "$USERLAND_CACHE_DIR/homebrew-health.XXXXXX")
    userland_homebrew_collect_health >"$userland_homebrew_health"
    if [ -s "$userland_homebrew_health" ]; then
      while IFS="$(printf '\t')" read -r userland_homebrew_state _ userland_homebrew_name; do
        userland_log attention "$userland_homebrew_name is $userland_homebrew_state"
      done <"$userland_homebrew_health"
      rm -f "$userland_homebrew_health"
      exit 2
    fi
    rm -f "$userland_homebrew_health"
    userland_log healthy 'declared Homebrew applications are installed and current'
    ;;
  *) userland_die 'homebrew-apps adapter expects plan, apply, or doctor' 64 ;;
esac
