#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/libexec/userland/common.sh"

userland_file_handlers=$USERLAND_ROOT/state/file-handlers.tsv

userland_file_handler_matches() {
  userland_expected_bundle=$1
  userland_extension=$2
  userland_actual_bundle=$(duti -x "$userland_extension" 2>/dev/null | sed -n '3p')
  [ "$userland_actual_bundle" = "$userland_expected_bundle" ]
}

userland_check_file_handlers() {
  userland_file_handler_drift=0
  while IFS="$(printf '\t')" read -r userland_bundle userland_extension userland_role; do
    case "$userland_bundle" in '' | '#'*) continue ;; esac
    if userland_file_handler_matches "$userland_bundle" "$userland_extension"; then
      :
    else
      userland_log attention ".$userland_extension is not assigned to $userland_bundle"
      userland_file_handler_drift=$((userland_file_handler_drift + 1))
    fi
  done <"$userland_file_handlers"
  [ "$userland_file_handler_drift" -eq 0 ]
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    command -v duti >/dev/null 2>&1 || {
      userland_log change "file-handler declarations will apply after duti is installed"
      exit 0
    }
    userland_check_file_handlers || exit 2
    userland_log current "declared file handlers match"
    ;;
  apply)
    userland_is_macos || exit 0
    command -v duti >/dev/null 2>&1 || {
      userland_log attention "duti is unavailable; skipped file-handler declarations"
      exit 2
    }
    while IFS="$(printf '\t')" read -r userland_bundle userland_extension userland_role; do
      case "$userland_bundle" in '' | '#'*) continue ;; esac
      if ! userland_file_handler_matches "$userland_bundle" "$userland_extension"; then
        duti -s "$userland_bundle" "$userland_extension" "$userland_role"
        userland_log changed "assigned .$userland_extension to $userland_bundle"
      fi
    done <"$userland_file_handlers"
    ;;
  doctor)
    userland_is_macos || exit 0
    command -v duti >/dev/null 2>&1 || {
      userland_log attention "duti is unavailable"
      exit 2
    }
    userland_check_file_handlers || exit 2
    userland_log healthy "declared file handlers match"
    ;;
  *)
    userland_die "file-handlers adapter expects plan, apply, or doctor" 64
    ;;
esac
