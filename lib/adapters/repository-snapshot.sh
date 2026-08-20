#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"
# shellcheck source=../repository.sh
. "$USERLAND_ROOT/lib/repository.sh"

case "${1:-}" in
  plan)
    if userland_repository_snapshot_is_fresh; then
      userland_log current "repository snapshot is current"
    else
      userland_log change "repository snapshot will refresh during plan"
    fi
    ;;
  apply)
    # Repository discovery is observational. Plan owns its 24-hour refresh.
    ;;
  doctor)
    if userland_repository_snapshot_is_fresh; then
      userland_log healthy "repository snapshot is current"
    else
      userland_log attention "repository snapshot is missing or older than 24 hours"
      exit 2
    fi
    ;;
  *)
    userland_die "repository-snapshot adapter expects plan, apply, or doctor" 64
    ;;
esac
