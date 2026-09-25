#!/bin/sh
set -eu

tag='@USERLAND_TAG@'
commit='@USERLAND_COMMIT@'
archive_sha256_darwin_arm64='@USERLAND_ARCHIVE_SHA256_DARWIN_ARM64@'
archive_sha256_linux_arm64='@USERLAND_ARCHIVE_SHA256_LINUX_ARM64@'
archive_sha256_linux_x64='@USERLAND_ARCHIVE_SHA256_LINUX_X64@'
repository='https://github.com/giacomoguidotto/userland.git'
platform_os=$(uname -s 2>/dev/null || printf unknown)
platform_arch=$(uname -m 2>/dev/null || printf unknown)
if [ -n "${USERLAND_PLATFORM:-}" ]; then
  case "$USERLAND_PLATFORM" in
    darwin-arm64)
      platform_os=Darwin
      platform_arch=arm64
      ;;
    linux-arm64)
      platform_os=Linux
      platform_arch=aarch64
      ;;
    linux-x64)
      platform_os=Linux
      platform_arch=x86_64
      ;;
    *)
      printf 'userland: invalid USERLAND_PLATFORM %s\n' "$USERLAND_PLATFORM" >&2
      exit 1
      ;;
  esac
fi
case "$platform_os:$platform_arch" in
  Darwin:arm64 | Darwin:aarch64)
    platform=darwin-arm64
    archive="userland-$tag.tar.gz"
    archive_sha256=$archive_sha256_darwin_arm64
    ;;
  Linux:arm64 | Linux:aarch64 | Linux:armv8l)
    platform=linux-arm64
    archive="userland-$tag-linux-arm64.tar.gz"
    archive_sha256=$archive_sha256_linux_arm64
    ;;
  Linux:x86_64 | Linux:amd64)
    platform=linux-x64
    archive="userland-$tag-linux-x64.tar.gz"
    archive_sha256=$archive_sha256_linux_x64
    ;;
  *)
    die() {
      printf 'userland: unsupported platform %s/%s\n' "$platform_os" "$platform_arch" >&2
      exit 1
    }
    die
    ;;
esac
release_url="https://github.com/giacomoguidotto/userland/releases/download/$tag/$archive"
data_dir=${USERLAND_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/userland}
release_key=$tag
[ "$platform" = darwin-arm64 ] || release_key="$tag-$platform"
release_dir="$data_dir/releases/$release_key"
repo_dir=$HOME/.userland
legacy_repo_dir="$data_dir/repo"
bin_dir=${USERLAND_BIN_DIR:-$HOME/.local/bin}
: "${USERLAND_ORIGINAL_PATH:=${PATH:-}}"
PATH="$bin_dir:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export PATH USERLAND_ORIGINAL_PATH

die() {
  printf 'userland: %s\n' "$*" >&2
  exit 1
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

replace_link_atomically() {
  replacement_link=$1
  destination_link=$2

  # BSD mv needs -h to replace a symlink to a directory instead of following
  # it. GNU mv expresses the same rule with -T.
  if /bin/mv -fh "$replacement_link" "$destination_link" 2>/dev/null; then
    return 0
  fi
  if /bin/mv -fT "$replacement_link" "$destination_link" 2>/dev/null; then
    return 0
  fi
  return 1
}

cleanup_stale_current_links() {
  for stale_link in "$data_dir"/releases/*/.current.new.*; do
    [ -L "$stale_link" ] || continue
    stale_target=$(readlink "$stale_link")
    case "$stale_target" in
      "$data_dir"/releases/*) rm "$stale_link" ;;
    esac
  done
}

install_command_link() {
  command_target=$1
  command_path="$bin_dir/userland"

  if [ -L "$command_path" ]; then
    existing_target=$(readlink "$command_path")
    case "$existing_target" in
      "$data_dir"/releases/*/bin/userland | "$repo_dir"/bin/userland | "$legacy_repo_dir"/bin/userland) ;;
      *) die "$command_path is an unmanaged symlink to $existing_target" ;;
    esac
  elif [ -e "$command_path" ]; then
    die "$command_path exists and is not a userland-managed symlink"
  fi

  temporary_link="$bin_dir/.userland.new.$$"
  [ ! -e "$temporary_link" ] && [ ! -L "$temporary_link" ] ||
    die "temporary command path already exists: $temporary_link"
  ln -s "$command_target" "$temporary_link"
  replace_link_atomically "$temporary_link" "$command_path" ||
    die "could not replace managed link: $command_path"
}

