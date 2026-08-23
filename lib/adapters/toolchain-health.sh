#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_tool_probes=$USERLAND_ROOT/config/tool-probes.tsv
userland_public_mise=$USERLAND_HOME/.local/bin/mise

if [ "${USERLAND_TESTING:-0}" = 1 ] && [ "${TEST_TOOLCHAIN_HEALTH:-0}" != 1 ]; then
  case "${1:-}" in
    plan) userland_log current 'test toolchain fixture is healthy' ;;
    apply) exit 0 ;;
    doctor) userland_log healthy 'test toolchain fixture is healthy' ;;
    *) userland_die 'toolchain-health adapter expects plan, apply, or doctor' 64 ;;
  esac
  exit 0
fi

userland_toolchain_declared_tools() {
  awk '
    /^\[tools\]$/ { inside = 1; next }
    /^\[/ { inside = 0 }
    inside && /^[[:space:]]*[A-Za-z0-9_:-]+[[:space:]]*=/ {
      line = $0
      sub(/^[[:space:]]*/, "", line)
      sub(/[[:space:]]*=.*/, "", line)
      print line
    }
    inside && /^[[:space:]]*"[^"]+"[[:space:]]*=/ {
      line = $0
      sub(/^[[:space:]]*"/, "", line)
      sub(/"[[:space:]]*=.*/, "", line)
      print line
    }
  ' "$USERLAND_ROOT/mise.toml"
}

userland_toolchain_manifest_is_complete() {
  userland_toolchain_declared=$(mktemp "$USERLAND_CACHE_DIR/toolchain-declared.XXXXXX")
  userland_toolchain_probed=$(mktemp "$USERLAND_CACHE_DIR/toolchain-probed.XXXXXX")
  userland_toolchain_declared_tools | sort -u >"$userland_toolchain_declared"
  awk -F '\t' '$1 !~ /^#/ && NF >= 3 { print $1 }' "$userland_tool_probes" | sort -u >"$userland_toolchain_probed"
  if cmp -s "$userland_toolchain_declared" "$userland_toolchain_probed"; then
    userland_toolchain_manifest_result=0
  else
    userland_toolchain_manifest_result=1
  fi
  rm -f "$userland_toolchain_declared" "$userland_toolchain_probed"
  return "$userland_toolchain_manifest_result"
}

userland_toolchain_public_mise_is_current() {
  [ -x "$userland_public_mise" ] || return 1
  userland_toolchain_pinned_version=$($USERLAND_MISE --version 2>/dev/null | awk 'NR == 1 { print $1 }')
  userland_toolchain_public_version=$($userland_public_mise --version 2>/dev/null | awk 'NR == 1 { print $1 }') || return 1
  [ -n "$userland_toolchain_pinned_version" ] &&
    [ "$userland_toolchain_public_version" = "$userland_toolchain_pinned_version" ]
}

userland_toolchain_probe_pinned() {
  userland_toolchain_probe_command=$1
  shift
  MISE_QUIET=true "$USERLAND_MISE" -C "$USERLAND_ROOT" exec -- \
    "$userland_toolchain_probe_command" "$@" </dev/null >/dev/null 2>&1
}

userland_toolchain_probe_global() {
  userland_toolchain_probe_command=$1
  shift
  userland_toolchain_shim=$USERLAND_HOME/.local/share/mise/shims/$userland_toolchain_probe_command
  [ -x "$userland_toolchain_shim" ] || return 1
  (
    cd "$USERLAND_HOME"
    HOME=$USERLAND_HOME
    PATH="$USERLAND_HOME/.local/share/mise/shims:$USERLAND_HOME/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin"
    export HOME PATH
    "$userland_toolchain_shim" "$@" </dev/null >/dev/null 2>&1
  )
}

userland_toolchain_plan_item() {
  userland_toolchain_area=$1
  userland_toolchain_action=$2
  userland_toolchain_target=$3
  userland_toolchain_detail=$4
  userland_toolchain_proof=$5
  if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ] && command -v userland_plan_add >/dev/null 2>&1; then
    userland_plan_add "$userland_toolchain_area" "$userland_toolchain_action" automatic declared \
      "$userland_toolchain_target" "$userland_toolchain_detail" "$userland_toolchain_proof"
  else
    userland_log change "$userland_toolchain_target: $userland_toolchain_detail"
  fi
}

