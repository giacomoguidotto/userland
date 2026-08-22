#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_file_mtime() {
  if stat -f '%m' "$1" >/dev/null 2>&1; then
    stat -f '%m' "$1"
  else
    stat -c '%Y' "$1"
  fi
}

userland_repository_snapshot_is_fresh() {
  userland_snapshot=$USERLAND_CACHE_DIR/repositories.tsv
  userland_snapshot_meta=$USERLAND_CACHE_DIR/repositories.meta
  userland_expected_meta="v2 ${USERLAND_REPO_ROOTS:-$USERLAND_HOME/dev/life:$USERLAND_HOME/dev/uni}"
  [ -f "$userland_snapshot" ] || return 1
  [ -f "$userland_snapshot_meta" ] || return 1
  [ "$(sed -n '1p' "$userland_snapshot_meta")" = "$userland_expected_meta" ] || return 1
  userland_snapshot_age=$(($(date +%s) - $(userland_file_mtime "$userland_snapshot")))
  [ "$userland_snapshot_age" -lt "$USERLAND_REPOSITORY_TTL_SECONDS" ]
}

userland_refresh_repository_snapshot() {
  userland_mkdirs
  userland_snapshot=$USERLAND_CACHE_DIR/repositories.tsv
  userland_snapshot_meta=$USERLAND_CACHE_DIR/repositories.meta

  if userland_repository_snapshot_is_fresh; then
    userland_log current "repository snapshot is younger than 24 hours"
    return 0
  fi

  userland_scan_roots=${USERLAND_REPO_ROOTS:-$USERLAND_HOME/dev/life:$USERLAND_HOME/dev/uni}
  userland_snapshot_tmp=$userland_snapshot.tmp.$$
  : >"$userland_snapshot_tmp"

  userland_old_ifs=$IFS
  IFS=:
  for userland_scan_root in $userland_scan_roots; do
    [ -d "$userland_scan_root" ] || continue
    if command -v fd >/dev/null 2>&1; then
      fd -H '^\.git$' "$userland_scan_root" \
        --exclude node_modules --exclude .next --exclude .venv \
        --exclude target --exclude vendor --type d --type f 2>/dev/null
    else
      find "$userland_scan_root" \
        \( -name node_modules -o -name .next -o -name .venv -o -name target -o -name vendor \) -prune \
        -o -name .git -print 2>/dev/null
    fi
  done | while IFS= read -r userland_git_marker; do
    userland_discovered_repo=$(dirname "$userland_git_marker")
    if command -v git >/dev/null 2>&1; then
      userland_superproject=$(git -C "$userland_discovered_repo" rev-parse --show-superproject-working-tree 2>/dev/null || true)
      [ -z "$userland_superproject" ] || continue
    fi
    printf '%s\n' "$userland_discovered_repo"
  done | LC_ALL=C sort -u >"$userland_snapshot_tmp"
  IFS=$userland_old_ifs

  mv "$userland_snapshot_tmp" "$userland_snapshot"
  printf '%s\n' "v2 $userland_scan_roots" >"$userland_snapshot_meta.tmp.$$"
  mv "$userland_snapshot_meta.tmp.$$" "$userland_snapshot_meta"
  userland_repo_count=$(wc -l <"$userland_snapshot" | tr -d ' ')
  userland_log changed "refreshed the 24-hour repository snapshot ($userland_repo_count repositories)"
}

userland_repository_refresh_checkout() {
  [ -d "$USERLAND_ROOT/.git" ] || return 0
  command -v git >/dev/null 2>&1 || {
    userland_log warning "Git is unavailable; skipped the userland checkout refresh"
    return 0
  }

  if [ -n "$(git -C "$USERLAND_ROOT" status --porcelain)" ]; then
    userland_log warning "userland has local changes; skipped checkout refresh"
    return 0
  fi

  userland_branch=$(git -C "$USERLAND_ROOT" branch --show-current)
  if [ "$userland_branch" != "main" ]; then
    userland_log current "checkout is on $userland_branch; automatic refresh is limited to main"
    return 0
  fi

  if ! git -C "$USERLAND_ROOT" fetch --quiet origin main; then
    userland_log warning "could not reach origin; continuing with the local checkout"
    return 0
  fi

  userland_head=$(git -C "$USERLAND_ROOT" rev-parse HEAD)
  userland_remote_head=$(git -C "$USERLAND_ROOT" rev-parse origin/main)
  if [ "$userland_head" = "$userland_remote_head" ]; then
    git -C "$USERLAND_ROOT" submodule sync --quiet --recursive
    git -C "$USERLAND_ROOT" submodule update --quiet --init --recursive
    userland_log current "userland checkout is current"
    return 0
  fi

  if ! git -C "$USERLAND_ROOT" merge-base --is-ancestor HEAD origin/main; then
    userland_log warning "local and remote main diverged; refused to update"
    return 0
  fi

  git -C "$USERLAND_ROOT" merge --ff-only --quiet origin/main
  git -C "$USERLAND_ROOT" submodule sync --quiet --recursive
  git -C "$USERLAND_ROOT" submodule update --quiet --init --recursive
  userland_log changed "advanced userland to origin/main"
  return 10
}