install_current_release_link() {
  current_path="$data_dir/current"
  if [ -L "$current_path" ]; then
    existing_target=$(readlink "$current_path")
    case "$existing_target" in
      "$data_dir"/releases/*) ;;
      *) die "$current_path is an unmanaged symlink to $existing_target" ;;
    esac
  elif [ -e "$current_path" ]; then
    die "$current_path exists and is not a userland-managed symlink"
  fi

  temporary_link="$data_dir/.current.new.$$"
  [ ! -e "$temporary_link" ] && [ ! -L "$temporary_link" ] ||
    die "temporary release path already exists: $temporary_link"
  ln -s "$release_dir" "$temporary_link"
  replace_link_atomically "$temporary_link" "$current_path" ||
    die "could not replace managed link: $current_path"
}

bootstrap_git() {
  GIT_CONFIG_GLOBAL=/dev/null \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_OPTIONAL_LOCKS=0 \
    GIT_TERMINAL_PROMPT=0 \
    git \
    -c core.fsmonitor=false \
    -c core.hooksPath=/dev/null \
    "$@"
}

checkout_git() {
  checkout_path=$1
  shift
  bootstrap_git \
    -C "$checkout_path" \
    "$@"
}

migrate_obsolete_nvim_submodule() {
  checkout_path=$1
  legacy_relative=config/xdg/nvim
  current_relative=cfg/xdg/nvim
  legacy_path="$checkout_path/$legacy_relative"

  [ -d "$legacy_path" ] && [ ! -L "$legacy_path" ] || return 0
  [ -e "$legacy_path/.git" ] && [ ! -L "$legacy_path/.git" ] || return 0

  legacy_parent_status=$(checkout_git "$checkout_path" status \
    --porcelain=v1 --untracked-files=all --ignore-submodules=none -- "$legacy_relative") ||
    return 0
  [ "$legacy_parent_status" = "?? $legacy_relative/" ] || return 0

  expected_commit=$(checkout_git "$checkout_path" rev-parse "HEAD:$current_relative" 2>/dev/null) ||
    return 0
  actual_commit=$(bootstrap_git -C "$legacy_path" rev-parse HEAD 2>/dev/null) ||
    return 0
  [ "$actual_commit" = "$expected_commit" ] || return 0

  if bootstrap_git -C "$legacy_path" config --local --no-includes --get core.worktree >/dev/null 2>&1; then
    return 0
  fi
  legacy_origin=$(bootstrap_git -C "$legacy_path" config --local --no-includes --get remote.origin.url 2>/dev/null) ||
    return 0
  [ "$legacy_origin" = 'https://github.com/GiacomoGuidotto/kickstart.nvim.git' ] || return 0
  legacy_checkout_status=$(bootstrap_git -C "$legacy_path" status \
    --porcelain=v1 --untracked-files=all --ignore-submodules=none) ||
    return 0
  [ -z "$legacy_checkout_status" ] || return 0

  trash_root="$HOME/.Trash"
  if [ -e "$trash_root" ] || [ -L "$trash_root" ]; then
    [ -d "$trash_root" ] && [ ! -L "$trash_root" ] || return 0
  else
    mkdir -p "$trash_root" || return 0
  fi
  migration_backup=$(mktemp -d "$trash_root/userland-migration.XXXXXX") || return 0
  if ! mv "$legacy_path" "$migration_backup/config-xdg-nvim"; then
    rmdir "$migration_backup" 2>/dev/null || :
    return 0
  fi
  printf 'userland: moved obsolete config/xdg/nvim checkout to Trash at %s\n' \
    "$migration_backup/config-xdg-nvim" >"$control_dir/migration-notice"
}

report_migration_notice() {
  migration_notice="$control_dir/migration-notice"
  [ -f "$migration_notice" ] || return 0
  cat "$migration_notice"
  rm "$migration_notice"
}

# This runs before sync, including when stdin contains the downloaded script.
# Keep diff output and answers on the terminal, out of the transaction log.
review_checkout_diff() (
  review_checkout=$1
  umask 077
  review_dir=$(mktemp -d "${TMPDIR:-/tmp}/userland-diff.XXXXXX") || exit 1
  trap 'rm -rf "$review_dir"' EXIT
  trap 'exit 130' HUP INT TERM
  checkout_git "$review_checkout" --no-pager diff --no-color --no-ext-diff --no-textconv --cached -- >"$review_dir/staged" 2>&9 || exit 1
  checkout_git "$review_checkout" --no-pager diff --no-color --no-ext-diff --no-textconv -- >"$review_dir/unstaged" 2>&9 || exit 1
  if [ ! -s "$review_dir/staged" ] && [ ! -s "$review_dir/unstaged" ]; then
    printf ' ·  No tracked file patches to review.\n' >&9
    exit 0
  fi
  {
    if [ -s "$review_dir/staged" ]; then
      printf 'Staged changes\n\n'
      cat "$review_dir/staged"
    fi
    if [ -s "$review_dir/unstaged" ]; then
      printf '\nUnstaged changes\n\n'
      cat "$review_dir/unstaged"
    fi
  } >"$review_dir/patch"

  # Invoke Delta directly so bootstrap_git can keep ignoring global Git
  # settings while Delta still reads the user's theme and layout. Its output
  # must be the terminal even though the installer itself arrived over a pipe.
  cd "$review_checkout" || exit 1
  if command -v delta >/dev/null 2>&1; then
    printf ' ·  Reviewing the full patch in Delta. Press q to return to the recovery choices.\n' >&9
    if delta --paging always --pager 'less -R' --line-numbers <"$review_dir/patch" >&9 2>&9; then
      exit 0
    fi
    printf ' ·  Delta could not display the patch; showing the unified diff.\n' >&9
  fi
  if command -v less >/dev/null 2>&1; then
    printf ' ·  Reviewing the full patch. Press q to return to the recovery choices.\n' >&9
    less -R <"$review_dir/patch" >&9 2>&9 && exit 0
  fi
  cat "$review_dir/patch" >&9
)

recover_checkout_changes() (
  recovery_checkout=$1
  checkout_status=$(checkout_git "$recovery_checkout" status --porcelain=v1 --untracked-files=all --ignore-submodules=none) ||
    die "could not read checkout status"
  [ -n "$checkout_status" ] || exit 0
  if [ "${USERLAND_NO_TTY:-0}" = 1 ] || ! (exec 9<>/dev/tty) 2>/dev/null; then
    printf 'userland: %s has local changes; the installer will not overwrite them\n' "$recovery_checkout" >&2
    printf '%s\n' 'userland: review them with: git -C "$HOME/.userland" status --short' >&2
    printf '%s\n' 'userland: keep them with: git -C "$HOME/.userland" stash push --include-untracked' >&2
    printf '%s\n' 'userland: then rerun: curl -fsSL https://userland.guidotto.dev | sh' >&2
    exit 1
  fi
  exec 9<>/dev/tty
  printf '\n ◆  Local configuration changes\n │\n' >&9
  printf '%s\n' "$checkout_status" >&9
  review_checkout_diff "$recovery_checkout" || exit 1
  printf ' │\n ·  Untracked files are listed above; their contents are not shown.\n' >&9
  while :; do
    printf ' ·  Stash saves tracked and untracked changes for later. Restore discards tracked changes only.\n ?  [s] Stash and continue / [r] Restore tracked files / [c] Cancel [s] › ' >&9
    IFS= read -r recovery_choice <&9 || exit 1
    case "$recovery_choice" in
      '' | s | S | stash)
        checkout_git "$recovery_checkout" stash push --include-untracked -m "userland before $tag" >&9 2>&9 || exit 1
        printf ' ✓  Saved local changes in git stash; continuing installation.\n' >&9
        ;;
      r | R | restore)
        printf ' ?  Discard ALL staged and unstaged tracked changes? Type restore to confirm › ' >&9
        IFS= read -r recovery_confirmation <&9 || exit 1
        [ "$recovery_confirmation" = restore ] || continue
        checkout_git "$recovery_checkout" restore --source=HEAD --staged --worktree -- . >&9 2>&9 || exit 1
        printf ' ✓  Restored tracked files.\n' >&9
        ;;
      c | C | cancel)
        printf ' ·  Installation cancelled; remaining local changes kept.\n' >&9
        exit 1
        ;;
      *) continue ;;
    esac
    remaining=$(checkout_git "$recovery_checkout" status --porcelain=v1 --untracked-files=all --ignore-submodules=none) || exit 1
    [ -n "$remaining" ] || exit 0
    printf ' !  Local changes remain. Nested repositories must be handled separately.\n%s\n' "$remaining" >&9
  done
)

validate_checkout_identity() {
  checkout_path=$1
  [ ! -L "$checkout_path" ] || die "$checkout_path must not be a symlink"
  [ -d "$checkout_path/.git" ] || die "$checkout_path is not a Git checkout"
  [ ! -L "$checkout_path/.git" ] || die "$checkout_path/.git must not be a symlink"

  inside_work_tree=$(checkout_git "$checkout_path" rev-parse --is-inside-work-tree 2>/dev/null) ||
    die "could not inspect $checkout_path"
  [ "$inside_work_tree" = true ] || die "$checkout_path is not a Git work tree"

  if checkout_git "$checkout_path" config --local --no-includes --get core.worktree >/dev/null 2>&1; then
    die "$checkout_path uses an external Git work tree"
  fi

  origin=$(checkout_git "$checkout_path" config --local --no-includes --get remote.origin.url 2>/dev/null) ||
    die "$checkout_path has no origin"
  [ "$origin" = "$repository" ] || die "$checkout_path has an unexpected origin: $origin"

  branch=$(checkout_git "$checkout_path" symbolic-ref --quiet --short HEAD 2>/dev/null) ||
    die "$checkout_path is not on a branch"
  [ "$branch" = main ] || die "$checkout_path is on $branch, not main"
  upstream=$(checkout_git "$checkout_path" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null) ||
    die "$checkout_path main has no upstream"
  [ "$upstream" = origin/main ] || die "$checkout_path main does not track origin/main"

  [ -f "$checkout_path/cfg/mise.toml" ] && [ ! -L "$checkout_path/cfg/mise.toml" ] ||
    die "$checkout_path/cfg/mise.toml is not a regular file"
  [ -f "$checkout_path/cmd/userland/main.go" ] && [ ! -L "$checkout_path/cmd/userland/main.go" ] ||
    die "$checkout_path/cmd/userland/main.go is not a regular file"

  if [ "${2:-}" = recover ]; then
    recover_checkout_changes "$checkout_path"
  else
    checkout_status=$(checkout_git "$checkout_path" status --porcelain=v1 --untracked-files=all --ignore-submodules=none) ||
      die "could not read checkout status"
    if [ -n "$checkout_status" ]; then
      printf 'userland: %s has local changes; the installer will not overwrite them\n' "$checkout_path" >&2
      printf '%s\n' 'userland: review them with: git -C "$HOME/.userland" status --short' >&2
      printf '%s\n' 'userland: keep them with: git -C "$HOME/.userland" stash push --include-untracked' >&2
      printf '%s\n' 'userland: then rerun: curl -fsSL https://userland.guidotto.dev | sh' >&2
      exit 1
    fi

  fi
}

validate_checkout() {
  checkout_path=$1
  migrate_obsolete_nvim_submodule "$checkout_path"
  validate_checkout_identity "$checkout_path"

  head_commit=$(checkout_git "$checkout_path" rev-parse 'HEAD^{commit}' 2>/dev/null) ||
    die "could not resolve the checkout commit"
  [ "$head_commit" = "$commit" ] ||
    die "$checkout_path is not at the verified $tag commit"
  remote_main=$(
    cd "$release_dir"
    GIT_CEILING_DIRECTORIES="$data_dir" \
      GIT_CONFIG_GLOBAL=/dev/null \
      GIT_CONFIG_NOSYSTEM=1 \
      GIT_TERMINAL_PROMPT=0 \
      git ls-remote "$repository" refs/heads/main |
      awk 'NR == 1 { print $1 }'
  )
  [ -n "$remote_main" ] || die "could not resolve the remote main branch"
  local_remote_main=$(checkout_git "$checkout_path" rev-parse 'refs/remotes/origin/main^{commit}' 2>/dev/null) ||
    die "$checkout_path has no origin/main commit"
  [ "$local_remote_main" = "$remote_main" ] ||
    die "origin/main moved; refresh the checkout with git pull --ff-only before bootstrapping again"
  checkout_git "$checkout_path" cat-file -e "$commit^{commit}" 2>/dev/null ||
    die "$checkout_path does not contain the released commit"
  checkout_git "$checkout_path" merge-base --is-ancestor "$commit" "$remote_main" ||
    die "$tag is not on the remote main history"
}

prepare_checkout() {
  checkout_path=$1
  migrate_obsolete_nvim_submodule "$checkout_path"
  validate_checkout_identity "$checkout_path" recover
  previous_head=$(checkout_git "$checkout_path" rev-parse 'HEAD^{commit}' 2>/dev/null) ||
    die "could not resolve the checkout commit"

  checkout_git "$checkout_path" fetch --quiet --force origin \
    "refs/heads/main:refs/remotes/origin/main" \
    "refs/tags/$tag:refs/userland/bootstrap/$tag"
  fetched_commit=$(checkout_git "$checkout_path" rev-parse "refs/userland/bootstrap/$tag^{commit}" 2>/dev/null) ||
    die "could not resolve fetched $tag"
  [ "$fetched_commit" = "$commit" ] || die "$tag does not resolve to the released commit"
  checkout_git "$checkout_path" merge-base --is-ancestor "$previous_head" "$commit" ||
    die "$checkout_path cannot fast-forward to $tag"
  if [ "$previous_head" != "$commit" ]; then
    checkout_git "$checkout_path" merge --ff-only --quiet "$commit"
  fi
  checkout_git "$checkout_path" submodule sync --quiet --recursive
  checkout_git "$checkout_path" submodule update --quiet --init --recursive
  validate_checkout "$checkout_path"
}

run_repository_transaction() {
  repository_transaction_log=$control_dir/repository.log
  rm -f "$repository_transaction_log"
  set +e
  (
    set -e
    "$@"
  ) >"$repository_transaction_log" 2>&1
  repository_transaction_status=$?
  set -e

  if [ "$repository_transaction_status" -eq 0 ]; then
    rm -f "$repository_transaction_log"
    report_migration_notice
    return 0
  fi

  cat "$repository_transaction_log" >&2
  rm -f "$repository_transaction_log"
  report_migration_notice
  return "$repository_transaction_status"
}

materialize_git_checkout() {
  bootstrap_git clone --quiet --filter=blob:none --recurse-submodules "$repository" "$checkout_work/repo"
  cloned_commit=$(bootstrap_git -C "$checkout_work/repo" rev-parse "$tag^{commit}")
  [ "$cloned_commit" = "$commit" ] || die "$tag does not resolve to the released commit"
  bootstrap_git -C "$checkout_work/repo" merge-base --is-ancestor "$tag" origin/main ||
    die "$tag is not an ancestor of origin/main"
  bootstrap_git -C "$checkout_work/repo" checkout -B main "$tag"
  bootstrap_git -C "$checkout_work/repo" branch --set-upstream-to=origin/main main
  bootstrap_git -C "$checkout_work/repo" submodule update --quiet --init --recursive
  validate_checkout "$checkout_work/repo"
}

validate_materialized_checkout() {
  materialized_path=$1
  [ -d "$materialized_path" ] && [ ! -L "$materialized_path" ] ||
    die "$materialized_path is not a managed userland directory"
  [ -f "$materialized_path/.userland-stage" ] && [ ! -L "$materialized_path/.userland-stage" ] ||
    die "$materialized_path is not a managed userland stage"
  [ -f "$materialized_path/.userland-stage-version" ] && [ ! -L "$materialized_path/.userland-stage-version" ] ||
    die "$materialized_path has no staged release version"
  materialized_tag=$(cat "$materialized_path/.userland-stage-version")
  if [ "$(cat "$materialized_path/.userland-stage")" != "$commit" ] || [ "$materialized_tag" != "$tag" ]; then
    if [ "$materialized_tag" != "$tag" ]; then
      printf 'userland: discarding interrupted %s stage before installing %s\n' "$materialized_tag" "$tag" >&2
      rm -rf "$materialized_path"
      return 0
    fi
    die "$materialized_path contains a tampered interrupted $materialized_tag install"
  fi
  [ -f "$materialized_path/.userland-release" ] && [ ! -L "$materialized_path/.userland-release" ] ||
    die "$materialized_path has no release marker"
  [ "$(cat "$materialized_path/.userland-release")" = "$commit" ] ||
    die "$materialized_path contains another release"
  [ -x "$materialized_path/bin/userland" ] && [ ! -L "$materialized_path/bin/userland" ] ||
    die "$materialized_path has no regular userland command"
  [ -x "$materialized_path/bin/mise" ] && [ ! -L "$materialized_path/bin/mise" ] ||
    die "$materialized_path has no regular mise launcher"
  [ -f "$materialized_path/cfg/mise.toml" ] && [ ! -L "$materialized_path/cfg/mise.toml" ] ||
    die "$materialized_path/cfg/mise.toml is not a regular file"
  compare_materialized_tree "$release_dir" "$materialized_path" ||
    die "$materialized_path differs from the verified release"
}

create_materialized_checkout() {
  checkout_work=$(mktemp -d "$HOME/.userland.new.XXXXXX")
  cp -pR "$release_dir/." "$checkout_work/"
  [ ! -e "$checkout_work/.userland-stage" ] && [ ! -L "$checkout_work/.userland-stage" ] ||
    die "release contains a reserved stage marker"
  [ ! -e "$checkout_work/.userland-stage-version" ] && [ ! -L "$checkout_work/.userland-stage-version" ] ||
    die "release contains a reserved stage version"
  [ ! -e "$checkout_work/.userland-bootstrap-owner" ] && [ ! -L "$checkout_work/.userland-bootstrap-owner" ] ||
    die "release contains a reserved ownership marker"
  printf '%s\n' "$commit" >"$checkout_work/.userland-stage"
  printf '%s\n' "$tag" >"$checkout_work/.userland-stage-version"
  printf '%s\n' "$transaction_id" >"$checkout_work/.userland-bootstrap-owner"
  validate_materialized_checkout "$checkout_work"
  [ ! -e "$repo_dir" ] && [ ! -L "$repo_dir" ] ||
    die "$repo_dir appeared while preparing userland"
  mv "$checkout_work" "$repo_dir"
  checkout_work=
  repo_created=1
}

tree_manifest() {
  tree_root=$1
  (
    cd "$tree_root"
    find . -print |
      grep -v -e '^\./\.userland-stage$' \
        -e '^\./\.userland-stage-version$' \
        -e '^\./\.userland-bootstrap-owner$' |
      LC_ALL=C sort
  )
}

file_mode() {
  mode=$(stat -f '%Lp' "$1" 2>/dev/null || :)
  case "$mode" in
    '' | *[!0-9]*) stat -c '%a' "$1" ;;
    *) printf '%s\n' "$mode" ;;
  esac
}

compare_materialized_tree() {
  expected_root=$1
  actual_root=$2
  expected_manifest=$(tree_manifest "$expected_root") || return 1
  actual_manifest=$(tree_manifest "$actual_root") || return 1

  # Compare paths before inspecting contents. This prevents a symlink in either
  # tree from hiding an added or missing path, and it avoids following the
  # absolute Docker Compose link in the release.
  [ "$expected_manifest" = "$actual_manifest" ] || return 1
  while IFS= read -r relative_path; do
    [ "$relative_path" = . ] && continue
    expected_path="$expected_root/${relative_path#./}"
    actual_path="$actual_root/${relative_path#./}"
    if [ -L "$expected_path" ]; then
      [ -L "$actual_path" ] || return 1
      [ "$(readlink "$expected_path")" = "$(readlink "$actual_path")" ] || return 1
    elif [ -d "$expected_path" ]; then
      [ -d "$actual_path" ] && [ ! -L "$actual_path" ] || return 1
    elif [ -f "$expected_path" ]; then
      [ -f "$actual_path" ] && [ ! -L "$actual_path" ] || return 1
      cmp -s "$expected_path" "$actual_path" || return 1
      [ "$(file_mode "$expected_path")" = "$(file_mode "$actual_path")" ] || return 1
    else
      return 1
    fi
  done <<EOF
$expected_manifest
EOF
}

release_work=
checkout_work=
backup_dir=
control_dir=
transaction_id=
repo_created=0
lock_dir=
lock_acquired=0
promotion_published=0
repository_prepared=0

restore_release_command() {
  rollback_link="$bin_dir/.userland.rollback.$$"
  [ ! -e "$rollback_link" ] && [ ! -L "$rollback_link" ] || return 1
  ln -s "$release_dir/bin/userland" "$rollback_link" || return 1
  replace_link_atomically "$rollback_link" "$bin_dir/userland" || return 1
  [ "$(readlink "$bin_dir/userland" 2>/dev/null)" = "$release_dir/bin/userland" ]
}

repo_is_owned_by_transaction() {
  [ "$repo_created" -eq 1 ] || return 1
  [ -d "$repo_dir" ] && [ ! -L "$repo_dir" ] || return 1
  [ -f "$repo_dir/.userland-stage" ] && [ ! -L "$repo_dir/.userland-stage" ] || return 1
  [ "$(cat "$repo_dir/.userland-stage" 2>/dev/null)" = "$commit" ] || return 1
  [ -f "$repo_dir/.userland-bootstrap-owner" ] && [ ! -L "$repo_dir/.userland-bootstrap-owner" ] || return 1
  [ "$(cat "$repo_dir/.userland-bootstrap-owner" 2>/dev/null)" = "$transaction_id" ]
}

apply_started() {
  [ -n "$control_dir" ] || return 1
  [ -f "$control_dir/apply-started" ] && [ ! -L "$control_dir/apply-started" ] || return 1
  [ "$(cat "$control_dir/apply-started" 2>/dev/null)" = "$transaction_id" ]
}

backup_is_owned_by_transaction() {
  [ -n "$backup_dir" ] || return 1
  [ "$backup_dir" = "$HOME/.userland.archive.$transaction_id" ] || return 1
  [ -d "$backup_dir" ] && [ ! -L "$backup_dir" ] || return 1
  [ "$(cat "$backup_dir/.userland-stage" 2>/dev/null)" = "$commit" ] || return 1
  [ "$(cat "$backup_dir/.userland-bootstrap-owner" 2>/dev/null)" = "$transaction_id" ]
}

bootstrap_prepare_cancel_ui() {
  bootstrap_ui_mode=${USERLAND_UI_MODE:-auto}
  if [ "$bootstrap_ui_mode" = auto ]; then
    if [ -t 1 ] && [ "${TERM:-}" != dumb ] && [ -z "${CI:-}" ]; then
      bootstrap_ui_mode=rich
    else
      bootstrap_ui_mode=plain
    fi
  fi
  bootstrap_ui_unicode=${USERLAND_UNICODE:-0}
  return 0
}

bootstrap_ui_status() {
  bootstrap_ui_state=$1
  shift
  if [ "$bootstrap_ui_mode" = rich ]; then
    bootstrap_ui_symbol=o
    [ "$bootstrap_ui_unicode" = 0 ] || bootstrap_ui_symbol='◇'
    printf ' %s  %s\n' "$bootstrap_ui_symbol" "$*"
  else
    printf '[ok] %s\n' "$*"
  fi
}

bootstrap_ui_summary() {
  bootstrap_ui_state=$1
  shift
  if [ "$bootstrap_ui_mode" = rich ]; then
    bootstrap_ui_close='`'
    bootstrap_ui_rail='|'
    if [ "$bootstrap_ui_unicode" != 0 ]; then
      bootstrap_ui_close='└'
      bootstrap_ui_rail='│'
    fi
    printf ' %s\n %s  %s\n    <1s\n' "$bootstrap_ui_rail" "$bootstrap_ui_close" "$*"
  else
    printf '\n[%s] %s (<1s)\n' "$bootstrap_ui_state" "$*"
  fi
}

# shellcheck disable=SC2329 # Invoked by the signal and exit trap.
cleanup() {
  cleanup_status=$?
  trap - 0 HUP INT TERM

  if [ -n "$backup_dir" ] && [ -d "$backup_dir" ] && [ ! -e "$repo_dir" ]; then
    mv "$backup_dir" "$repo_dir" 2>/dev/null || :
  fi

  if [ -n "$backup_dir" ] && [ -e "$backup_dir" ] && [ ! -e "$repo_dir" ]; then
    restore_release_command || :
    printf 'userland: checkout recovery is at %s\n' "$backup_dir" >&2
  elif [ "$promotion_published" -eq 1 ] && backup_is_owned_by_transaction; then
    rm -rf "$backup_dir" || :
    backup_dir=
  fi

  if ! apply_started && repo_is_owned_by_transaction; then
    if restore_release_command; then
      cleanup_cancel_ui=0
      case "$cleanup_status" in
        3 | 129 | 130 | 143)
          if bootstrap_prepare_cancel_ui; then
            cleanup_cancel_ui=1
          fi
          ;;
      esac
      if [ "$cleanup_cancel_ui" -eq 0 ]; then
        printf 'userland: deleting ~/.userland\n' >&2
      fi
      if rm -rf "$repo_dir"; then
        repo_created=0
        if [ "$cleanup_cancel_ui" -eq 1 ]; then
          bootstrap_ui_status "done" "Deleting ~/.userland"
          bootstrap_ui_summary cancelled "Cancelled. No changes were applied."
        else
          printf 'userland: deleted ~/.userland\n' >&2
        fi
      else
        cleanup_status=1
        if [ "$cleanup_cancel_ui" -eq 1 ]; then
          bootstrap_ui_summary error "Could not delete ~/.userland."
        else
          printf 'userland: could not delete cancelled checkout at %s\n' "$repo_dir" >&2
        fi
      fi
    else
      printf 'userland: could not restore the recovery command; preserving %s\n' "$repo_dir" >&2
    fi
  elif apply_started && repo_is_owned_by_transaction; then
    case "$cleanup_status" in
      129 | 130 | 143)
        if bootstrap_prepare_cancel_ui; then
          bootstrap_ui_summary cancelled "Cancelled. Applied progress was preserved."
        else
          printf 'userland: cancelled; applied progress was preserved\n' >&2
        fi
        ;;
    esac
  fi

  [ -z "$release_work" ] || rm -rf "$release_work" || :
  [ -z "$checkout_work" ] || rm -rf "$checkout_work" || :
  if [ -n "$backup_dir" ] && [ -e "$backup_dir" ]; then
    printf 'userland: preserving interrupted checkout backup at %s\n' "$backup_dir" >&2
  fi
  [ -z "$control_dir" ] || rm -rf "$control_dir" || :
  if [ "$lock_acquired" -eq 1 ] && [ -d "$lock_dir" ] && [ ! -L "$lock_dir" ] &&
    [ "$(cat "$lock_dir/owner" 2>/dev/null)" = "$transaction_id" ]; then
    rm -f "$lock_dir/owner" "$lock_dir/pid" || :
    rmdir "$lock_dir" 2>/dev/null || :
  fi
  exit "$cleanup_status"
}

install_signal_traps() {
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

recover_stale_lock() {
  stale_lock=$1
  [ -d "$stale_lock" ] && [ ! -L "$stale_lock" ] || return 1
  stale_owner=$(cat "$stale_lock/owner" 2>/dev/null || :)
  stale_pid=$(cat "$stale_lock/pid" 2>/dev/null || :)
  case "$stale_pid" in
    '' | *[!0-9]*) stale_pid= ;;
    *) kill -0 "$stale_pid" 2>/dev/null && return 1 ;;
  esac
  case "$stale_owner" in
    .bootstrap.[A-Za-z0-9]*)
      case "$stale_pid" in
        '' | *[!0-9]*)
          stale_owner=
          stale_pid=
          ;;
      esac
      ;;
    *)
      stale_owner=
      stale_pid=
      ;;
  esac

  # The lock directory is created before its metadata. If the process dies in
  # that narrow window, use the transaction metadata written before lock
  # acquisition to distinguish a live bootstrap from a stale lock.
  if [ -z "$stale_owner" ]; then
    for stale_control in "$data_dir"/.bootstrap.*; do
      [ "$stale_control" = "$control_dir" ] && continue
      [ -d "$stale_control" ] && [ ! -L "$stale_control" ] || continue
      candidate_pid=$(cat "$stale_control/pid" 2>/dev/null || :)
      case "$candidate_pid" in
        '' | *[!0-9]*) continue ;;
      esac
      kill -0 "$candidate_pid" 2>/dev/null && return 1
    done
  fi

  if [ -n "$stale_owner" ]; then
    stale_control="$data_dir/$stale_owner"
    if [ -d "$stale_control" ] && [ ! -L "$stale_control" ]; then
      rm -rf "$stale_control" || return 1
    fi
  else
    for stale_control in "$data_dir"/.bootstrap.*; do
      [ "$stale_control" = "$control_dir" ] && continue
      [ -d "$stale_control" ] && [ ! -L "$stale_control" ] || continue
      candidate_pid=$(cat "$stale_control/pid" 2>/dev/null || :)
      case "$candidate_pid" in
        '' | *[!0-9]*) continue ;;
      esac
      kill -0 "$candidate_pid" 2>/dev/null && return 1
      rm -rf "$stale_control" || return 1
    done
  fi
  rm -f "$stale_lock/owner" "$stale_lock/pid" || return 1
  rmdir "$stale_lock" 2>/dev/null
}

recover_interrupted_promotion() {
  [ ! -e "$repo_dir" ] && [ ! -L "$repo_dir" ] || return 0
  promotion_backup=
  for candidate_backup in "$HOME"/.userland.archive.*; do
    [ -d "$candidate_backup" ] && [ ! -L "$candidate_backup" ] || continue
    candidate_owner=$(cat "$candidate_backup/.userland-bootstrap-owner" 2>/dev/null || :)
    case "$candidate_owner" in
      .bootstrap.[A-Za-z0-9]*) ;;
      *) continue ;;
    esac
    [ -f "$candidate_backup/.userland-stage" ] &&
      [ "$(cat "$candidate_backup/.userland-stage" 2>/dev/null)" = "$commit" ] || continue
    [ -f "$candidate_backup/.userland-stage-version" ] &&
      [ "$(cat "$candidate_backup/.userland-stage-version" 2>/dev/null)" = "$tag" ] || continue
    [ -z "$promotion_backup" ] || die "multiple interrupted checkout backups need attention"
    promotion_backup=$candidate_backup
  done

  [ -n "$promotion_backup" ] || return 0
  mv "$promotion_backup" "$repo_dir" || die "could not recover the interrupted checkout"
  promotion_owner_tmp="$repo_dir/.userland-bootstrap-owner.$$"
  printf '%s\n' "$transaction_id" >"$promotion_owner_tmp"
  mv "$promotion_owner_tmp" "$repo_dir/.userland-bootstrap-owner"
  repo_created=1
  printf 'userland: recovered interrupted checkout at %s\n' "$repo_dir" >&2
}

trap cleanup 0
install_signal_traps

mkdir -p "$data_dir/releases" "$bin_dir"

control_dir=$(mktemp -d "$data_dir/.bootstrap.XXXXXX")
transaction_id=${control_dir##*/}
printf '%s\n' "$transaction_id" >"$control_dir/owner"
printf '%s\n' "$$" >"$control_dir/pid"
lock_dir=$data_dir/bootstrap.lock
if ! mkdir "$lock_dir" 2>/dev/null; then
  recover_stale_lock "$lock_dir" ||
    die "another userland bootstrap is running; if it was force-quit, remove $lock_dir"
  mkdir "$lock_dir" || die "could not recover the stale bootstrap lock"
