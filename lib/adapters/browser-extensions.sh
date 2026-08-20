#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_browser_extensions=$USERLAND_ROOT/config/browser-extensions.tsv

userland_browser_root() {
  case "$1" in
    helium) printf '%s\n' "$USERLAND_HOME/Library/Application Support/net.imput.helium" ;;
    chrome) printf '%s\n' "$USERLAND_HOME/Library/Application Support/Google/Chrome" ;;
    *) return 1 ;;
  esac
}

userland_open_extension_store() {
  userland_extension_browser=$1
  userland_extension_url=$2
  case "$userland_extension_browser" in
    helium) open -a Helium "$userland_extension_url" ;;
    chrome) open -a "Google Chrome" "$userland_extension_url" ;;
    *) return 1 ;;
  esac
}

userland_browser_extension_is_installed() {
  userland_extension_browser=$1
  userland_extension_id=$2
  userland_extension_root=$(userland_browser_root "$userland_extension_browser")
  for userland_extension_directory in \
    "$userland_extension_root/Extensions/$userland_extension_id" \
    "$userland_extension_root"/*/Extensions/"$userland_extension_id"; do
    [ -d "$userland_extension_directory" ] && return 0
  done
  return 1
}

userland_browser_extensions_check() {
  userland_extension_drift=0
  while IFS="$(printf '\t')" read -r userland_extension_browser userland_extension_id userland_extension_name; do
    case "$userland_extension_browser" in '' | '#'*) continue ;; esac
    if ! userland_browser_extension_is_installed "$userland_extension_browser" "$userland_extension_id"; then
      userland_log manual "$userland_extension_name is missing from $userland_extension_browser"
      userland_extension_drift=$((userland_extension_drift + 1))
    fi
  done <"$userland_browser_extensions"
  [ "$userland_extension_drift" -eq 0 ]
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    userland_browser_extensions_check || exit 2
    userland_log current "declared browser extensions are installed"
    ;;
  apply)
    userland_is_macos || exit 0
    userland_browser_extensions_check && exit 0
    userland_log manual "opening the supported Chrome Web Store pages for missing extensions"
    while IFS="$(printf '\t')" read -r userland_extension_browser userland_extension_id userland_extension_name; do
      case "$userland_extension_browser" in '' | '#'*) continue ;; esac
      if ! userland_browser_extension_is_installed "$userland_extension_browser" "$userland_extension_id"; then
        userland_open_extension_store "$userland_extension_browser" \
          "https://chromewebstore.google.com/detail/$userland_extension_id"
      fi
    done <"$userland_browser_extensions"
    exit 2
    ;;
  doctor)
    userland_is_macos || exit 0
    if userland_browser_extensions_check; then
      userland_log healthy "declared browser extensions are installed"
    else
      exit 2
    fi
    ;;
  *)
    userland_die "browser-extensions adapter expects plan, apply, or doctor" 64
    ;;
esac