userland_toolchain_each_problem() {
  userland_toolchain_problem_action=$1
  userland_toolchain_problem_count=0
  userland_toolchain_global_probe_ready=1

  if ! userland_toolchain_public_mise_is_current; then
    "$userland_toolchain_problem_action" mise promote launcher
    userland_toolchain_problem_count=$((userland_toolchain_problem_count + 1))
    userland_toolchain_global_probe_ready=0
  fi

  if ! userland_toolchain_manifest_is_complete; then
    "$userland_toolchain_problem_action" probe-manifest review tool-probes
    userland_toolchain_problem_count=$((userland_toolchain_problem_count + 1))
    return 0
  fi

  userland_toolchain_probe_dir=$(mktemp -d "$USERLAND_CACHE_DIR/toolchain-probes.XXXXXX")
  userland_toolchain_probe_index=0
  while IFS="$(printf '\t')" read -r userland_tool_id userland_tool_command userland_tool_arguments; do
    case "$userland_tool_id" in '' | \#*) continue ;; esac
    userland_toolchain_probe_index=$((userland_toolchain_probe_index + 1))
    (
      # Probe arguments are repository-owned literal words, never shell code.
      set -- $userland_tool_arguments
      if ! userland_toolchain_probe_pinned "$userland_tool_command" "$@"; then
        printf 'reinstall\n'
      elif [ "$userland_toolchain_global_probe_ready" -eq 1 ] &&
        ! userland_toolchain_probe_global "$userland_tool_command" "$@"; then
        printf 'activate\n'
      fi
    ) >"$userland_toolchain_probe_dir/$userland_toolchain_probe_index" &
  done <"$userland_tool_probes"
  wait

  userland_toolchain_probe_index=0
  while IFS="$(printf '\t')" read -r userland_tool_id userland_tool_command _; do
    case "$userland_tool_id" in '' | \#*) continue ;; esac
    userland_toolchain_probe_index=$((userland_toolchain_probe_index + 1))
    userland_toolchain_repair=$(sed -n '1p' "$userland_toolchain_probe_dir/$userland_toolchain_probe_index")
    if [ -n "$userland_toolchain_repair" ]; then
      "$userland_toolchain_problem_action" "$userland_tool_id" "$userland_toolchain_repair" "$userland_tool_command"
      userland_toolchain_problem_count=$((userland_toolchain_problem_count + 1))
    fi
  done <"$userland_tool_probes"
  userland_toolchain_probe_count=$userland_toolchain_probe_index
  userland_toolchain_probe_index=0
  while [ "$userland_toolchain_probe_index" -lt "$userland_toolchain_probe_count" ]; do
    userland_toolchain_probe_index=$((userland_toolchain_probe_index + 1))
    rm -f "$userland_toolchain_probe_dir/$userland_toolchain_probe_index"
  done
  rmdir "$userland_toolchain_probe_dir"

  [ "$userland_toolchain_problem_count" -eq 0 ]
}

userland_toolchain_render_plan_problem() {
  userland_tool_id=$1
  userland_tool_repair=$2
  userland_tool_command=$3
  case "$userland_tool_repair" in
    promote)
      userland_toolchain_plan_item fs update "$userland_public_mise" \
        'atomically promote the pinned Userland mise launcher' 'toolchain:mise-launcher'
      ;;
    reinstall)
      userland_toolchain_plan_item apps update "$userland_tool_command" \
        "reinstall only the corrupted pinned tool $userland_tool_id" "toolchain:reinstall:$userland_tool_id"
      ;;
    activate)
      userland_toolchain_plan_item fs update "$userland_tool_command shim" \
        "activate pinned $userland_tool_id in clean global shells" "toolchain:activate:$userland_tool_id"
      ;;
    review)
      if [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ] && command -v userland_plan_add >/dev/null 2>&1; then
        userland_plan_add apps review blocked declared 'tool probe manifest' \
          'add one executable probe for every pinned mise tool' 'toolchain:probe-manifest'
      else
        userland_log attention 'tool probe manifest does not match the pinned mise declaration'
      fi
      ;;
  esac
}

