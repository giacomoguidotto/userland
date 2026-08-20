#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"
# shellcheck source=repository.sh
. "$USERLAND_ROOT/lib/repository.sh"
# shellcheck source=dotfiles.sh
. "$USERLAND_ROOT/lib/dotfiles.sh"

: "${userland_ui_task_log:=}"

userland_plan_mise_resources() {
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap plan --json >"$USERLAND_PLAN_RESULT"
}

userland_plan_mise_dotfiles() {
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status --json >"$USERLAND_PLAN_RESULT"
}

userland_plan_import_mise() {
  userland_plan_json=$1
  userland_plan_create=$(/usr/bin/plutil -extract summary.create raw -o - "$userland_plan_json" 2>/dev/null) || return 1
  userland_plan_update=$(/usr/bin/plutil -extract summary.update raw -o - "$userland_plan_json" 2>/dev/null) || return 1
  userland_plan_remove=$(/usr/bin/plutil -extract summary.remove raw -o - "$userland_plan_json" 2>/dev/null) || return 1
  userland_plan_unknown=$(/usr/bin/plutil -extract summary.unknown raw -o - "$userland_plan_json" 2>/dev/null) || return 1
  case "$userland_plan_create $userland_plan_update $userland_plan_remove $userland_plan_unknown" in
    *[!0-9\ ]*) return 1 ;;
  esac

  [ "$userland_plan_create" -eq 0 ] || userland_log change "$userland_plan_create managed resources to create"
  [ "$userland_plan_update" -eq 0 ] || userland_log change "$userland_plan_update managed resources to update"
  [ "$userland_plan_remove" -eq 0 ] || userland_log warning "$userland_plan_remove managed resources to remove"
  [ "$userland_plan_unknown" -eq 0 ] || userland_log warning "$userland_plan_unknown resources need manual review"
}

userland_plan_import_dotfiles() {
  userland_dotfiles_json=$1
  userland_dotfile_count=$(/usr/bin/plutil -extract files raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
  case "$userland_dotfile_count" in *[!0-9]* | '') return 1 ;; esac
  userland_dotfile_index=0
  userland_dotfile_drift=0
  userland_dotfile_risk=0
  while [ "$userland_dotfile_index" -lt "$userland_dotfile_count" ]; do
    userland_dotfile_state=$(/usr/bin/plutil -extract "files.$userland_dotfile_index.state" raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
    case "$userland_dotfile_state" in
      applied) ;;
      source_missing)
        userland_dotfile_drift=$((userland_dotfile_drift + 1))
        userland_dotfile_risk=$((userland_dotfile_risk + 1))
        ;;
      *) userland_dotfile_drift=$((userland_dotfile_drift + 1)) ;;
    esac
    userland_dotfile_index=$((userland_dotfile_index + 1))
  done
  [ "$userland_dotfile_drift" -eq 0 ] || userland_log change "$userland_dotfile_drift managed paths need reconciliation"
  [ "$userland_dotfile_risk" -eq 0 ] || userland_log warning "$userland_dotfile_risk managed sources are missing; review details before applying"
}

userland_plan() {
  userland_plan_mode=${1:-standalone}
  userland_require_schema
  userland_require_mise
  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui command plan "Preview declared state without applying it."
  fi
  userland_mkdirs
  userland_ui report begin
  userland_ui task inspect "Refresh repository index" userland_refresh_repository_snapshot

  userland_log info "A fresh Mac may take 30-90 minutes. Package managers report download sizes when available."
  userland_log info "No automatic reboot. Some changes may need an app relaunch or logout."
  userland_log info "Configuration is Git-backed. Package and application upgrades have no automatic rollback."

  userland_ui section "Software"
  USERLAND_PLAN_RESULT=$(mktemp "$USERLAND_CACHE_DIR/plan.XXXXXX")
  chmod 600 "$USERLAND_PLAN_RESULT"
  export USERLAND_PLAN_RESULT
  userland_ui task inspect "Inspect packages and system resources" \
    userland_plan_mise_resources
  userland_plan_import_mise "$USERLAND_PLAN_RESULT" ||
    userland_die "mise returned an unreadable plan; no approval was requested"

  userland_log change "Check installed rolling packages and apply available upgrades"

  userland_ui section "Machine state"
  # Packages are planned above. Dotfiles are reported from their status graph
  # because sync never force-overwrites unmanaged conflicts.
  userland_ui task inspect "Inspect managed paths" \
    userland_plan_mise_dotfiles
  userland_plan_import_dotfiles "$USERLAND_PLAN_RESULT" ||
    userland_die "mise returned unreadable managed-path status; no approval was requested"
  rm -f "$USERLAND_PLAN_RESULT"
  unset USERLAND_PLAN_RESULT

  userland_ui section "Legacy files"
  userland_plan_legacy_dotfiles

  userland_ui section "Personal state"
  userland_run_adapters plan

  userland_ui report render
  userland_log info "Fresh Mac: 30-90 minutes; no automatic reboot"
  userland_log info "Rollback covers configuration, not package or application upgrades"

  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui summary ok "Plan complete. No changes were applied."
  fi
}