fi
printf '%s\n' "$transaction_id" >"$lock_dir/owner"
printf '%s\n' "$$" >"$lock_dir/pid"
lock_acquired=1
recover_interrupted_promotion

if [ -d "$release_dir" ]; then
  [ -f "$release_dir/.userland-release" ] || die "$release_dir exists but userland did not create it"
  [ "$(cat "$release_dir/.userland-release")" = "$commit" ] || die "$release_dir contains another release"
else
  release_work=$(mktemp -d "${TMPDIR:-/tmp}/userland-bootstrap.XXXXXX")
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
    --output "$release_work/$archive" "$release_url"
  actual_sha256=$(sha256 "$release_work/$archive")
  [ "$actual_sha256" = "$archive_sha256" ] || die "release archive checksum mismatch"

  tar -xzf "$release_work/$archive" -C "$release_work"
  extracted="$release_work/userland-${tag#v}"
  [ -x "$extracted/bin/userland" ] || die "release archive has no bin/userland"
  [ -x "$extracted/bin/mise" ] || die "release archive has no generated bin/mise"
  [ ! -e "$extracted/.userland-release" ] && [ ! -L "$extracted/.userland-release" ] ||
    die "release archive contains a reserved marker"
  printf '%s\n' "$commit" >"$extracted/.userland-release"
  mv "$extracted" "$release_dir"
