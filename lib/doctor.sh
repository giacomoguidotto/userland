#!/bin/sh

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_doctor_version_probe() {
  userland_doctor_version_state=unknown
  userland_doctor_latest_version=
  userland_doctor_latest_commit=
  userland_doctor_local_commit=
  userland_doctor_release_endpoint=${USERLAND_RELEASE_ENDPOINT:-https://userland.guidotto.dev}
  userland_doctor_curl=${USERLAND_CURL:-curl}

  command -v "$userland_doctor_curl" >/dev/null 2>&1 || return 0
  userland_doctor_release_header=$("$userland_doctor_curl" \
    --proto '=https' \
    --tlsv1.2 \
    --fail \
    --location \
    --silent \
    --connect-timeout 2 \
    --max-time 5 \
    --range 0-511 \
    "$userland_doctor_release_endpoint" 2>/dev/null) || return 0

  userland_doctor_version_candidate=$(printf '%s\n' "$userland_doctor_release_header" |
    sed -n "s/^tag='\([^']*\)'$/\1/p" | sed -n '1p')
  userland_doctor_commit_candidate=$(printf '%s\n' "$userland_doctor_release_header" |
    sed -n "s/^commit='\([0-9a-f]*\)'$/\1/p" | sed -n '1p')

  userland_doctor_version=${userland_doctor_version_candidate#v}
  userland_doctor_version_major=${userland_doctor_version%%.*}
  userland_doctor_version_rest=${userland_doctor_version#*.}
  userland_doctor_version_minor=${userland_doctor_version_rest%%.*}
  userland_doctor_version_patch=${userland_doctor_version_rest#*.}
  case "$userland_doctor_version_major:$userland_doctor_version_minor:$userland_doctor_version_patch" in
    *[!0-9:]* | :* | *: | *::*) return 0 ;;
  esac
  [ "$userland_doctor_version_candidate" = "v$userland_doctor_version_major.$userland_doctor_version_minor.$userland_doctor_version_patch" ] || return 0
  [ "${#userland_doctor_commit_candidate}" -eq 40 ] || return 0
  case "$userland_doctor_commit_candidate" in *[!0-9a-f]*) return 0 ;; esac
  userland_doctor_latest_version=$userland_doctor_version_candidate
  userland_doctor_latest_commit=$userland_doctor_commit_candidate

  if command -v git >/dev/null 2>&1 &&
    git -C "$USERLAND_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    userland_doctor_local_commit=$(git -C "$USERLAND_ROOT" rev-parse 'HEAD^{commit}' 2>/dev/null) || return 0
  elif [ -f "$USERLAND_ROOT/.userland-release" ] && [ ! -L "$USERLAND_ROOT/.userland-release" ]; then
    userland_doctor_local_commit=$(sed -n '1p' "$USERLAND_ROOT/.userland-release")
  else
    return 0
  fi

  if [ "$userland_doctor_local_commit" = "$userland_doctor_latest_commit" ]; then
    userland_doctor_version_state=current
  elif command -v git >/dev/null 2>&1 &&
    git -C "$USERLAND_ROOT" merge-base --is-ancestor \
      "$userland_doctor_latest_commit" "$userland_doctor_local_commit" 2>/dev/null; then
    userland_doctor_version_state=ahead
  else
    userland_doctor_version_state=outdated
  fi
}

userland_doctor_json() {
  userland_doctor_version_probe
  userland_mise_state=missing
  userland_bootstrap_state=unknown
  userland_adapter_state=healthy

  if [ -n "${USERLAND_MISE:-}" ] && [ -x "$USERLAND_MISE" ]; then
    userland_mise_state=present
    if "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap status --missing >/dev/null 2>&1; then
      userland_bootstrap_state=healthy
    else
      userland_bootstrap_state=drift
    fi
  fi

  if ! userland_run_adapters doctor >/dev/null 2>&1; then
    userland_adapter_state=attention
  fi

  if { [ "$userland_doctor_version_state" = current ] || [ "$userland_doctor_version_state" = ahead ]; } &&
    [ "$userland_mise_state" = present ] &&
    [ "$userland_bootstrap_state" = healthy ] &&
    [ "$userland_adapter_state" = healthy ]; then
    userland_overall=true
  else
    userland_overall=false
  fi

  printf '{"schema_version":1,"ok":%s,"checks":[' "$userland_overall"
  if [ -n "$userland_doctor_latest_version" ]; then
    printf '{"name":"userland","status":"%s","version":"%s"},' \
      "$userland_doctor_version_state" "$userland_doctor_latest_version"
  else
    printf '{"name":"userland","status":"unknown","version":null},'
  fi
  printf '{"name":"mise","status":"%s"},' "$userland_mise_state"
  printf '{"name":"bootstrap","status":"%s"},' "$userland_bootstrap_state"
  printf '{"name":"adapters","status":"%s"}' "$userland_adapter_state"
  printf ']}\n'

  [ "$userland_overall" = true ]
}

userland_doctor_human() {
  userland_doctor_mode=${1:-standalone}
  userland_require_mise
  userland_doctor_code=0

  if [ "$userland_doctor_mode" = standalone ]; then
    userland_ui command doctor "Check drift and machine health. Nothing will be changed."
  fi
  userland_mkdirs

  userland_ui section "Userland"
  userland_doctor_version_probe
  case "$userland_doctor_version_state" in
    current)
      userland_log healthy "$userland_doctor_latest_version is current"
      ;;
    ahead)
      userland_log healthy "Userland includes changes after $userland_doctor_latest_version"
      ;;
    outdated)
      userland_log attention "Userland is outdated; run userland sync"
      userland_doctor_code=1
      ;;
    unknown)
      userland_log warning "Could not check the latest Userland version"
      if [ "$userland_doctor_mode" = standalone ]; then
        userland_doctor_code=1
      fi
      ;;
  esac

  userland_ui section "Toolchain"
  userland_ui task check "Toolchain" "$USERLAND_MISE" doctor || userland_doctor_code=1

  userland_ui section "Machine state"
  USERLAND_UI_TASK_EXCERPT_PATTERN='(^|[[:space:]])(differs|missing|unknown|unavailable|failed|error)([[:space:](]|$)'
  export USERLAND_UI_TASK_EXCERPT_PATTERN
  userland_ui task check "Machine state" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap status --missing || userland_doctor_code=1
  unset USERLAND_UI_TASK_EXCERPT_PATTERN

  userland_ui section "Personal state"
  USERLAND_UI_HIDE_OK=1
  export USERLAND_UI_HIDE_OK
  userland_run_adapters doctor || userland_doctor_code=1
  unset USERLAND_UI_HIDE_OK

  if [ "$userland_doctor_code" -eq 0 ]; then
    if [ "$userland_doctor_mode" = standalone ]; then
      userland_ui summary ok "Everything matches."
    else
      userland_log healthy "Userland matches the declaration"
    fi
  else
    if [ "$userland_doctor_mode" = standalone ]; then
      userland_ui summary attention "Needs attention. Review the items above."
    else
      userland_log attention "Userland found drift or a manual step"
    fi
  fi
  return "$userland_doctor_code"
}

userland_doctor() {
  userland_doctor_mode=${1:-standalone}
  userland_require_schema
  if [ "${userland_doctor_format:-human}" = json ]; then
    userland_doctor_json
  else
    userland_doctor_human "$userland_doctor_mode"
  fi
}
