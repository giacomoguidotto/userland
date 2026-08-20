#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"
# shellcheck source=repository.sh
. "$USERLAND_ROOT/lib/repository.sh"
# shellcheck source=dotfiles.sh
. "$USERLAND_ROOT/lib/dotfiles.sh"
# shellcheck source=preflight.sh
. "$USERLAND_ROOT/lib/preflight.sh"

userland_confirm_sync() {
  if [ "${USERLAND_ASSUME_YES:-}" = 1 ]; then
    userland_log consent "continuing because USERLAND_ASSUME_YES=1"
    return 0
  fi
  [ -t 0 ] || userland_die "sync needs an interactive terminal to confirm its plan"
  printf 'Apply this plan? [y/N] ' >/dev/tty
  IFS= read -r userland_confirmation </dev/tty
  case "$userland_confirmation" in
    y | Y | yes | YES) ;;
    *)
      userland_log cancelled "no changes were applied"
      return 3
      ;;
  esac
}

userland_sync() {
  userland_require_schema
  userland_require_mise
  userland_mkdirs
  userland_sync_preflight

  # Sync updates its own clean main checkout first. The plan and consent below
  # therefore describe exactly the version that will be applied.
  if [ -z "${USERLAND_ARCHIVE:-}" ] && [ -z "${USERLAND_REFRESHED:-}" ]; then
    userland_refresh_code=0
    userland_repository_refresh_checkout || userland_refresh_code=$?
    if [ "$userland_refresh_code" -eq 10 ]; then
      USERLAND_REFRESHED=1 exec "$USERLAND_ROOT/bin/userland" sync
    elif [ "$userland_refresh_code" -ne 0 ]; then
      return "$userland_refresh_code"
    fi
  fi

  # A fast-forward can move managed source paths. Repair those links before
  # any replanning or package work so an interrupted layout upgrade is safe.
  if [ "${USERLAND_REFRESHED:-}" = 1 ]; then
    userland_prepare_legacy_dotfiles
  fi

  # Show one combined read-only plan before package or declared-state changes.
  # The same native operations run below without force semantics.
  # shellcheck source=plan.sh
  . "$USERLAND_ROOT/lib/plan.sh"
  userland_plan
  userland_confirm_sync

  userland_log sync "installing missing rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages apply --yes --jobs "${USERLAND_JOBS:-4}"

  userland_log sync "upgrading installed rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages upgrade --yes --jobs "${USERLAND_JOBS:-4}"

  userland_prepare_legacy_dotfiles

  userland_log sync "applying declared tools, dotfiles, repositories, and macOS preferences"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --yes --skip packages --jobs "${USERLAND_JOBS:-4}"

  userland_log sync "applying attended and app-specific state"
  userland_run_adapters apply

  userland_log sync "verifying the result"
  # shellcheck source=doctor.sh
  . "$USERLAND_ROOT/lib/doctor.sh"
  if userland_doctor; then
    return 0
  fi
  return 2
}