fi

# Reject an unfinished stage from another release before publishing any new
# current-release or command links. The pinned recovery command remains intact.
if [ ! -L "$repo_dir" ] && [ -e "$repo_dir" ] && [ ! -d "$repo_dir/.git" ]; then
  validate_materialized_checkout "$repo_dir"
fi

install_current_release_link
cleanup_stale_current_links
install_command_link "$release_dir/bin/userland"
MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$release_dir/cfg/mise.toml" >/dev/null

if [ -L "$repo_dir" ]; then
  die "$repo_dir must not be a symlink"
elif [ -d "$repo_dir/.git" ]; then
  repository_previous_head=$(checkout_git "$repo_dir" rev-parse 'HEAD^{commit}' 2>/dev/null) ||
    die "could not resolve the checkout commit"
  run_repository_transaction prepare_checkout "$repo_dir"
  if [ "$repository_previous_head" != "$commit" ]; then
    repository_prepared=1
  fi
elif [ -e "$repo_dir" ]; then
  validate_materialized_checkout "$repo_dir"
  [ -e "$repo_dir" ] || create_materialized_checkout
else
  create_materialized_checkout
fi

MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$repo_dir/cfg/mise.toml" >/dev/null
materialize_checkout_command() {
  mkdir -p "$repo_dir/bin"
  checkout_command_tmp=$repo_dir/bin/.userland.$$
  cp -p "$release_dir/bin/userland" "$checkout_command_tmp"
  mv "$checkout_command_tmp" "$repo_dir/bin/userland"
  if [ -d "$repo_dir/.git" ] && [ ! -L "$repo_dir/.git" ]; then
    mkdir -p "$repo_dir/.git/info"
    checkout_exclude=$repo_dir/.git/info/exclude
    touch "$checkout_exclude"
    grep -Fqx '/bin/userland' "$checkout_exclude" || printf '%s\n' '/bin/userland' >>"$checkout_exclude"
  fi
}
materialize_checkout_command
install_command_link "$repo_dir/bin/userland"

