#!/bin/sh
set -eu

bootstrap_started_at=$(date +%s)

tag='@USERLAND_TAG@'
commit='@USERLAND_COMMIT@'
archive_sha256='@USERLAND_ARCHIVE_SHA256@'
archive="userland-$tag.tar.gz"
repository='https://github.com/giacomoguidotto/userland.git'
release_url="https://github.com/giacomoguidotto/userland/releases/download/$tag/$archive"
data_dir=${USERLAND_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/userland}
release_dir="$data_dir/releases/$tag"
repo_dir=$HOME/.userland
legacy_repo_dir="$data_dir/repo"
bin_dir=${USERLAND_BIN_DIR:-$HOME/.local/bin}
: "${USERLAND_ORIGINAL_PATH:=${PATH:-}}"
PATH="$HOME/.local/share/mise/shims:$bin_dir:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
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

  checkout_status=$(checkout_git "$checkout_path" status --porcelain=v1 --untracked-files=all --ignore-submodules=none) ||
    die "could not read checkout status"
  [ -z "$checkout_status" ] || die "$checkout_path has local changes"

  branch=$(checkout_git "$checkout_path" symbolic-ref --quiet --short HEAD 2>/dev/null) ||
    die "$checkout_path is not on a branch"
  [ "$branch" = main ] || die "$checkout_path is on $branch, not main"
  upstream=$(checkout_git "$checkout_path" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null) ||
    die "$checkout_path main has no upstream"
  [ "$upstream" = origin/main ] || die "$checkout_path main does not track origin/main"

  [ -f "$checkout_path/mise.toml" ] && [ ! -L "$checkout_path/mise.toml" ] ||
    die "$checkout_path/mise.toml is not a regular file"
  [ -f "$checkout_path/bin/userland" ] && [ -x "$checkout_path/bin/userland" ] && [ ! -L "$checkout_path/bin/userland" ] ||
    die "$checkout_path/bin/userland is not a regular executable"
}

validate_checkout() {
  checkout_path=$1
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
  validate_checkout_identity "$checkout_path"
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
    return 0
  fi

  cat "$repository_transaction_log" >&2
  rm -f "$repository_transaction_log"
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
  [ "$(cat "$materialized_path/.userland-stage")" = "$commit" ] && [ "$materialized_tag" = "$tag" ] ||
    die "$materialized_path contains an interrupted $materialized_tag install; finish it with: curl -fsSL https://userland.guidotto.dev/$materialized_tag | sh"
  [ -f "$materialized_path/.userland-release" ] && [ ! -L "$materialized_path/.userland-release" ] ||
    die "$materialized_path has no release marker"
  [ "$(cat "$materialized_path/.userland-release")" = "$commit" ] ||
    die "$materialized_path contains another release"
  [ -x "$materialized_path/bin/userland" ] && [ ! -L "$materialized_path/bin/userland" ] ||
    die "$materialized_path has no regular userland command"
  [ -x "$materialized_path/bin/mise" ] && [ ! -L "$materialized_path/bin/mise" ] ||
    die "$materialized_path has no regular mise launcher"
  [ -f "$materialized_path/mise.toml" ] && [ ! -L "$materialized_path/mise.toml" ] ||
    die "$materialized_path/mise.toml is not a regular file"
  [ -z "$(find "$release_dir" -type l -print -quit 2>/dev/null)" ] ||
    die "release archives with symlinks are not supported"
  [ -z "$(find "$materialized_path" -type l -print -quit 2>/dev/null)" ] ||
    die "$materialized_path contains an unexpected symlink"
  /usr/bin/diff -qr \
    -x .userland-stage \
    -x .userland-stage-version \
    -x .userland-bootstrap-owner \
    "$release_dir" "$materialized_path" >/dev/null ||
    die "$materialized_path differs from the verified release"
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
  [ -f "$repo_dir/lib/ui.sh" ] && [ ! -L "$repo_dir/lib/ui.sh" ] || return 1
  USERLAND_HOME=$HOME
  export USERLAND_HOME
  # shellcheck source=../lib/ui.sh
  . "$repo_dir/lib/ui.sh"
  # shellcheck disable=SC2034 # Consumed by userland_ui_elapsed in the sourced module.
  userland_ui_started_at=$bootstrap_started_at
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
          userland_ui status "done" "Deleting ~/.userland"
          userland_ui summary cancelled "Cancelled. No changes were applied."
        else
          printf 'userland: deleted ~/.userland\n' >&2
        fi
      else
        cleanup_status=1
        if [ "$cleanup_cancel_ui" -eq 1 ]; then
          userland_ui summary error "Could not delete ~/.userland."
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
          userland_ui summary cancelled "Cancelled. Applied progress was preserved."
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
    rm -f "$lock_dir/owner" || :
    rmdir "$lock_dir" 2>/dev/null || :
  fi
  exit "$cleanup_status"
}

install_signal_traps() {
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

trap cleanup 0
install_signal_traps

mkdir -p "$data_dir/releases" "$bin_dir"

control_dir=$(mktemp -d "$data_dir/.bootstrap.XXXXXX")
transaction_id=${control_dir##*/}
printf '%s\n' "$transaction_id" >"$control_dir/owner"
lock_dir=$data_dir/bootstrap.lock
if ! mkdir "$lock_dir" 2>/dev/null; then
  die "another userland bootstrap is running; if it was force-quit, remove $lock_dir"
fi
printf '%s\n' "$transaction_id" >"$lock_dir/owner"
lock_acquired=1

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
MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$release_dir/mise.toml" >/dev/null

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
else
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
fi

MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$repo_dir/mise.toml" >/dev/null
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
MISE_QUIET=1 "$release_dir/bin/mise" trust --yes "$repo_dir/mise.toml" >/dev/null
install_command_link "$repo_dir/bin/userland"

if [ "$sync_status" -eq 2 ]; then
  printf 'userland is installed. Manual steps remain; run: userland sync\n'
fi
exit 0
