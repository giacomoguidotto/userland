#!/bin/sh

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_doctor_json() {
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

  if [ "$userland_mise_state" = present ] && [ "$userland_bootstrap_state" = healthy ] && [ "$userland_adapter_state" = healthy ]; then
    userland_overall=true
  else
    userland_overall=false
  fi

  printf '{"schema_version":1,"ok":%s,"checks":[' "$userland_overall"
  printf '{"name":"mise","status":"%s"},' "$userland_mise_state"
  printf '{"name":"bootstrap","status":"%s"},' "$userland_bootstrap_state"
  printf '{"name":"adapters","status":"%s"}' "$userland_adapter_state"
  printf ']}\n'

  [ "$userland_overall" = true ]
}

userland_doctor_human() {
  userland_require_mise
  userland_doctor_code=0

  userland_log doctor "mise installation"
  "$USERLAND_MISE" doctor || userland_doctor_code=1

  userland_log doctor "declared machine state"
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap status --missing || userland_doctor_code=1

  userland_log doctor "userland adapters"
  userland_run_adapters doctor || userland_doctor_code=1

  if [ "$userland_doctor_code" -eq 0 ]; then
    userland_log healthy "userland matches the declaration"
  else
    userland_log attention "userland found drift or a manual step"
  fi
  return "$userland_doctor_code"
}

userland_doctor() {
  userland_require_schema
  if [ "${userland_doctor_format:-human}" = json ]; then
    userland_doctor_json
  else
    userland_doctor_human
  fi
}