run_sync() {
  if command -v caffeinate >/dev/null 2>&1; then
    USERLAND_ARCHIVE=1 \
      USERLAND_VERSION="$tag" \
      USERLAND_BOOTSTRAP_CREATED="$repo_created" \
      USERLAND_BOOTSTRAP_REPOSITORY_PREPARED="$repository_prepared" \
      USERLAND_BOOTSTRAP_CONTROL="$control_dir" \
      USERLAND_BOOTSTRAP_TOKEN="$transaction_id" \
      caffeinate -dims "$repo_dir/bin/userland" sync
  else
    USERLAND_ARCHIVE=1 \
      USERLAND_VERSION="$tag" \
      USERLAND_BOOTSTRAP_CREATED="$repo_created" \
      USERLAND_BOOTSTRAP_REPOSITORY_PREPARED="$repository_prepared" \
      USERLAND_BOOTSTRAP_CONTROL="$control_dir" \
      USERLAND_BOOTSTRAP_TOKEN="$transaction_id" \
      "$repo_dir/bin/userland" sync
  fi
}

sync_status=0
if [ "${USERLAND_NO_TTY:-0}" != 1 ] && tty -s 2>/dev/null </dev/tty; then
  if run_sync </dev/tty; then
    :
  else
    sync_status=$?
  fi
