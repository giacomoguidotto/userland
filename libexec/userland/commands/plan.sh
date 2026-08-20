#!/bin/sh

# shellcheck source=../common.sh
. "$USERLAND_ROOT/libexec/userland/common.sh"
# shellcheck source=../repository.sh
. "$USERLAND_ROOT/libexec/userland/repository.sh"
# shellcheck source=../dotfiles.sh
. "$USERLAND_ROOT/libexec/userland/dotfiles.sh"

userland_plan() {
  userland_require_schema
  userland_require_mise
  userland_mkdirs
  userland_refresh_repository_snapshot

  userland_log plan "source=mise.toml,Brewfile,state/* reason=converge declared personal state"
  userland_log plan "download=reported by package managers duration=seconds when current; a fresh Mac may take 30-90 minutes"
  userland_log plan "restart=no automatic reboot; macOS or an application may request a relaunch or logout"
  userland_log plan "reversibility=Git-backed configuration; package and application upgrades have no automatic rollback"

  userland_log plan "missing rolling packages"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages apply --dry-run

  userland_log plan "installed rolling package upgrades"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap packages upgrade --dry-run

  userland_log plan "native tools, dotfiles, and macOS state"
  # Packages are planned above. Dotfiles are reported from their status graph
  # because sync never force-overwrites unmanaged conflicts.
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap --dry-run --skip packages,dotfiles
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status || true

  userland_log plan "safe legacy-link migration"
  userland_plan_legacy_dotfiles

  userland_log plan "userland adapters"
  userland_run_adapters plan
}
