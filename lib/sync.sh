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
  if [ "$userland_confirm_code" -eq 3 ] && [ "${USERLAND_BOOTSTRAP_CREATED:-0}" != 1 ]; then
    userland_ui summary cancelled "Cancelled. No changes were applied."
  fi
  return "$userland_confirm_code"
}

userland_bootstrap_mark_apply_started() {
  [ -n "${USERLAND_BOOTSTRAP_CONTROL:-}" ] || return 0
  [ -n "${USERLAND_BOOTSTRAP_TOKEN:-}" ] ||
    userland_die "bootstrap control token is missing"
  [ -d "$USERLAND_BOOTSTRAP_CONTROL" ] && [ ! -L "$USERLAND_BOOTSTRAP_CONTROL" ] ||
    userland_die "bootstrap control directory is invalid"
  [ -f "$USERLAND_BOOTSTRAP_CONTROL/owner" ] && [ ! -L "$USERLAND_BOOTSTRAP_CONTROL/owner" ] ||
    userland_die "bootstrap control owner is invalid"
  [ "$(cat "$USERLAND_BOOTSTRAP_CONTROL/owner")" = "$USERLAND_BOOTSTRAP_TOKEN" ] ||
    userland_die "bootstrap control owner does not match"

  userland_apply_marker=$USERLAND_BOOTSTRAP_CONTROL/.apply-started.$$
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$userland_apply_marker"
  mv "$userland_apply_marker" "$USERLAND_BOOTSTRAP_CONTROL/apply-started"
}

userland_bootstrap_require_lock_access() {
  userland_bootstrap_lock=$USERLAND_DATA_DIR/bootstrap.lock
  [ -e "$userland_bootstrap_lock" ] || [ -L "$userland_bootstrap_lock" ] || return 0
  [ -d "$userland_bootstrap_lock" ] && [ ! -L "$userland_bootstrap_lock" ] ||
    userland_die "bootstrap lock is invalid: $userland_bootstrap_lock"
  [ -n "${USERLAND_BOOTSTRAP_TOKEN:-}" ] ||
    userland_die "bootstrap is preparing ~/.userland; finish or cancel that run first"
  [ -f "$userland_bootstrap_lock/owner" ] && [ ! -L "$userland_bootstrap_lock/owner" ] ||
    userland_die "bootstrap lock owner is invalid"
  [ "$(cat "$userland_bootstrap_lock/owner")" = "$USERLAND_BOOTSTRAP_TOKEN" ] ||
    userland_die "another userland bootstrap owns this checkout"
}

userland_sync() {
  userland_require_schema
  userland_require_mise
  userland_mkdirs
  userland_bootstrap_require_lock_access
  userland_ui command sync "Bring this Mac in line with the state declared in giacomoguidotto/userland."
  userland_ui section "Preflight"
  if [ "${USERLAND_BOOTSTRAP_CREATED:-0}" = 1 ]; then
    userland_log changed "Creating ~/.userland"
  fi
  if [ "${USERLAND_BOOTSTRAP_REPOSITORY_PREPARED:-0}" = 1 ]; then
    userland_log changed "Cloning giacomoguidotto/userland into ~/.userland"
  fi
  userland_dotfiles_recover ||
    userland_die "managed-file recovery needs attention before sync can continue"
  userland_dotfiles_prune_recovery ||
    userland_die "managed-file recovery cleanup failed"
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

  # Show one combined read-only plan before package or declared-state changes.
  # The same native operations run below without force semantics.
  # shellcheck source=plan.sh
  . "$USERLAND_ROOT/lib/plan.sh"
  userland_plan embedded
  userland_plan_require_applicable
  userland_confirm_sync
  userland_bootstrap_mark_apply_started

  userland_ui section "Apply packages"
  USERLAND_UI_PROGRESS=mise-install
  export USERLAND_UI_PROGRESS
  userland_ui task apply "Install missing rolling packages" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages apply --yes --jobs "${USERLAND_JOBS:-4}"

  USERLAND_UI_PROGRESS=mise-upgrade
  export USERLAND_UI_PROGRESS
  userland_ui task apply "Upgrade installed rolling packages" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages upgrade --yes --jobs "${USERLAND_JOBS:-4}"
  unset USERLAND_UI_PROGRESS

  userland_ui section "Apply machine state"
  userland_ui task apply "Install pinned development tools" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --yes --only tools --jobs "${USERLAND_JOBS:-4}"
  userland_ui task apply "Apply macOS preferences" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap macos defaults apply --yes

  userland_ui section "Apply personal state"
  USERLAND_UI_HIDE_OK=1
  export USERLAND_UI_HIDE_OK
  userland_adapters_code=0
  userland_run_adapters apply || userland_adapters_code=$?
  unset USERLAND_UI_HIDE_OK
  if [ "$userland_adapters_code" -ne 0 ]; then
    userland_ui summary error "Stopped at the failed step. Fix it, then rerun sync."
    return "$userland_adapters_code"
  fi

  userland_ui section "Apply managed files"
  userland_ui task apply "Apply managed files transactionally" userland_dotfiles_apply

  userland_ui section "Verify"
  # shellcheck source=doctor.sh
  . "$USERLAND_ROOT/lib/doctor.sh"
  if userland_doctor embedded; then
    if [ -e "$USERLAND_DATA_DIR/repo" ] && userland_dotfiles_recovery_window_open; then
      userland_log current "Keeping the legacy checkout for the 24-hour recovery window"
    elif ! userland_trash_legacy_checkout; then
      userland_ui summary attention "Sync complete, but a legacy checkout needs review."
      return 2
    fi
    userland_ui spacer
    userland_ui summary ok 'Done. This Mac matches userland. Run `userland doctor` to check the machine state'
    return 0
  fi
  userland_ui summary attention "Done with steps that need attention."
  return 2
}
