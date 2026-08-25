#!/bin/sh
set -eu

USERLAND_ROOT=${USERLAND_ORACLE_ROOT:-$USERLAND_ROOT}
export USERLAND_ROOT

. "$USERLAND_ROOT/lib/common.sh"
. "$USERLAND_ROOT/lib/plan-ledger.sh"
userland_ui_prepare_stream

USERLAND_PLAN_CSV=${USERLAND_COMPAT_ROOT:-$USERLAND_ROOT}/tests/compat/plan.csv
USERLAND_PLAN_FILE=${TEST_TMPDIR:-${TMPDIR:-/tmp}}/userland-oracle-plan.$$.tsv
trap 'rm -f "$USERLAND_PLAN_FILE"' EXIT HUP INT TERM
python3 - "$USERLAND_PLAN_CSV" "$USERLAND_PLAN_FILE" <<'PY'
import csv
import sys

with open(sys.argv[1], newline="", encoding="utf-8") as source:
    rows = csv.reader(source)
    next(rows)
    with open(sys.argv[2], "w", encoding="utf-8") as target:
        for row in rows:
            if any("\t" in value or "\n" in value for value in row):
                raise ValueError("legacy plan fixture cannot contain tabs or newlines")
            target.write("\t".join(row) + "\n")
PY
USERLAND_PLAN_ACTIVE=1
USERLAND_PLAN_COLLECTING=1
export USERLAND_PLAN_FILE USERLAND_PLAN_ACTIVE USERLAND_PLAN_COLLECTING
userland_ui_ensure_run_log
userland_plan_render
