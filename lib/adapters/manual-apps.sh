#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_manual_apps=$USERLAND_ROOT/config/manual-apps.tsv

userland_check_manual_apps() {
  userland_missing_apps=0
  while IFS="$(printf '\t')" read -r userland_app_name userland_app_path userland_app_reason; do
    case "$userland_app_name" in '' | '#'*) continue ;; esac
    if [ -e "$userland_app_path" ]; then
      userland_log healthy "$userland_app_name is installed"
    else
      userland_log manual "$userland_app_name: $userland_app_reason"
      userland_missing_apps=$((userland_missing_apps + 1))
    fi
  done <"$userland_manual_apps"
  [ "$userland_missing_apps" -eq 0 ]
}

case "${1:-}" in
  plan | doctor)
    userland_is_macos || exit 0
    userland_check_manual_apps || exit 2
    ;;
  apply)
    # These installers require a vendor or Apple interaction. We report them in
    # the same run but never download unverified installers or accept licenses.
    ;;
  *)
    userland_die "manual-apps adapter expects plan, apply, or doctor" 64
    ;;
esac
