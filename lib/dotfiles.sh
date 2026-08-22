#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_dotfiles_write_state() {
  userland_dotfiles_state_dir=$1
  userland_dotfiles_state_value=$2
  userland_dotfiles_state_tmp=$userland_dotfiles_state_dir/.state.$$
  printf '%s\n' "$userland_dotfiles_state_value" >"$userland_dotfiles_state_tmp"
  chmod 600 "$userland_dotfiles_state_tmp"
  mv "$userland_dotfiles_state_tmp" "$userland_dotfiles_state_dir/state"
}

userland_dotfiles_clear_active() {
  userland_dotfiles_clear_id=$1
  userland_dotfiles_active=$USERLAND_STATE_DIR/recovery/active
  [ -f "$userland_dotfiles_active" ] || return 0
  [ "$(cat "$userland_dotfiles_active" 2>/dev/null)" = "$userland_dotfiles_clear_id" ] || return 0
  rm -f "$userland_dotfiles_active"
}

userland_dotfiles_expand_target() {
  case "$1" in
    \~/*) userland_dotfiles_target=$USERLAND_HOME/${1#\~/} ;;
    /*) userland_dotfiles_target=$1 ;;
    *) return 1 ;;
  esac
  case "$userland_dotfiles_target" in
    "$USERLAND_HOME"/*) ;;
    *) return 1 ;;
  esac
}

userland_dotfiles_array_count() {
  userland_dotfiles_array_path=$1
  userland_dotfiles_array_file=$2
  userland_dotfiles_array_size=$(/usr/bin/plutil -extract "$userland_dotfiles_array_path" raw -o - "$userland_dotfiles_array_file" 2>/dev/null) || return 1
  case "$userland_dotfiles_array_size" in '' | *[!0-9]*) return 1 ;; esac
  printf '%s\n' "$userland_dotfiles_array_size"
}

userland_dotfiles_status_is_applied() {
  userland_dotfiles_status=$1
  userland_dotfiles_file_count=$(userland_dotfiles_array_count files "$userland_dotfiles_status") || return 1
  userland_dotfiles_file_index=0
  while [ "$userland_dotfiles_file_index" -lt "$userland_dotfiles_file_count" ]; do
    userland_dotfiles_file_state=$(/usr/bin/plutil -extract "files.$userland_dotfiles_file_index.state" raw -o - "$userland_dotfiles_status" 2>/dev/null) || return 1
    [ "$userland_dotfiles_file_state" = applied ] || return 1
    userland_dotfiles_file_index=$((userland_dotfiles_file_index + 1))
  done

  userland_dotfiles_edit_count=$(userland_dotfiles_array_count edits "$userland_dotfiles_status") || return 1
  userland_dotfiles_edit_index=0
  while [ "$userland_dotfiles_edit_index" -lt "$userland_dotfiles_edit_count" ]; do
    userland_dotfiles_edit_state=$(/usr/bin/plutil -extract "edits.$userland_dotfiles_edit_index.state" raw -o - "$userland_dotfiles_status" 2>/dev/null) || return 1
    case "$userland_dotfiles_edit_state" in applied | present) ;; *) return 1 ;; esac
    userland_dotfiles_edit_index=$((userland_dotfiles_edit_index + 1))
  done
}

userland_dotfiles_snapshot_target() {
  userland_dotfiles_snapshot_dir=$1
  userland_dotfiles_snapshot_path=$2
  userland_dotfiles_snapshot_manifest=$userland_dotfiles_snapshot_dir/manifest
  case "$userland_dotfiles_snapshot_path" in *"$(printf '\t')"* | *"
"*) return 1 ;; esac
  if awk -F '\t' -v path="$userland_dotfiles_snapshot_path" '$2 == path { found = 1 } END { exit !found }' "$userland_dotfiles_snapshot_manifest"; then
    return 0
  fi

  userland_dotfiles_snapshot_index=$(($(wc -l <"$userland_dotfiles_snapshot_manifest" | tr -d ' ') + 1))
  if [ -e "$userland_dotfiles_snapshot_path" ] || [ -L "$userland_dotfiles_snapshot_path" ]; then
    cp -pR "$userland_dotfiles_snapshot_path" "$userland_dotfiles_snapshot_dir/before/$userland_dotfiles_snapshot_index" || return 1
    userland_dotfiles_snapshot_presence=present
  else
    userland_dotfiles_snapshot_presence=absent
  fi
  printf '%s\t%s\t%s\n' \
    "$userland_dotfiles_snapshot_index" \
    "$userland_dotfiles_snapshot_path" \
    "$userland_dotfiles_snapshot_presence" >>"$userland_dotfiles_snapshot_manifest"
}

userland_dotfiles_begin() {
  userland_dotfiles_status=$1
  userland_dotfiles_recovery=$USERLAND_STATE_DIR/recovery
  mkdir -p "$USERLAND_CACHE_DIR" "$userland_dotfiles_recovery"
  chmod 700 "$userland_dotfiles_recovery"
  [ ! -e "$userland_dotfiles_recovery/active" ] || return 1

  userland_dotfiles_created_at=$(date +%s)
  userland_dotfiles_transaction_id=cutover-$userland_dotfiles_created_at-$$
  userland_dotfiles_transaction_dir=$userland_dotfiles_recovery/$userland_dotfiles_transaction_id
  [ ! -e "$userland_dotfiles_transaction_dir" ] || return 1
  mkdir -m 700 "$userland_dotfiles_transaction_dir" "$userland_dotfiles_transaction_dir/before"
  printf '%s\n' dotfiles-v1 >"$userland_dotfiles_transaction_dir/.userland-recovery"
  printf '%s\n' "$userland_dotfiles_created_at" >"$userland_dotfiles_transaction_dir/created-at"
  : >"$userland_dotfiles_transaction_dir/manifest"
  chmod 600 \
    "$userland_dotfiles_transaction_dir/.userland-recovery" \
    "$userland_dotfiles_transaction_dir/created-at" \
    "$userland_dotfiles_transaction_dir/manifest"
  cp "$userland_dotfiles_status" "$userland_dotfiles_transaction_dir/status-before.json"
  chmod 600 "$userland_dotfiles_transaction_dir/status-before.json"
  userland_dotfiles_write_state "$userland_dotfiles_transaction_dir" preparing

  userland_dotfiles_target_count=$(userland_dotfiles_array_count files "$userland_dotfiles_status") || return 1
  userland_dotfiles_target_index=0
  while [ "$userland_dotfiles_target_index" -lt "$userland_dotfiles_target_count" ]; do
    userland_dotfiles_declared_target=$(/usr/bin/plutil -extract "files.$userland_dotfiles_target_index.target" raw -o - "$userland_dotfiles_status" 2>/dev/null) || return 1
    userland_dotfiles_expand_target "$userland_dotfiles_declared_target" || return 1
    userland_dotfiles_snapshot_target "$userland_dotfiles_transaction_dir" "$userland_dotfiles_target" || return 1
    userland_dotfiles_target_index=$((userland_dotfiles_target_index + 1))
  done
  for userland_dotfiles_retired_target in \
    "$USERLAND_HOME/.codex/AGENTS.md" \
    "$USERLAND_HOME/.config/opencode/AGENTS.md"; do
    if [ -e "$userland_dotfiles_retired_target" ] || [ -L "$userland_dotfiles_retired_target" ]; then
      userland_dotfiles_snapshot_target "$userland_dotfiles_transaction_dir" "$userland_dotfiles_retired_target" || return 1
    fi
  done

  userland_dotfiles_active_tmp=$userland_dotfiles_recovery/.active.$$
  printf '%s\n' "$userland_dotfiles_transaction_id" >"$userland_dotfiles_active_tmp"
  chmod 600 "$userland_dotfiles_active_tmp"
  mv "$userland_dotfiles_active_tmp" "$userland_dotfiles_recovery/active"
  userland_dotfiles_write_state "$userland_dotfiles_transaction_dir" applying
}

userland_dotfiles_rollback_dir() {
  userland_dotfiles_rollback_dir=$1
  [ -f "$userland_dotfiles_rollback_dir/.userland-recovery" ] &&
    [ "$(cat "$userland_dotfiles_rollback_dir/.userland-recovery")" = dotfiles-v1 ] || return 1
  userland_dotfiles_write_state "$userland_dotfiles_rollback_dir" rolling-back
  userland_dotfiles_rollback_run=$userland_dotfiles_rollback_dir/after-$(date +%s)-$$
  mkdir -m 700 "$userland_dotfiles_rollback_run" || return 1
  userland_dotfiles_rollback_manifest=$userland_dotfiles_rollback_dir/manifest.reverse.$$
  awk '{ row[NR] = $0 } END { for (i = NR; i > 0; i--) print row[i] }' \
    "$userland_dotfiles_rollback_dir/manifest" >"$userland_dotfiles_rollback_manifest"
  userland_dotfiles_rollback_failed=0
  while IFS="$(printf '\t')" read -r userland_dotfiles_rollback_index userland_dotfiles_rollback_path userland_dotfiles_rollback_presence; do
    case "$userland_dotfiles_rollback_path" in
      "$USERLAND_HOME"/*) ;;
      *)
        userland_dotfiles_rollback_failed=1
        break
        ;;
    esac
    if [ -e "$userland_dotfiles_rollback_path" ] || [ -L "$userland_dotfiles_rollback_path" ]; then
      mv "$userland_dotfiles_rollback_path" "$userland_dotfiles_rollback_run/$userland_dotfiles_rollback_index" || {
        userland_dotfiles_rollback_failed=1
        break
      }
    fi
    if [ "$userland_dotfiles_rollback_presence" = present ]; then
      mkdir -p "${userland_dotfiles_rollback_path%/*}" || {
        userland_dotfiles_rollback_failed=1
        break
      }
      cp -pR \
        "$userland_dotfiles_rollback_dir/before/$userland_dotfiles_rollback_index" \
        "$userland_dotfiles_rollback_path" || {
        userland_dotfiles_rollback_failed=1
        break
      }
    fi
  done <"$userland_dotfiles_rollback_manifest"
  rm -f "$userland_dotfiles_rollback_manifest"

  if [ "$userland_dotfiles_rollback_failed" -ne 0 ]; then
    userland_dotfiles_write_state "$userland_dotfiles_rollback_dir" rollback-failed
    userland_log error "Managed-file rollback needs recovery at $userland_dotfiles_rollback_dir"
    return 1
  fi
  userland_dotfiles_write_state "$userland_dotfiles_rollback_dir" rolled-back
  userland_dotfiles_clear_active "${userland_dotfiles_rollback_dir##*/}"
  userland_log changed "Restored the managed-file state from before this sync"
}

