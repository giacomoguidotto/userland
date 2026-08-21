#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"

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
    "$USERLAND_HOME"/.config/opencode/AGENTS.md \
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
    "$USERLAND_HOME"/.config/opencode/AGENTS.md \
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
    "$USERLAND_HOME"/.config/opencode/AGENTS.md \
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
