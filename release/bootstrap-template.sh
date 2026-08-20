#!/bin/sh
set -eu

tag='@USERLAND_TAG@'
commit='@USERLAND_COMMIT@'
archive_sha256='@USERLAND_ARCHIVE_SHA256@'
archive="userland-$tag.tar.gz"
repository='https://github.com/giacomoguidotto/userland.git'
release_url="https://github.com/giacomoguidotto/userland/releases/download/$tag/$archive"
data_dir=${USERLAND_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/userland}
release_dir="$data_dir/releases/$tag"
repo_dir="$data_dir/repo"
bin_dir=${USERLAND_BIN_DIR:-$HOME/.local/bin}
PATH="$HOME/.local/share/mise/shims:$bin_dir:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export PATH

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

install_command_link() {
  command_target=$1
  command_path="$bin_dir/userland"

  if [ -L "$command_path" ]; then
    existing_target=$(readlink "$command_path")
    case "$existing_target" in
      "$data_dir"/releases/*/bin/userland | "$repo_dir"/bin/userland) ;;
      *) die "$command_path is an unmanaged symlink to $existing_target" ;;
    esac
  elif [ -e "$command_path" ]; then
    die "$command_path exists and is not a userland-managed symlink"
  fi

  temporary_link="$bin_dir/.userland.new.$$"
  [ ! -e "$temporary_link" ] && [ ! -L "$temporary_link" ] ||
    die "temporary command path already exists: $temporary_link"
  ln -s "$command_target" "$temporary_link"
  mv -f "$temporary_link" "$command_path"
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
  mv -f "$temporary_link" "$current_path"
}

checkout_git() {
  GIT_CONFIG_GLOBAL=/dev/null \
    GIT_CONFIG_NOSYSTEM=1 \
    GIT_OPTIONAL_LOCKS=0 \
    git \
    -c core.fsmonitor=false \
    -c core.hooksPath=/dev/null \
    -C "$repo_dir" \
    "$@"
}

validate_checkout() {
  [ ! -L "$repo_dir" ] || die "$repo_dir must not be a symlink"
  [ -d "$repo_dir/.git" ] || die "$repo_dir is not a Git checkout"
  [ ! -L "$repo_dir/.git" ] || die "$repo_dir/.git must not be a symlink"

  inside_work_tree=$(checkout_git rev-parse --is-inside-work-tree 2>/dev/null) ||
    die "could not inspect $repo_dir"
  [ "$inside_work_tree" = true ] || die "$repo_dir is not a Git work tree"

  if checkout_git config --local --no-includes --get core.worktree >/dev/null 2>&1; then
    die "$repo_dir uses an external Git work tree"
  fi

  origin=$(checkout_git config --local --no-includes --get remote.origin.url 2>/dev/null) ||
    die "$repo_dir has no origin"
  [ "$origin" = "$repository" ] || die "$repo_dir has an unexpected origin: $origin"

  checkout_status=$(checkout_git status --porcelain=v1 --untracked-files=all --ignore-submodules=none) ||
    die "could not read checkout status"
  [ -z "$checkout_status" ] || die "$repo_dir has local changes"

  branch=$(checkout_git symbolic-ref --quiet --short HEAD 2>/dev/null) ||
    die "$repo_dir is not on a branch"
  [ "$branch" = main ] || die "$repo_dir is on $branch, not main"
  upstream=$(checkout_git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null) ||
    die "$repo_dir main has no upstream"
  [ "$upstream" = origin/main ] || die "$repo_dir main does not track origin/main"

  head_commit=$(checkout_git rev-parse 'HEAD^{commit}' 2>/dev/null) ||
    die "could not resolve the checkout commit"
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
  local_remote_main=$(checkout_git rev-parse 'refs/remotes/origin/main^{commit}' 2>/dev/null) ||
    die "$repo_dir has no origin/main commit"
  [ "$local_remote_main" = "$remote_main" ] ||
    die "origin/main moved; refresh the checkout with git pull --ff-only before bootstrapping again"
  checkout_git cat-file -e "$commit^{commit}" 2>/dev/null ||
    die "$repo_dir does not contain the released commit"
  checkout_git merge-base --is-ancestor "$commit" "$head_commit" ||
    die "$repo_dir predates or diverges from $tag"
  checkout_git merge-base --is-ancestor "$head_commit" "$remote_main" ||
    die "$repo_dir is not on the remote main history"

  [ -f "$repo_dir/mise.toml" ] && [ ! -L "$repo_dir/mise.toml" ] ||
    die "$repo_dir/mise.toml is not a regular file"
  [ -f "$repo_dir/bin/userland" ] && [ -x "$repo_dir/bin/userland" ] && [ ! -L "$repo_dir/bin/userland" ] ||
    die "$repo_dir/bin/userland is not a regular executable"
}

# shellcheck disable=SC2329 # Invoked by the signal and exit trap.
cleanup() {
  [ -z "${work-}" ] || rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$data_dir/releases" "$bin_dir"

if [ -d "$release_dir" ]; then
  [ -f "$release_dir/.userland-release" ] || die "$release_dir exists but userland did not create it"
  [ "$(cat "$release_dir/.userland-release")" = "$commit" ] || die "$release_dir contains another release"
else
  work=$(mktemp -d "${TMPDIR:-/tmp}/userland-bootstrap.XXXXXX")
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
    --output "$work/$archive" "$release_url"
  actual_sha256=$(sha256 "$work/$archive")
  [ "$actual_sha256" = "$archive_sha256" ] || die "release archive checksum mismatch"

  tar -xzf "$work/$archive" -C "$work"
  extracted="$work/userland-${tag#v}"
  [ -x "$extracted/bin/userland" ] || die "release archive has no bin/userland"
  [ -x "$extracted/bin/mise" ] || die "release archive has no generated bin/mise"
  printf '%s\n' "$commit" >"$extracted/.userland-release"
  mv "$extracted" "$release_dir"
fi

install_current_release_link
install_command_link "$release_dir/bin/userland"
"$release_dir/bin/mise" trust --yes "$release_dir/mise.toml"

run_sync() {
  if command -v caffeinate >/dev/null 2>&1; then
    USERLAND_ARCHIVE=1 caffeinate -dims "$release_dir/bin/userland" sync
  else
    USERLAND_ARCHIVE=1 "$release_dir/bin/userland" sync
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

if [ -L "$repo_dir" ]; then
  die "$repo_dir is a symlink"
fi

if [ -e "$repo_dir" ] && [ ! -d "$repo_dir/.git" ]; then
  die "$repo_dir exists and is not a Git checkout"
fi

if [ ! -d "$repo_dir/.git" ]; then
  command -v git >/dev/null 2>&1 || die "sync completed without installing Git"
  work=$(mktemp -d "$data_dir/.repo.XXXXXX")
  git clone --filter=blob:none --recurse-submodules "$repository" "$work/repo"
  cloned_commit=$(git -C "$work/repo" rev-parse "$tag^{commit}")
  [ "$cloned_commit" = "$commit" ] || die "$tag does not resolve to the released commit"
  git -C "$work/repo" merge-base --is-ancestor "$tag" origin/main ||
    die "$tag is not an ancestor of origin/main"
  git -C "$work/repo" checkout -B main "$tag"
  git -C "$work/repo" branch --set-upstream-to=origin/main main
  git -C "$work/repo" submodule update --init --recursive
  [ ! -e "$repo_dir" ] || die "$repo_dir appeared while cloning"
  mv "$work/repo" "$repo_dir"
fi

validate_checkout
"$release_dir/bin/mise" trust --yes "$repo_dir/mise.toml"
install_command_link "$repo_dir/bin/userland"

if [ "$sync_status" -eq 2 ]; then
  printf 'userland is installed. Manual steps remain; run: userland sync\n'
else
  printf 'userland is ready. Run: userland doctor\n'
fi
exit 0