else
  if run_sync; then
    :
  else
    sync_status=$?
  fi
fi

case "$sync_status" in
  0 | 2) ;;
  *) exit "$sync_status" ;;
esac

# Exit 2 also represents a plan blocked before approval. Only promote a staged
# checkout after sync crossed the apply checkpoint.
if [ "$sync_status" -eq 2 ] && ! apply_started; then
  exit 2
fi

if [ ! -d "$repo_dir/.git" ]; then
  command -v git >/dev/null 2>&1 || die "sync completed without installing Git"
  checkout_work=$(mktemp -d "$HOME/.userland.git.XXXXXX")
  run_repository_transaction materialize_git_checkout

  backup_dir="$HOME/.userland.archive.$transaction_id"
  [ ! -e "$backup_dir" ] && [ ! -L "$backup_dir" ] ||
    die "temporary checkout backup already exists: $backup_dir"

  printf '%s\n' "$transaction_id" >"$repo_dir/.userland-bootstrap-owner"

  trap '' HUP INT TERM
  if ! mv "$repo_dir" "$backup_dir"; then
    install_signal_traps
    die "could not prepare the Git checkout promotion"
  fi
  if ! mv "$checkout_work/repo" "$repo_dir"; then
    mv "$backup_dir" "$repo_dir" 2>/dev/null || :
    install_signal_traps
    die "could not publish the Git checkout"
  fi
  promotion_published=1
  install_signal_traps
  backup_is_owned_by_transaction || die "checkout backup ownership changed during promotion"
  rm -rf "$backup_dir"
  backup_dir=
  rm -rf "$checkout_work"
  checkout_work=
