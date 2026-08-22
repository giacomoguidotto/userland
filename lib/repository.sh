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
  USERLAND_REPOSITORY_REFRESH_NOTICE=
  export USERLAND_REPOSITORY_REFRESH_NOTICE
  [ -e "$USERLAND_ROOT/.git" ] || return 0
  command -v git >/dev/null 2>&1 || {
    USERLAND_REPOSITORY_REFRESH_NOTICE="Git is unavailable; skipped the userland checkout refresh"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  }

  userland_refresh_log=$USERLAND_CACHE_DIR/repository-refresh.log
  : >"$userland_refresh_log"
  chmod 600 "$userland_refresh_log"

  if ! userland_checkout_status=$(git -C "$USERLAND_ROOT" status --porcelain 2>>"$userland_refresh_log"); then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Could not inspect the userland checkout; continuing with the local version"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi
  if [ -n "$userland_checkout_status" ]; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="userland has local changes; skipped checkout refresh"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi

  if ! userland_branch=$(git -C "$USERLAND_ROOT" branch --show-current 2>>"$userland_refresh_log"); then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Could not inspect the userland branch; continuing with the local version"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi
  if [ "$userland_branch" != "main" ]; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="checkout is on $userland_branch; automatic refresh is limited to main"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi

  if ! git -C "$USERLAND_ROOT" fetch --quiet --tags origin main >>"$userland_refresh_log" 2>&1; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Could not reach origin; continuing with the local checkout"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi

  if ! userland_head=$(git -C "$USERLAND_ROOT" rev-parse HEAD 2>>"$userland_refresh_log") ||
    ! userland_remote_head=$(git -C "$USERLAND_ROOT" rev-parse origin/main 2>>"$userland_refresh_log"); then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Could not compare the userland checkout; continuing with the local version"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi
  if [ "$userland_head" = "$userland_remote_head" ]; then
    if ! git -C "$USERLAND_ROOT" submodule sync --quiet --recursive >>"$userland_refresh_log" 2>&1 ||
      ! git -C "$USERLAND_ROOT" submodule update --quiet --init --recursive >>"$userland_refresh_log" 2>&1; then
      USERLAND_REPOSITORY_REFRESH_NOTICE="Could not refresh userland submodules; continuing with their local versions"
      export USERLAND_REPOSITORY_REFRESH_NOTICE
      return 0
    fi
    rm -f "$userland_refresh_log"
    return 0
  fi

  if ! git -C "$USERLAND_ROOT" merge-base --is-ancestor HEAD origin/main >>"$userland_refresh_log" 2>&1; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="local and remote main diverged; refused to update"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi

  if ! git -C "$USERLAND_ROOT" merge --ff-only --quiet origin/main >>"$userland_refresh_log" 2>&1; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Could not fast-forward userland; continuing with the local version"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
    return 0
  fi
  if ! git -C "$USERLAND_ROOT" submodule sync --quiet --recursive >>"$userland_refresh_log" 2>&1 ||
    ! git -C "$USERLAND_ROOT" submodule update --quiet --init --recursive >>"$userland_refresh_log" 2>&1; then
    USERLAND_REPOSITORY_REFRESH_NOTICE="Updated userland, but its submodules need attention"
    export USERLAND_REPOSITORY_REFRESH_NOTICE
  else
    rm -f "$userland_refresh_log"
  fi
  return 10
}
