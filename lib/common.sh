#!/bin/sh

if [ -n "${USERLAND_COMMON_LOADED:-}" ]; then
  return 0
fi
USERLAND_COMMON_LOADED=1

: "${USERLAND_ROOT:?USERLAND_ROOT must name the userland checkout}"
: "${USERLAND_HOME:=${HOME:?HOME must be set}}"
: "${USERLAND_DATA_DIR:=${XDG_DATA_HOME:-$USERLAND_HOME/.local/share}/userland}"
: "${USERLAND_CACHE_DIR:=${XDG_CACHE_HOME:-$USERLAND_HOME/.cache}/userland}"
: "${USERLAND_STATE_DIR:=${XDG_STATE_HOME:-$USERLAND_HOME/.local/state}/userland}"
: "${USERLAND_REPOSITORY_TTL_SECONDS:=86400}"

# A fresh Mac has these paths. Keep them explicit because app launchers and
# recovery shells can replace PATH with a narrower value.
PATH="$USERLAND_HOME/.local/share/mise/shims:$USERLAND_HOME/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
export PATH

export USERLAND_HOME USERLAND_DATA_DIR USERLAND_CACHE_DIR USERLAND_STATE_DIR
export USERLAND_REPOSITORY_TTL_SECONDS

userland_now() {
  date '+%Y-%m-%dT%H:%M:%S%z'
}

userland_log() {
  userland_log_level=$1
  shift
  userland_log_message=$*
  userland_log_message=$(printf '%s\n' "$userland_log_message" | sed "s|$USERLAND_HOME|~|g")
  printf '%s  %-7s %s\n' "$(userland_now)" "$userland_log_level" "$userland_log_message"
}

userland_die() {
  userland_error_message=$1
  userland_error_code=${2:-1}
  userland_log error "$userland_error_message" >&2
  exit "$userland_error_code"
}

userland_require_mise() {
  if [ -z "${USERLAND_MISE:-}" ] || [ ! -x "$USERLAND_MISE" ]; then
    userland_die "mise is missing; run the public bootstrap command first"
  fi
}

userland_require_schema() {
  userland_supported_schema=1
  userland_schema_file=$USERLAND_ROOT/config/schema-version
  [ -f "$userland_schema_file" ] || userland_die "state schema is missing"
  userland_declared_schema=$(sed -n '1p' "$userland_schema_file")
  [ "$userland_declared_schema" = "$userland_supported_schema" ] ||
    userland_die "state schema $userland_declared_schema requires a different userland release"
}

userland_mkdirs() {
  mkdir -p "$USERLAND_DATA_DIR" "$USERLAND_CACHE_DIR" "$USERLAND_STATE_DIR/receipts"
}

userland_sha256() {
  shasum -a 256 "$1" | awk '{print $1}'
}

userland_is_macos() {
  [ "${USERLAND_UNAME:-$(uname -s)}" = "Darwin" ]
}

userland_run_adapters() {
  userland_adapter_action=$1
  userland_adapter_failures=0
  userland_adapter_attention=0

  # Adapter order is implementation, not machine state. Keep it private to the
  # dispatcher so config contains only user-owned choices.
  userland_adapter_names='homebrew-apps android-sdk personal-repos browser-extensions file-handlers raycast shell-cache manual-apps repository-snapshot security-health'
  for userland_adapter_name in $userland_adapter_names; do
    userland_adapter=$USERLAND_ROOT/lib/adapters/$userland_adapter_name.sh
    [ -x "$userland_adapter" ] || {
      userland_log error "declared adapter is missing or not executable: $userland_adapter_name"
      userland_adapter_failures=$((userland_adapter_failures + 1))
      continue
    }
    if "$userland_adapter" "$userland_adapter_action"; then
      :
    else
      userland_adapter_code=$?
      if [ "$userland_adapter_code" -eq 2 ]; then
        userland_adapter_attention=1
      else
        userland_adapter_failures=$((userland_adapter_failures + 1))
      fi
    fi
  done

  [ "$userland_adapter_failures" -eq 0 ] || return 1
  if [ "$userland_adapter_action" = doctor ] && [ "$userland_adapter_attention" -ne 0 ]; then
    return 2
  fi
  return 0
}
