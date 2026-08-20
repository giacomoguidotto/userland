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
  userland_doctor_mode=${1:-standalone}
  userland_require_mise
  userland_doctor_code=0

  if [ "$userland_doctor_mode" = standalone ]; then
    userland_ui command doctor "Check drift and machine health. Nothing will be changed."
  fi
  userland_mkdirs

  userland_ui section "Toolchain"
  userland_ui task check "Toolchain" "$USERLAND_MISE" doctor || userland_doctor_code=1

  userland_ui section "Machine state"
  userland_ui task check "Machine state" \
    "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap status --missing || userland_doctor_code=1

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