userland_dotfiles_recover() {
  userland_dotfiles_recovery=$USERLAND_STATE_DIR/recovery
  userland_dotfiles_active=$userland_dotfiles_recovery/active
  [ -f "$userland_dotfiles_active" ] || return 0
  userland_dotfiles_recovery_id=$(cat "$userland_dotfiles_active" 2>/dev/null) || return 1
  case "$userland_dotfiles_recovery_id" in '' | *[!A-Za-z0-9._-]*) return 1 ;; esac
  userland_dotfiles_recovery_dir=$userland_dotfiles_recovery/$userland_dotfiles_recovery_id
  [ -d "$userland_dotfiles_recovery_dir" ] && [ ! -L "$userland_dotfiles_recovery_dir" ] || return 1
  userland_dotfiles_recovery_state=$(cat "$userland_dotfiles_recovery_dir/state" 2>/dev/null) || return 1
  case "$userland_dotfiles_recovery_state" in
    committed | rolled-back)
      userland_dotfiles_clear_active "$userland_dotfiles_recovery_id"
      ;;
    preparing | applying | rolling-back | rollback-failed)
      userland_dotfiles_rollback_dir "$userland_dotfiles_recovery_dir"
      ;;
    *) return 1 ;;
  esac
}

userland_dotfiles_prune_recovery() {
  userland_dotfiles_recovery=$USERLAND_STATE_DIR/recovery
  [ -d "$userland_dotfiles_recovery" ] || return 0
  userland_dotfiles_now=$(date +%s)
  for userland_dotfiles_candidate in "$userland_dotfiles_recovery"/cutover-*; do
    [ -d "$userland_dotfiles_candidate" ] && [ ! -L "$userland_dotfiles_candidate" ] || continue
    [ -f "$userland_dotfiles_candidate/.userland-recovery" ] &&
      [ "$(cat "$userland_dotfiles_candidate/.userland-recovery" 2>/dev/null)" = dotfiles-v1 ] || continue
    [ "$(cat "$userland_dotfiles_candidate/state" 2>/dev/null)" = committed ] || continue
    userland_dotfiles_created_at=$(cat "$userland_dotfiles_candidate/created-at" 2>/dev/null) || continue
    case "$userland_dotfiles_created_at" in '' | *[!0-9]*) continue ;; esac
    [ "$((userland_dotfiles_now - userland_dotfiles_created_at))" -ge 86400 ] || continue
    rm -rf "$userland_dotfiles_candidate"
  done
}