userland_toolchain_render_doctor_problem() {
  userland_tool_id=$1
  userland_tool_repair=$2
  userland_tool_command=$3
  case "$userland_tool_repair" in
    promote) userland_log attention 'public mise launcher is older than Userland pinned mise' ;;
    reinstall) userland_log attention "$userland_tool_command cannot execute; pinned $userland_tool_id needs reinstall" ;;
    activate) userland_log attention "$userland_tool_command is not executable through the clean global shim" ;;
    review) userland_log attention 'tool probe manifest does not cover every pinned mise tool' ;;
  esac
}

userland_toolchain_promote_mise() {
  userland_toolchain_public_dir=${userland_public_mise%/*}
  mkdir -p "$userland_toolchain_public_dir"
  [ ! -L "$userland_toolchain_public_dir" ] || userland_die 'refusing to promote mise through a symlinked ~/.local/bin'
  # mise dispatches as a shim when argv[0] is not named `mise`. Keep the
  # validation copy on the destination filesystem for an atomic rename, but
  # give the executable its canonical basename inside a private directory.
  userland_toolchain_mise_tmp_dir=$(mktemp -d "$userland_toolchain_public_dir/.mise.userland.XXXXXX")
  userland_toolchain_mise_tmp=$userland_toolchain_mise_tmp_dir/mise
  cp "$USERLAND_MISE" "$userland_toolchain_mise_tmp"
  chmod 755 "$userland_toolchain_mise_tmp"
  "$userland_toolchain_mise_tmp" --version >/dev/null 2>&1 || {
    rm -f "$userland_toolchain_mise_tmp"
    rmdir "$userland_toolchain_mise_tmp_dir"
    userland_die 'pinned mise launcher failed validation before promotion'
  }
  mv -f "$userland_toolchain_mise_tmp" "$userland_public_mise"
  rmdir "$userland_toolchain_mise_tmp_dir"
  userland_log changed 'promoted the pinned mise launcher atomically'
}

userland_toolchain_apply() {
  if ! userland_toolchain_public_mise_is_current; then
    userland_toolchain_promote_mise
  fi
  userland_toolchain_manifest_is_complete || userland_die 'tool probe manifest is incomplete'

  userland_toolchain_reinstalled=0
  while IFS="$(printf '\t')" read -r userland_tool_id userland_tool_command userland_tool_arguments; do
    case "$userland_tool_id" in '' | \#*) continue ;; esac
    set -- $userland_tool_arguments
    if ! userland_toolchain_probe_pinned "$userland_tool_command" "$@"; then
      userland_log changed "reinstalling affected pinned tool: $userland_tool_id"
      MISE_QUIET=true "$USERLAND_MISE" -C "$USERLAND_ROOT" install --force --yes "$userland_tool_id"
      userland_toolchain_probe_pinned "$userland_tool_command" "$@" ||
        userland_die "$userland_tool_command still cannot execute after targeted reinstall"
      userland_toolchain_reinstalled=1
    fi
  done <"$userland_tool_probes"
  [ "$userland_toolchain_reinstalled" -eq 0 ] || MISE_QUIET=true "$USERLAND_MISE" reshim
}

case "${1:-}" in
  plan)
    if userland_toolchain_each_problem userland_toolchain_render_plan_problem; then
      userland_log current 'every pinned tool executes and is active in clean global shells'
    fi
    exit 0
    ;;
  apply)
    userland_toolchain_apply
    ;;
  doctor)
    if userland_toolchain_each_problem userland_toolchain_render_doctor_problem; then
      userland_log healthy 'every pinned tool executes and is active in clean global shells'
    else
      exit 2
    fi
    ;;
  *)
    userland_die 'toolchain-health adapter expects plan, apply, or doctor' 64
    ;;
esac
