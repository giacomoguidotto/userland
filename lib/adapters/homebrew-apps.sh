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

userland_install_homebrew() {
  userland_homebrew_tmp=$(mktemp -d "${TMPDIR:-/tmp}/userland-homebrew.XXXXXX")
  trap 'rm -rf "$userland_homebrew_tmp"' EXIT HUP INT TERM
  userland_homebrew_installer=$userland_homebrew_tmp/install.sh

  curl --proto '=https' --tlsv1.2 -fsSL "$userland_homebrew_installer_url" -o "$userland_homebrew_installer"
  userland_homebrew_actual_sha256=$(userland_sha256 "$userland_homebrew_installer")
  [ "$userland_homebrew_actual_sha256" = "$userland_homebrew_installer_sha256" ] ||
    userland_die "Homebrew installer checksum mismatch"

  userland_log changed "installing Homebrew from pinned commit $userland_homebrew_commit"
  NONINTERACTIVE=1 /bin/bash "$userland_homebrew_installer"
}

userland_homebrew_check() {
  HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 \
    userland_brew bundle check --file "$userland_brewfile" --no-upgrade
}

userland_homebrew_plan_add() {
  userland_homebrew_kind=$1
  userland_homebrew_name=$2
  case "$userland_homebrew_kind" in
    Tap) userland_homebrew_detail='add Homebrew tap' ;;
    Formula) userland_homebrew_detail='install with Homebrew' ;;
    Cask)
      if grep -Fq "cask \"$userland_homebrew_name\", args: { force: true }" "$userland_brewfile"; then
        userland_homebrew_detail='install or replace with the current Homebrew cask'
      else
        userland_homebrew_detail='install or adopt Homebrew cask'
      fi
      ;;
    App) userland_homebrew_detail='install from Mac App Store' ;;
    Homebrew) userland_homebrew_detail='install from the pinned Homebrew installer' ;;
    *) return 64 ;;
  esac

  if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ] && command -v userland_plan_add >/dev/null 2>&1; then
    userland_plan_add apps install automatic declared \
      "$userland_homebrew_name" \
      "$userland_homebrew_detail" \
      "brewfile:$userland_homebrew_kind:$userland_homebrew_name"
  else
    userland_log change "$userland_homebrew_name: $userland_homebrew_detail"
  fi
}

userland_homebrew_plan_brewfile() {
  while IFS= read -r userland_homebrew_line || [ -n "$userland_homebrew_line" ]; do
    case "$userland_homebrew_line" in
      '' | \#*) continue ;;
      tap\ \"* | brew\ \"* | cask\ \"* | mas\ \"*)
        userland_homebrew_declaration=${userland_homebrew_line%% *}
        userland_homebrew_name=${userland_homebrew_line#*\"}
        userland_homebrew_name=${userland_homebrew_name%%\"*}
        case "$userland_homebrew_declaration" in
          tap) userland_homebrew_kind=Tap ;;
          brew) userland_homebrew_kind=Formula ;;
          cask) userland_homebrew_kind=Cask ;;
          mas) userland_homebrew_kind=App ;;
        esac
        userland_homebrew_plan_add "$userland_homebrew_kind" "$userland_homebrew_name"
        ;;
      *)
        userland_log attention "Unsupported Brewfile declaration: $userland_homebrew_line"
        return 2
        ;;
    esac
  done <"$userland_brewfile"
}

userland_homebrew_plan_missing() {
  userland_homebrew_plan_output=$(mktemp "$USERLAND_CACHE_DIR/homebrew-plan.XXXXXX")
  if HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 \
    userland_brew bundle check --file "$userland_brewfile" --no-upgrade --verbose \
    >"$userland_homebrew_plan_output" 2>&1; then
    rm -f "$userland_homebrew_plan_output"
    userland_log current "declared Homebrew applications are installed"
    return 0
  fi

  userland_homebrew_missing_count=0
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
    case "$userland_homebrew_kind" in Tap | Formula | Cask | App) ;; *) continue ;; esac
    userland_homebrew_plan_add "$userland_homebrew_kind" "$userland_homebrew_name"
    userland_homebrew_missing_count=$((userland_homebrew_missing_count + 1))
  done <"$userland_homebrew_plan_output"
  rm -f "$userland_homebrew_plan_output"

  if [ "$userland_homebrew_missing_count" -eq 0 ]; then
    userland_log attention "Homebrew could not enumerate its missing applications"
    return 2
  fi
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_homebrew_plan_add Homebrew Homebrew
      userland_homebrew_plan_brewfile
    else
      userland_homebrew_plan_missing
    fi
    ;;
  apply)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_install_homebrew
    fi
    HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 HOMEBREW_NO_COLOR=1 \
      userland_brew bundle --file "$userland_brewfile" --no-upgrade
    ;;
  doctor)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_log attention "Homebrew is missing"
      exit 2
    elif userland_homebrew_check >/dev/null 2>&1; then
      userland_log healthy "declared Homebrew applications are installed"
    else
      userland_log attention "declared Homebrew applications are missing"
      exit 2
    fi
    ;;
  *)
    userland_die "homebrew-apps adapter expects plan, apply, or doctor" 64
    ;;
esac
