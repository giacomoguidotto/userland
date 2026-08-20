#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_security_health() {
  userland_security_attention=0

  if fdesetup status 2>/dev/null | grep -q 'FileVault is On'; then
    userland_log healthy "FileVault is on"
  else
    userland_log attention "FileVault is not on"
    userland_security_attention=1
  fi

  if csrutil status 2>/dev/null | grep -q 'enabled'; then
    userland_log healthy "System Integrity Protection is enabled"
  else
    userland_log attention "System Integrity Protection is not enabled"
    userland_security_attention=1
  fi

  if softwareupdate --schedule 2>/dev/null | grep -qi 'on'; then
    userland_log healthy "automatic update checks are on"
  else
    userland_log attention "automatic update checks are off"
    userland_security_attention=1
  fi

  if tmutil destinationinfo >/dev/null 2>&1; then
    userland_log healthy "a Time Machine destination is configured"
  else
    userland_log attention "no Time Machine destination is configured"
    userland_security_attention=1
  fi

  userland_free_kib=$(df -Pk / | awk 'NR == 2 {print $4}')
  if [ "${userland_free_kib:-0}" -ge 31457280 ]; then
    userland_log healthy "at least 30 GiB is free for large developer applications"
  else
    userland_log attention "less than 30 GiB is free; Xcode or simulator installs may fail"
    userland_security_attention=1
  fi

  [ "$userland_security_attention" -eq 0 ]
}

case "${1:-}" in
  plan)
    ;;
  apply)
    # Security posture is intentionally observational. userland does not weaken
    # or silently mutate system security controls.
    ;;
  doctor)
    userland_is_macos || exit 0
    userland_security_health || exit 2
    ;;
  *)
    userland_die "security-health adapter expects plan, apply, or doctor" 64
    ;;
esac
