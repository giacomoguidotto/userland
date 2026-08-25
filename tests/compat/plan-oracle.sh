#!/bin/sh
set -eu

USERLAND_ROOT=${USERLAND_ORACLE_ROOT:-$USERLAND_ROOT}
export USERLAND_ROOT

. "$USERLAND_ROOT/lib/common.sh"
. "$USERLAND_ROOT/lib/plan-ledger.sh"
userland_ui_prepare_stream

USERLAND_PLAN_FILE=${USERLAND_COMPAT_ROOT:-$USERLAND_ROOT}/tests/compat/plan.tsv
USERLAND_PLAN_ACTIVE=1
USERLAND_PLAN_COLLECTING=1
export USERLAND_PLAN_FILE USERLAND_PLAN_ACTIVE USERLAND_PLAN_COLLECTING
userland_ui_ensure_run_log
userland_plan_render