fi

validate_checkout "$repo_dir"
report_migration_notice
MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$repo_dir/cfg/mise.toml" >/dev/null
install_command_link "$repo_dir/bin/userland"

# A first bootstrap is usually launched from Terminal.app. Move the completed
# health check into Ghostty so the bootstrap window can close without hiding
# the result. This is deliberately opt-in by environment only for other
# terminals; rerunning `userland sync` from Ghostty never creates another
# window.
handoff_to_ghostty() {
  [ "${USERLAND_NO_GHOSTTY_HANDOFF:-0}" != 1 ] || return 0
  [ "${TERM_PROGRAM:-}" = "Apple_Terminal" ] || return 0
  [ -d /Applications/Ghostty.app ] || return 0
  command -v osascript >/dev/null 2>&1 || return 0
  doctor_command="$repo_dir/bin/userland doctor"
  if osascript - "$doctor_command" >/dev/null 2>&1 <<'APPLESCRIPT'; then
on run argv
  set doctorCommand to item 1 of argv
  tell application "Ghostty"
    activate
    set cfg to new surface configuration
    set command of cfg to doctorCommand
    set wait after command of cfg to true
    set newWindow to new window with configuration cfg
  end tell
  tell application "Terminal"
    try
      close front window
    end try
  end tell
end run
APPLESCRIPT
    return 0
  fi
  printf '%s\n' 'userland: could not hand off doctor to Ghostty; run: userland doctor' >&2
}

if [ "$sync_status" -eq 2 ]; then
  printf 'userland is installed. Manual steps remain; run: userland sync\n'
fi
handoff_to_ghostty
exit 0