userland_dotfiles_recovery_window_open() {
  userland_dotfiles_recovery=$USERLAND_STATE_DIR/recovery
  [ -d "$userland_dotfiles_recovery" ] || return 1
  userland_dotfiles_now=$(date +%s)
  for userland_dotfiles_candidate in "$userland_dotfiles_recovery"/cutover-*; do
    [ -d "$userland_dotfiles_candidate" ] && [ ! -L "$userland_dotfiles_candidate" ] || continue
    [ "$(cat "$userland_dotfiles_candidate/state" 2>/dev/null)" = committed ] || continue
    userland_dotfiles_created_at=$(cat "$userland_dotfiles_candidate/created-at" 2>/dev/null) || continue
    case "$userland_dotfiles_created_at" in '' | *[!0-9]*) continue ;; esac
    [ "$((userland_dotfiles_now - userland_dotfiles_created_at))" -lt 86400 ] && return 0
  done
  return 1
}

userland_dotfiles_abort() {
  userland_dotfiles_abort_code=$1
  userland_dotfiles_rollback_dir "$userland_dotfiles_transaction_dir" || :
  trap - HUP INT TERM
  exit "$userland_dotfiles_abort_code"
}

userland_dotfiles_apply() {
  userland_dotfiles_recover || return 1
  userland_dotfiles_prune_recovery || return 1
  mkdir -p "$USERLAND_CACHE_DIR"
  userland_dotfiles_status=$(mktemp "$USERLAND_CACHE_DIR/dotfiles-status.XXXXXX")
  if ! "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status --json >"$userland_dotfiles_status"; then
    rm -f "$userland_dotfiles_status"
    return 1
  fi
  if userland_dotfiles_status_is_applied "$userland_dotfiles_status"; then
    rm -f "$userland_dotfiles_status"
    return 0
  fi
  if ! userland_dotfiles_begin "$userland_dotfiles_status"; then
    rm -f "$userland_dotfiles_status"
    return 1
  fi
  rm -f "$userland_dotfiles_status"

  trap 'userland_dotfiles_abort 129' HUP
  trap 'userland_dotfiles_abort 130' INT
  trap 'userland_dotfiles_abort 143' TERM
  userland_dotfiles_apply_code=0
  userland_prepare_legacy_dotfiles || userland_dotfiles_apply_code=$?
  if [ "$userland_dotfiles_apply_code" -eq 0 ]; then
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles apply --yes || userland_dotfiles_apply_code=$?
  fi
  if [ "$userland_dotfiles_apply_code" -eq 0 ]; then
    userland_dotfiles_verify=$(mktemp "$USERLAND_CACHE_DIR/dotfiles-verify.XXXXXX")
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status --json >"$userland_dotfiles_verify" || userland_dotfiles_apply_code=$?
    if [ "$userland_dotfiles_apply_code" -eq 0 ] && ! userland_dotfiles_status_is_applied "$userland_dotfiles_verify"; then
      userland_dotfiles_apply_code=1
    fi
    rm -f "$userland_dotfiles_verify"
  fi
  if [ "$userland_dotfiles_apply_code" -ne 0 ]; then
    userland_dotfiles_rollback_dir "$userland_dotfiles_transaction_dir" || return 1
    trap - HUP INT TERM
    return "$userland_dotfiles_apply_code"
  fi

  userland_dotfiles_write_state "$userland_dotfiles_transaction_dir" committed
  userland_dotfiles_clear_active "$userland_dotfiles_transaction_id"
  trap - HUP INT TERM
  return 0
}

