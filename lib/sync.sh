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
  userland_confirm_code=0
  userland_ui confirm "Apply this plan?" || userland_confirm_code=$?
  if [ "$userland_confirm_code" -eq 3 ]; then
    userland_ui summary cancelled "Cancelled. No changes were applied."
  fi
  return "$userland_confirm_code"
}

userland_sync() {
  userland_require_schema
  userland_require_mise
  userland_mkdirs
  userland_ui command sync "Update userland, apply declared state, then verify it."
  userland_ui section "Preflight"
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
  userland_plan embedded
  userland_confirm_sync

  userland_ui section "Apply packages"
  userland_log info "Installing missing rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages apply --yes --jobs "${USERLAND_JOBS:-4}"

  userland_log info "Upgrading installed rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages upgrade --yes --jobs "${USERLAND_JOBS:-4}"

  userland_prepare_legacy_dotfiles

  userland_ui section "Apply machine state"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --yes --skip packages --jobs "${USERLAND_JOBS:-4}"

  userland_ui section "Apply personal state"
  userland_run_adapters apply

  userland_ui section "Verify"
  # shellcheck source=doctor.sh
  . "$USERLAND_ROOT/lib/doctor.sh"
  if userland_doctor embedded; then
    userland_ui summary ok "Sync complete. This Mac matches userland."
    return 0
  fi
  userland_ui summary attention "Sync complete with steps that need attention."
  return 2
}
