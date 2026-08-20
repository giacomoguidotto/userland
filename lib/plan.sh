#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"
# shellcheck source=repository.sh
. "$USERLAND_ROOT/lib/repository.sh"
# shellcheck source=dotfiles.sh
. "$USERLAND_ROOT/lib/dotfiles.sh"

userland_plan() {
  userland_plan_mode=${1:-standalone}
  userland_require_schema
  userland_require_mise
  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui command plan "Preview declared state without applying it."
  fi
  userland_mkdirs
  userland_refresh_repository_snapshot

  userland_log info "A fresh Mac may take 30-90 minutes. Package managers report download sizes when available."
  userland_log info "No automatic reboot. Some changes may need an app relaunch or logout."
  userland_log info "Configuration is Git-backed. Package and application upgrades have no automatic rollback."

  userland_ui section "Packages"
  userland_log info "Missing rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages apply --dry-run

  userland_log info "Available rolling package upgrades"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages upgrade --dry-run

  userland_ui section "Machine state"
  # Packages are planned above. Dotfiles are reported from their status graph
  # because sync never force-overwrites unmanaged conflicts.
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --dry-run --skip packages,dotfiles
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status || true

  userland_ui section "Legacy files"
  userland_plan_legacy_dotfiles

  userland_ui section "Personal state"
  userland_run_adapters plan

  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui summary ok "Plan complete. No changes were applied."
  fi
}