userland_link_target() {
  /usr/bin/stat -f '%Y' "$1" 2>/dev/null || /usr/bin/stat -c '%N' "$1" 2>/dev/null
}

userland_is_owned_legacy_source() {
  case "$1" in
    */workspace/cfg/* | "$USERLAND_ROOT"/cfg/* | "$USERLAND_ROOT"/agents/* | "$USERLAND_DATA_DIR"/repo/cfg/* | "$USERLAND_DATA_DIR"/repo/config/* | "$USERLAND_DATA_DIR"/repo/agents/* | "$USERLAND_DATA_DIR"/releases/*/cfg/* | "$USERLAND_DATA_DIR"/releases/*/config/* | "$USERLAND_DATA_DIR"/releases/*/agents/*) return 0 ;;
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
    [ "$userland_child_name" != .git ] || continue
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

  case "$userland_legacy_source" in
    */cfg/*) userland_legacy_suffix=${userland_legacy_source#*/cfg/} ;;
    "$USERLAND_DATA_DIR"/repo/config/*) userland_legacy_suffix=${userland_legacy_source#"$USERLAND_DATA_DIR"/repo/config/} ;;
    "$USERLAND_DATA_DIR"/releases/*/config/*) userland_legacy_suffix=${userland_legacy_source#*/config/} ;;
    */agents/*) userland_legacy_suffix=agents/${userland_legacy_source#*/agents/} ;;
    *) return 0 ;;
  esac

  case "$userland_legacy_suffix" in
    home/agents/AGENT.md | home/agents/opencode/AGENTS.md)
      rm "$userland_legacy_link"
      userland_log changed "removed retired agent instructions at $userland_legacy_link"
      return 0
      ;;
    home/agents/skills)
      userland_replacement_source=$USERLAND_ROOT/config/agents/skills
      ;;
    home/claude/settings.json)
      userland_replacement_source=$USERLAND_ROOT/config/agents/claude/settings.json
      ;;
    agents/skills)
      userland_replacement_source=$USERLAND_ROOT/config/agents/skills
      ;;
    agents/claude/settings.json)
      userland_replacement_source=$USERLAND_ROOT/config/agents/claude/settings.json
      ;;
    *)
      userland_replacement_source=$USERLAND_ROOT/config/$userland_legacy_suffix
      ;;
  esac
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
    "$USERLAND_HOME"/.config/*; do
    if [ -L "$userland_legacy_link" ]; then
      userland_migrate_legacy_link "$userland_legacy_link"
    elif [ -d "$userland_legacy_link" ]; then
      find "$userland_legacy_link" -type l -print 2>/dev/null |
        while IFS= read -r userland_nested_legacy_link; do
          userland_migrate_legacy_link "$userland_nested_legacy_link"
        done
    fi
  done
}

userland_legacy_checkout_has_links() {
  userland_legacy_checkout=$USERLAND_DATA_DIR/repo
  userland_legacy_link_file=$(mktemp "$USERLAND_CACHE_DIR/legacy-checkout-links.XXXXXX")
  for userland_legacy_root in \
    "$USERLAND_HOME"/.zshrc \
    "$USERLAND_HOME"/.zshenv \
    "$USERLAND_HOME"/.hushlogin \
    "$USERLAND_HOME"/.ssh/config \
    "$USERLAND_HOME"/.agents/skills \
    "$USERLAND_HOME"/.claude/skills \
    "$USERLAND_HOME"/.claude/settings.json \
    "$USERLAND_HOME"/.codex/AGENTS.md \
    "$USERLAND_HOME"/.config/*; do
    find "$userland_legacy_root" -type l -print 2>/dev/null >>"$userland_legacy_link_file" || :
  done

  while IFS= read -r userland_legacy_link; do
    userland_legacy_source=$(userland_link_target "$userland_legacy_link")
    case "$userland_legacy_source" in
      "$userland_legacy_checkout"/*)
        rm -f "$userland_legacy_link_file"
        return 0
        ;;
    esac
  done <"$userland_legacy_link_file"
  rm -f "$userland_legacy_link_file"
  return 1
}

userland_trash_legacy_checkout() {
  userland_legacy_checkout=$USERLAND_DATA_DIR/repo
  [ -e "$userland_legacy_checkout" ] || [ -L "$userland_legacy_checkout" ] || return 0
  if [ -L "$userland_legacy_checkout" ] || [ ! -d "$userland_legacy_checkout/.git" ] ||
    [ -L "$userland_legacy_checkout/.git" ]; then
    userland_log attention "preserved unrecognized legacy checkout at $userland_legacy_checkout"
    return 2
  fi
  if GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    git -C "$userland_legacy_checkout" config --local --no-includes --get core.worktree >/dev/null 2>&1; then
    userland_log attention "preserved external-worktree checkout at $userland_legacy_checkout"
    return 2
  fi
  userland_legacy_origin=$(GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
    git -C "$userland_legacy_checkout" config --local --no-includes --get remote.origin.url 2>/dev/null) || {
    userland_log attention "preserved legacy checkout without an origin at $userland_legacy_checkout"
    return 2
  }
  if [ "$userland_legacy_origin" != https://github.com/giacomoguidotto/userland.git ]; then
    userland_log attention "preserved legacy checkout with an unexpected origin at $userland_legacy_checkout"
    return 2
  fi
  userland_legacy_status=$(GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_OPTIONAL_LOCKS=0 \
    git -c core.fsmonitor=false -c core.hooksPath=/dev/null \
    -C "$userland_legacy_checkout" status --porcelain=v1 --untracked-files=all --ignore-submodules=none) || {
    userland_log attention "could not inspect legacy checkout at $userland_legacy_checkout"
    return 2
  }
  if [ -n "$userland_legacy_status" ] || userland_legacy_checkout_has_links; then
    userland_log attention "preserved legacy checkout with local state or active links at $userland_legacy_checkout"
    return 2
  fi
  userland_trash_dir=${USERLAND_TRASH_DIR:-$USERLAND_HOME/.Trash}
  if [ -L "$userland_trash_dir" ] || { [ -e "$userland_trash_dir" ] && [ ! -d "$userland_trash_dir" ]; }; then
    userland_log attention "preserved legacy checkout because Trash is unavailable at $userland_trash_dir"
    return 2
  fi
  mkdir -p "$userland_trash_dir" || {
    userland_log attention "preserved legacy checkout because Trash could not be created at $userland_trash_dir"
    return 2
  }
  userland_trash_bundle=$(mktemp -d "$userland_trash_dir/userland-legacy.XXXXXX") || {
    userland_log attention "preserved legacy checkout because a Trash destination could not be reserved"
    return 2
  }
  if ! mv "$userland_legacy_checkout" "$userland_trash_bundle/checkout"; then
    rmdir "$userland_trash_bundle" 2>/dev/null || :
    userland_log attention "preserved legacy checkout because it could not be moved to Trash"
    return 2
  fi
  userland_log changed "moved legacy userland checkout to Trash at $userland_trash_bundle/checkout"
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
    "$USERLAND_HOME"/.config/*; do
    if [ -L "$userland_legacy_link" ]; then
      userland_legacy_source=$(userland_link_target "$userland_legacy_link")
      if userland_is_owned_legacy_source "$userland_legacy_source"; then
        if command -v userland_plan_add >/dev/null 2>&1; then
          userland_plan_add cleanup release automatic userland \
            "$userland_legacy_link" \
            "release legacy workspace link" \
            "legacy-link:$userland_legacy_source"
        else
          userland_log change "release legacy workspace link $userland_legacy_link"
        fi
        userland_legacy_count=$((userland_legacy_count + 1))
      fi
    elif [ -d "$userland_legacy_link" ]; then
      userland_nested_legacy_file=$(mktemp "$USERLAND_CACHE_DIR/legacy-links.XXXXXX")
      find "$userland_legacy_link" -type l -print 2>/dev/null >"$userland_nested_legacy_file"
      userland_nested_legacy_count=0
      while IFS= read -r userland_nested_legacy_link; do
        userland_nested_legacy_source=$(userland_link_target "$userland_nested_legacy_link")
        userland_is_owned_legacy_source "$userland_nested_legacy_source" || continue
        if command -v userland_plan_add >/dev/null 2>&1; then
          userland_plan_add cleanup release automatic userland \
            "$userland_nested_legacy_link" \
            "release legacy workspace link" \
            "legacy-link:$userland_nested_legacy_source"
        fi
        userland_nested_legacy_count=$((userland_nested_legacy_count + 1))
      done <"$userland_nested_legacy_file"
      rm -f "$userland_nested_legacy_file"
      if [ "$userland_nested_legacy_count" -gt 0 ]; then
        if ! command -v userland_plan_add >/dev/null 2>&1; then
          userland_log change "release $userland_nested_legacy_count legacy links under $userland_legacy_link"
        fi
        userland_legacy_count=$((userland_legacy_count + userland_nested_legacy_count))
      fi
    fi
  done
  userland_legacy_checkout=$USERLAND_DATA_DIR/repo
  if [ -L "$userland_legacy_checkout" ] || { [ -e "$userland_legacy_checkout" ] && [ ! -d "$userland_legacy_checkout/.git" ]; }; then
    userland_plan_add cleanup review blocked userland \
      "$userland_legacy_checkout" \
      "unrecognized legacy checkout requires review" \
      "legacy-checkout:$userland_legacy_checkout"
  elif [ -d "$userland_legacy_checkout/.git" ]; then
    userland_plan_add cleanup release automatic userland \
      "$userland_legacy_checkout" \
      "move to Trash after managed links migrate and doctor passes" \
      "legacy-checkout:$userland_legacy_checkout"
  fi
  [ "$userland_legacy_count" -ne 0 ] || userland_log current "no legacy workspace links need migration"
}
