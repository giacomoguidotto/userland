#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/libexec/userland/common.sh"

userland_link_target() {
  /usr/bin/stat -f '%Y' "$1" 2>/dev/null || /usr/bin/stat -c '%N' "$1" 2>/dev/null
}

userland_is_owned_legacy_source() {
  case "$1" in
    */workspace/cfg/* | "$USERLAND_DATA_DIR"/releases/*/cfg/*) return 0 ;;
    *) return 1 ;;
  esac
}

userland_preserve_unmanaged_children() {
  userland_old_directory=$1
  userland_new_source=$2
  userland_new_directory=$3

  for userland_old_child in "$userland_old_directory"/* "$userland_old_directory"/.[!.]* "$userland_old_directory"/..?*; do
    [ -e "$userland_old_child" ] || continue
    userland_child_name=${userland_old_child##*/}
    [ -e "$userland_new_source/$userland_child_name" ] && continue
    cp -pR "$userland_old_child" "$userland_new_directory/$userland_child_name"
    userland_log preserved "$userland_new_directory/$userland_child_name"
  done
}

userland_migrate_legacy_link() {
  userland_legacy_link=$1
  [ -L "$userland_legacy_link" ] || return 0

  userland_legacy_source=$(userland_link_target "$userland_legacy_link")
  userland_is_owned_legacy_source "$userland_legacy_source" || return 0

  userland_cfg_suffix=${userland_legacy_source#*/cfg/}
  userland_replacement_source=$USERLAND_ROOT/cfg/$userland_cfg_suffix
  [ -e "$userland_replacement_source" ] || return 0

  if [ -d "$userland_legacy_source" ]; then
    userland_migration_tmp=$userland_legacy_link.userland-migrate.$$
    mkdir -p "$userland_migration_tmp"
    userland_preserve_unmanaged_children "$userland_legacy_source" "$userland_replacement_source" "$userland_migration_tmp"
    rm "$userland_legacy_link"
    mv "$userland_migration_tmp" "$userland_legacy_link"
  else
    rm "$userland_legacy_link"
  fi
  userland_log changed "released legacy workspace ownership of $userland_legacy_link"
}

userland_prepare_legacy_dotfiles() {
  userland_log sync "checking legacy workspace links"

  for userland_legacy_link in \
    "$USERLAND_HOME"/.zshrc \
    "$USERLAND_HOME"/.zshenv \
    "$USERLAND_HOME"/.hushlogin \
    "$USERLAND_HOME"/.ssh/config \
    "$USERLAND_HOME"/.agents/skills \
    "$USERLAND_HOME"/.claude/skills \
    "$USERLAND_HOME"/.claude/settings.json \
    "$USERLAND_HOME"/.codex/AGENTS.md \
    "$USERLAND_HOME"/.config/opencode/AGENTS.md \
    "$USERLAND_HOME"/.config/*; do
    [ -L "$userland_legacy_link" ] || continue
    userland_migrate_legacy_link "$userland_legacy_link"
  done
}

userland_plan_legacy_dotfiles() {
  userland_legacy_count=0
  for userland_legacy_link in \
    "$USERLAND_HOME"/.zshrc \
    "$USERLAND_HOME"/.zshenv \
    "$USERLAND_HOME"/.hushlogin \
    "$USERLAND_HOME"/.ssh/config \
    "$USERLAND_HOME"/.agents/skills \
    "$USERLAND_HOME"/.claude/skills \
    "$USERLAND_HOME"/.claude/settings.json \
    "$USERLAND_HOME"/.codex/AGENTS.md \
    "$USERLAND_HOME"/.config/opencode/AGENTS.md \
    "$USERLAND_HOME"/.config/*; do
    [ -L "$userland_legacy_link" ] || continue
    userland_legacy_source=$(userland_link_target "$userland_legacy_link")
    userland_is_owned_legacy_source "$userland_legacy_source" || continue
    userland_log change "release legacy workspace link $userland_legacy_link"
    userland_legacy_count=$((userland_legacy_count + 1))
  done
  [ "$userland_legacy_count" -ne 0 ] || userland_log current "no legacy workspace links need migration"
}
