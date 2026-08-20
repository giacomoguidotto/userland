#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_sync_preflight() {
  if [ "${USERLAND_TESTING:-}" = 1 ]; then
    return 0
  fi

  userland_is_macos || userland_die "sync currently supports macOS only"

  userland_machine_arch=$(uname -m)
  [ "$userland_machine_arch" = arm64 ] ||
    userland_die "sync supports Apple silicon only; found $userland_machine_arch"

  userland_free_kib=$(df -Pk / | awk 'NR == 2 {print $4}')
  [ "${userland_free_kib:-0}" -ge 31457280 ] ||
    userland_die "sync needs at least 30 GiB free before large application installs"

  userland_log ready "macOS, Apple silicon, and disk-space preflight passed"
}
