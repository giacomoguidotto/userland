#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/libexec/userland/common.sh"

userland_repositories=${USERLAND_REPOSITORIES:-$USERLAND_ROOT/state/repositories.tsv}

userland_repositories_check() {
  userland_repository_drift=0
  while IFS="$(printf '\t')" read -r userland_repo_slug userland_repo_relative_path; do
    case "$userland_repo_slug" in '' | '#'*) continue ;; esac
    userland_repo_path=$USERLAND_HOME/$userland_repo_relative_path
    if [ -e "$userland_repo_path/.git" ]; then
      :
    elif [ -e "$userland_repo_path" ]; then
      userland_log attention "$userland_repo_path exists and is not a Git checkout"
      userland_repository_drift=$((userland_repository_drift + 1))
    else
      userland_log change "$userland_repo_slug is missing at $userland_repo_path"
      userland_repository_drift=$((userland_repository_drift + 1))
    fi
  done <"$userland_repositories"
  [ "$userland_repository_drift" -eq 0 ]
}

case "${1:-}" in
  plan)
    userland_repositories_check || exit 2
    userland_log current "declared personal repositories exist"
    ;;
  apply)
    userland_repositories_check && exit 0
    command -v gh >/dev/null 2>&1 || {
      userland_log attention "GitHub CLI is unavailable"
      exit 2
    }
    if ! gh auth status --hostname github.com >/dev/null 2>&1; then
      userland_log manual "authenticate GitHub with: gh auth login --hostname github.com --git-protocol https"
      exit 2
    fi

    while IFS="$(printf '\t')" read -r userland_repo_slug userland_repo_relative_path; do
      case "$userland_repo_slug" in '' | '#'*) continue ;; esac
      userland_repo_path=$USERLAND_HOME/$userland_repo_relative_path
      if [ -e "$userland_repo_path/.git" ]; then
        continue
      elif [ -e "$userland_repo_path" ]; then
        userland_log attention "refused to replace $userland_repo_path"
        continue
      fi
      mkdir -p "$(dirname "$userland_repo_path")"
      gh repo clone "$userland_repo_slug" "$userland_repo_path" -- --filter=blob:none
      userland_log changed "cloned $userland_repo_slug"
    done <"$userland_repositories"
    ;;
  doctor)
    if userland_repositories_check; then
      userland_log healthy "declared personal repositories exist"
    else
      exit 2
    fi
    ;;
  *)
    userland_die "personal-repos adapter expects plan, apply, or doctor" 64
    ;;
esac
