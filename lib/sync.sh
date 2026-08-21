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

  userland_prepare_legacy_dotfiles

  userland_ui section "Apply machine state"
  userland_ui task apply "Apply tools, files, repositories, and macOS preferences" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --yes --skip packages --jobs "${USERLAND_JOBS:-4}"

  userland_ui section "Apply personal state"
  USERLAND_UI_HIDE_OK=1
  export USERLAND_UI_HIDE_OK
  userland_run_adapters apply
  unset USERLAND_UI_HIDE_OK

  userland_ui section "Verify"
  # shellcheck source=doctor.sh
  . "$USERLAND_ROOT/lib/doctor.sh"
  if userland_doctor embedded; then
    if ! userland_trash_legacy_checkout; then
      userland_ui summary attention "Sync complete, but a legacy checkout needs review."
      return 2
    fi
    userland_ui summary ok "Sync complete. This Mac matches userland."
    return 0
  fi
  userland_ui summary attention "Sync complete with steps that need attention."
  return 2
}
