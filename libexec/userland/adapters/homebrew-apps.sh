#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/libexec/userland/common.sh"

userland_brewfile=$USERLAND_ROOT/Brewfile
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

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_log change "Homebrew and declared personal applications will be installed"
    elif userland_homebrew_check >/dev/null 2>&1; then
      userland_log current "declared Homebrew applications are installed"
    else
      userland_log change "Homebrew will install or adopt missing personal applications"
    fi
    ;;
  apply)
    userland_is_macos || exit 0
    if ! userland_brew --version >/dev/null 2>&1; then
      userland_install_homebrew
    fi
    HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 userland_brew update
    HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 \
      userland_brew bundle --file "$userland_brewfile"
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
