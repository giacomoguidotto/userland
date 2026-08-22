#!/bin/sh

if [ -n "${USERLAND_UI_LOADED:-}" ]; then
  return 0
fi
USERLAND_UI_LOADED=1

: "${USERLAND_UI_MODE:=auto}"
case "$USERLAND_UI_MODE" in auto | rich | plain) ;; *) USERLAND_UI_MODE=plain ;; esac
userland_ui_escape=$(printf '\033')
userland_ui_backslash=$(printf '\\')

userland_ui_prepare_stream() {
  case "$USERLAND_UI_MODE" in
    auto)
      if [ -t 1 ] && [ "${TERM:-}" != dumb ] && [ -z "${CI:-}" ]; then
        userland_ui_active_mode=rich
      else
        userland_ui_active_mode=plain
      fi
      ;;
    *) userland_ui_active_mode=$USERLAND_UI_MODE ;;
  esac
  if [ "$userland_ui_active_mode" = rich ]; then
    userland_ui_margin=' '
  else
    userland_ui_margin=
  fi

  userland_ui_color=0
  if [ -z "${NO_COLOR:-}" ] && [ "${CLICOLOR:-1}" != 0 ] && [ "${TERM:-}" != dumb ]; then
    if [ "$userland_ui_active_mode" = rich ] || { [ -n "${CLICOLOR_FORCE:-}" ] && [ "${CLICOLOR_FORCE:-0}" != 0 ]; }; then
      userland_ui_color=1
    fi
  fi
  if [ "$userland_ui_color" -eq 1 ]; then
    userland_ui_reset="${userland_ui_escape}[0m"
    userland_ui_bold="${userland_ui_escape}[1m"
    userland_ui_dim="${userland_ui_escape}[2m"
    userland_ui_green="${userland_ui_escape}[32m"
    userland_ui_yellow="${userland_ui_escape}[33m"
    userland_ui_red="${userland_ui_escape}[31m"
    userland_ui_cyan="${userland_ui_escape}[36m"
  else
    userland_ui_reset=
    userland_ui_bold=
    userland_ui_dim=
    userland_ui_green=
    userland_ui_yellow=
    userland_ui_red=
    userland_ui_cyan=
  fi

  if [ -n "${USERLAND_UNICODE+x}" ]; then
    userland_ui_unicode=$USERLAND_UNICODE
  else
    userland_ui_locale=${LC_ALL:-${LC_CTYPE:-${LANG:-}}}
    case "$userland_ui_locale" in
      *[Uu][Tt][Ff]-8* | *[Uu][Tt][Ff]8*) userland_ui_unicode=1 ;;
      *) userland_ui_unicode=0 ;;
    esac
  fi
  if [ "$userland_ui_active_mode" = rich ] && [ "$userland_ui_unicode" != 0 ]; then
    userland_ui_rail='│'
    userland_ui_open='┌'
    userland_ui_close='└'
    userland_ui_done='◇'
    userland_ui_section='◆'
    userland_ui_ok_symbol='✓'
    userland_ui_change_symbol='+'
    userland_ui_attention_symbol='!'
    userland_ui_error_symbol='×'
    userland_ui_info_symbol='·'
    userland_ui_cancel_symbol='–'
  else
    userland_ui_rail='|'
    userland_ui_open='+'
    userland_ui_close='`'
    userland_ui_done='o'
    userland_ui_section='*'
    userland_ui_ok_symbol='ok'
    userland_ui_change_symbol='+'
    userland_ui_attention_symbol='!'
    userland_ui_error_symbol='x'
    userland_ui_info_symbol='-'
    userland_ui_cancel_symbol='-'
  fi
}

userland_ui_wordmark() {
  if [ "$userland_ui_unicode" != 0 ]; then
    printf '%s%s▗▖ ▗▖ ▗▄▄▖▗▄▄▄▖▗▄▄▖ ▗▖    ▗▄▖ ▗▖  ▗▖▗▄▄▄%s\n' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset"
    printf '%s%s▐▌ ▐▌▐▌   ▐▌   ▐▌ ▐▌▐▌   ▐▌ ▐▌▐▛▚▖▐▌▐▌  █%s\n' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset"
    printf '%s%s▐▌ ▐▌ ▝▀▚▖▐▛▀▀▘▐▛▀▚▖▐▌   ▐▛▀▜▌▐▌ ▝▜▌▐▌  █%s\n' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset"
    printf '%s%s▝▚▄▞▘▗▄▄▞▘▐▙▄▄▖▐▌ ▐▌▐▙▄▄▖▐▌ ▐▌▐▌  ▐▌▐▙▄▄▀%s\n' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset"
  else
    printf '%s%s+-- USERLAND --+%s\n' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset"
  fi
}

userland_ui_redact() {
  userland_ui_redact_rest=$1
  userland_ui_text=
  while :; do
    case "$userland_ui_redact_rest" in
      *"$USERLAND_HOME"*)
        userland_ui_redact_prefix=${userland_ui_redact_rest%%"$USERLAND_HOME"*}
        userland_ui_text=$userland_ui_text$userland_ui_redact_prefix'~'
        userland_ui_redact_rest=${userland_ui_redact_rest#*"$USERLAND_HOME"}
        ;;
      *)
        userland_ui_text=$userland_ui_text$userland_ui_redact_rest
        break
        ;;
    esac
  done
}

userland_ui_ensure_run_log() {
  [ -z "${USERLAND_UI_RUN_LOG:-}" ] || return 0
  mkdir -p "$USERLAND_CACHE_DIR" "$USERLAND_STATE_DIR"
  USERLAND_UI_RUN_LOG=$USERLAND_STATE_DIR/last-run.log
  : >"$USERLAND_UI_RUN_LOG"
  chmod 600 "$USERLAND_UI_RUN_LOG"
  export USERLAND_UI_RUN_LOG
}

userland_ui_clear_active() {
  if [ "${userland_ui_active_row:-0}" = 1 ]; then
    printf '\r%s[2K' "$userland_ui_escape"
    userland_ui_active_row=0
  fi
}

userland_ui_cleanup() {
  userland_ui_clear_active
  if [ -n "${userland_ui_child_pid:-}" ]; then
    kill "$userland_ui_child_pid" 2>/dev/null || :
  fi
  userland_ui_preserve_interrupted_task
  for userland_ui_cleanup_file in \
    "${userland_ui_task_log:-}" \
    "${userland_ui_task_status:-}" \
    "${userland_ui_progress_file:-}" \
    "${USERLAND_PLAN_RESULT:-}" \
    "${USERLAND_PLAN_FILE:-}"; do
    [ -n "$userland_ui_cleanup_file" ] && [ -f "$userland_ui_cleanup_file" ] && rm -f "$userland_ui_cleanup_file"
  done
  return 0
}

userland_ui_preserve_interrupted_task() {
  [ -n "${userland_ui_task_log:-}" ] && [ -f "$userland_ui_task_log" ] || return 0
  [ -n "${USERLAND_UI_RUN_LOG:-}" ] || return 0
  {
    printf '\n## %s (interrupted)\n' "${userland_ui_task_label:-Task}"
    cat "$userland_ui_task_log"
  } >>"$USERLAND_UI_RUN_LOG"
}

userland_ui_exit() {
  userland_ui_exit_code=$?
  userland_ui_cleanup
  trap - EXIT HUP INT TERM
  exit "$userland_ui_exit_code"
}

userland_ui_signal() {
  userland_ui_signal_code=$1
  if [ "$userland_ui_signal_code" -eq 130 ] && [ "${USERLAND_BOOTSTRAP_CREATED:-0}" != 1 ]; then
    userland_ui_clear_active
    userland_ui_status cancelled "Cancelled."
  fi
  userland_ui_cleanup
  trap - EXIT HUP INT TERM
  exit "$userland_ui_signal_code"
}

userland_ui_task_excerpt() {
  tail -n 6 "$1" | LC_ALL=C tr -cd '\11\12\40-\176' | awk \
    -v margin="$userland_ui_margin" \
    -v rail="$userland_ui_rail" '
    {
      gsub(/\r/, "")
      if (length($0) > 72) $0 = substr($0, 1, 69) "..."
      print margin rail "  " $0
    }
  '
}

userland_ui_spinner_frame() {
  case $((userland_ui_spinner_tick % 10)) in
    0) userland_ui_spinner_glyph='⠋' ;;
    1) userland_ui_spinner_glyph='⠙' ;;
    2) userland_ui_spinner_glyph='⠹' ;;
    3) userland_ui_spinner_glyph='⠸' ;;
    4) userland_ui_spinner_glyph='⠼' ;;
    5) userland_ui_spinner_glyph='⠴' ;;
    6) userland_ui_spinner_glyph='⠦' ;;
    7) userland_ui_spinner_glyph='⠧' ;;
    8) userland_ui_spinner_glyph='⠇' ;;
    9) userland_ui_spinner_glyph='⠏' ;;
  esac
  if [ "$userland_ui_unicode" = 0 ]; then
    case $((userland_ui_spinner_tick % 4)) in
      0) userland_ui_spinner_glyph='-' ;;
      1) userland_ui_spinner_glyph=$userland_ui_backslash ;;
      2) userland_ui_spinner_glyph='|' ;;
      3) userland_ui_spinner_glyph='/' ;;
    esac
  fi
}

userland_ui_format_duration() {
  userland_ui_duration_seconds=$1
  if [ "$userland_ui_duration_seconds" -ge 3600 ]; then
    userland_ui_duration_text="$((userland_ui_duration_seconds / 3600))h $(((userland_ui_duration_seconds % 3600) / 60))m"
  elif [ "$userland_ui_duration_seconds" -ge 60 ]; then
    userland_ui_duration_text="$((userland_ui_duration_seconds / 60))m $((userland_ui_duration_seconds % 60))s"
  elif [ "$userland_ui_duration_seconds" -gt 0 ]; then
    userland_ui_duration_text="${userland_ui_duration_seconds}s"
  else
    userland_ui_duration_text='<1s'
  fi
}

userland_ui_progress_prepare() {
  userland_ui_progress_file=
  userland_ui_progress_total=0
  case "${USERLAND_UI_PROGRESS:-}" in
    mise-install)
      userland_ui_progress_kind=mise
      userland_ui_progress_action=install
      ;;
    mise-upgrade)
      userland_ui_progress_kind=mise
      userland_ui_progress_action=upgrade
      ;;
    homebrew-install)
      userland_ui_progress_kind=homebrew
      userland_ui_progress_action=install
      ;;
    *) return 0 ;;
  esac
  [ -n "${USERLAND_PLAN_FILE:-}" ] && [ -f "$USERLAND_PLAN_FILE" ] || return 0

  userland_ui_progress_file=$(mktemp "$USERLAND_CACHE_DIR/progress.XXXXXX")
  if [ "$userland_ui_progress_kind" = mise ]; then
    userland_ui_progress_source='^mise:package:'
  else
    userland_ui_progress_source='^brewfile:'
  fi
  awk -F '\t' -v action="$userland_ui_progress_action" -v source="$userland_ui_progress_source" '
    $1 == "apps" && $2 == action && $7 ~ source && !seen[$5]++ { print $5 }
  ' "$USERLAND_PLAN_FILE" >"$userland_ui_progress_file"
  userland_ui_progress_total=$(wc -l <"$userland_ui_progress_file" | tr -d ' ')
}

userland_ui_progress_refresh() {
  userland_ui_progress_now=$(date +%s)
  userland_ui_format_duration "$((userland_ui_progress_now - userland_ui_spinner_started_at))"
  userland_ui_spinner_detail=$userland_ui_duration_text
  [ "${userland_ui_progress_total:-0}" -gt 0 ] || return 0
  [ -n "${userland_ui_task_log:-}" ] && [ -f "$userland_ui_task_log" ] || return 0

  if [ "${userland_ui_progress_kind:-}" = homebrew ]; then
    userland_ui_progress_state=$(awk '
      NR == FNR { planned[$0] = 1; next }
      /^(Tapping|Installing|Upgrading) / {
        package = $0
        sub(/^[^ ]+ /, "", package)
        if (planned[package]) {
          current = package
          if (!seen[package]++) count++
        }
      }
      END {
        if (current == "") current = "-"
        printf "%d\t%s\t-\n", count, current
      }
    ' "$userland_ui_progress_file" "$userland_ui_task_log")
  else
    userland_ui_progress_state=$(awk '
    NR == FNR { planned[$0] = 1; next }
    {
      package = ""
      phase = ""
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^brew:/) {
          package = $i
          sub(/^brew:/, "", package)
        }
        if ($i == "download" || $i == "checksum" || $i == "verify" ||
            $i == "extract" || $i == "install" || $i == "relocate" ||
            $i == "codesign" || $i == "link") phase = $i
        if ($i == "✓" || $i == "done") phase = "done"
      }
      if (package != "") {
        current = package
        current_phase = phase == "" ? "working" : phase
        if (phase == "done" && planned[package]) completed[package] = 1
      }
    }
    END {
      count = 0
      for (package in completed) count++
      if (current == "") current = "-"
      if (current_phase == "") current_phase = "-"
      printf "%d\t%s\t%s\n", count, current, current_phase
    }
    ' "$userland_ui_progress_file" "$userland_ui_task_log")
  fi
  userland_ui_progress_tab=$(printf '\t')
  userland_ui_progress_completed=${userland_ui_progress_state%%"$userland_ui_progress_tab"*}
  userland_ui_progress_rest=${userland_ui_progress_state#*"$userland_ui_progress_tab"}
  userland_ui_progress_target=${userland_ui_progress_rest%%"$userland_ui_progress_tab"*}
  userland_ui_progress_phase=${userland_ui_progress_rest#*"$userland_ui_progress_tab"}
  userland_ui_spinner_detail="$userland_ui_duration_text · $userland_ui_progress_completed/$userland_ui_progress_total"
  if [ "$userland_ui_progress_target" != - ]; then
    userland_ui_spinner_detail="$userland_ui_spinner_detail · $userland_ui_progress_target"
  fi
  if [ "$userland_ui_progress_phase" != - ]; then
    userland_ui_spinner_detail="$userland_ui_spinner_detail · $userland_ui_progress_phase"
  fi
}

userland_ui_spin() {
  userland_ui_spinner_label=$1
  userland_ui_spinner_tick=0
  userland_ui_spinner_started_at=$(date +%s)
  userland_ui_progress_refresh
  while kill -0 "$userland_ui_child_pid" 2>/dev/null; do
    if [ $((userland_ui_spinner_tick % 8)) -eq 0 ]; then
      userland_ui_progress_refresh
    fi
    userland_ui_spinner_frame
    printf '\r%s[2K%s%s%s%s  %s… %s%s%s' \
      "$userland_ui_escape" \
      "$userland_ui_margin" \
      "$userland_ui_cyan" \
      "$userland_ui_spinner_glyph" \
      "$userland_ui_reset" \
      "$userland_ui_spinner_label" \
      "$userland_ui_dim" \
      "$userland_ui_spinner_detail" \
      "$userland_ui_reset"
    userland_ui_active_row=1
    userland_ui_spinner_tick=$((userland_ui_spinner_tick + 1))
    sleep 0.12
  done
}

userland_ui_task() {
  userland_ui_task_kind=$1
  userland_ui_task_label=$2
  shift 2
  case "$userland_ui_task_kind" in inspect | collect | apply | check) ;; *) return 64 ;; esac
  [ "$#" -gt 0 ] || return 64

  userland_ui_ensure_run_log
  userland_ui_task_log=$(mktemp "$USERLAND_CACHE_DIR/task.XXXXXX")
  userland_ui_task_status=$userland_ui_task_log.status
  : >"$userland_ui_task_status"
  chmod 600 "$userland_ui_task_log" "$userland_ui_task_status"
  userland_ui_progress_prepare

  if [ "$userland_ui_active_mode" = rich ]; then
    (
      set +e
      if [ "$userland_ui_task_kind" = inspect ]; then
        USERLAND_PLAN_COLLECTING=0
        export USERLAND_PLAN_COLLECTING
      fi
      "$@"
      userland_ui_child_code=$?
      printf '%s\n' "$userland_ui_child_code" >"$userland_ui_task_status"
      exit 0
    ) >"$userland_ui_task_log" 2>&1 &
    userland_ui_child_pid=$!
    userland_ui_spin "$userland_ui_task_label"
    wait "$userland_ui_child_pid" 2>/dev/null || :
    unset userland_ui_child_pid
    userland_ui_clear_active
  else
    userland_ui_status info "$userland_ui_task_label"
    (
      set +e
      if [ "$userland_ui_task_kind" = inspect ]; then
        USERLAND_PLAN_COLLECTING=0
        export USERLAND_PLAN_COLLECTING
      fi
      "$@"
      userland_ui_child_code=$?
      printf '%s\n' "$userland_ui_child_code" >"$userland_ui_task_status"
      exit 0
    ) 2>&1 | tee "$userland_ui_task_log"
  fi

  {
    printf '\n## %s\n' "$userland_ui_task_label"
    cat "$userland_ui_task_log"
  } >>"$USERLAND_UI_RUN_LOG"
  userland_ui_task_code=1
  IFS= read -r userland_ui_task_code <"$userland_ui_task_status" || userland_ui_task_code=1
  rm -f "$userland_ui_task_status"

  if [ "$userland_ui_task_code" -eq 0 ]; then
    if [ "$userland_ui_active_mode" = rich ]; then
      printf '%s%s%s%s  %s\n' "$userland_ui_margin" "$userland_ui_green" "$userland_ui_done" "$userland_ui_reset" "$userland_ui_task_label"
    fi
    rm -f "$userland_ui_task_log"
    [ -z "${userland_ui_progress_file:-}" ] || rm -f "$userland_ui_progress_file"
    unset userland_ui_task_log userland_ui_task_status userland_ui_progress_file
    return 0
  fi

  if { [ "$userland_ui_task_kind" = check ] && { [ "$userland_ui_task_code" -eq 1 ] || [ "$userland_ui_task_code" -eq 2 ]; }; } ||
    { [ "$userland_ui_task_kind" = apply ] && [ "$userland_ui_task_code" -eq 2 ]; }; then
    userland_ui_status attention "$userland_ui_task_label"
  else
    userland_ui_status error "$userland_ui_task_label failed (exit $userland_ui_task_code)"
  fi
  userland_ui_task_excerpt "$userland_ui_task_log"
  userland_ui_status info "Log: $USERLAND_UI_RUN_LOG"
  rm -f "$userland_ui_task_log"
  [ -z "${userland_ui_progress_file:-}" ] || rm -f "$userland_ui_progress_file"
  unset userland_ui_task_log userland_ui_task_status userland_ui_progress_file
  return "$userland_ui_task_code"
}

userland_ui_status() {
  userland_ui_prepare_stream
  userland_ui_state=$1
  shift
  userland_ui_redact "$*"
  if [ "${USERLAND_UI_HIDE_OK:-0}" = 1 ] && [ "$userland_ui_active_mode" = rich ]; then
    case "$userland_ui_state" in ok | info) return 0 ;; esac
  fi
  case "$userland_ui_state" in
    ok | changed)
      userland_ui_symbol=$userland_ui_ok_symbol
      userland_ui_tint=$userland_ui_green
      userland_ui_plain_state=ok
      ;;
    done)
      userland_ui_symbol=$userland_ui_done
      userland_ui_tint=$userland_ui_green
      userland_ui_plain_state=ok
      ;;
    change)
      userland_ui_symbol=$userland_ui_change_symbol
      userland_ui_tint=$userland_ui_cyan
      userland_ui_plain_state=change
      ;;
    attention | manual | warning)
      userland_ui_symbol=$userland_ui_attention_symbol
      userland_ui_tint=$userland_ui_yellow
      userland_ui_plain_state=$userland_ui_state
      ;;
    error)
      userland_ui_symbol=$userland_ui_error_symbol
      userland_ui_tint=$userland_ui_red
      userland_ui_plain_state=error
      ;;
    cancelled)
      userland_ui_symbol=$userland_ui_cancel_symbol
      userland_ui_tint=$userland_ui_dim
      userland_ui_plain_state=cancelled
      ;;
    info)
      userland_ui_symbol=$userland_ui_info_symbol
      userland_ui_tint=$userland_ui_dim
      userland_ui_plain_state=info
      ;;
    *) return 64 ;;
  esac
  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s%s%s%s  %s\n' "$userland_ui_margin" "$userland_ui_tint" "$userland_ui_symbol" "$userland_ui_reset" "$userland_ui_text"
  else
    printf '[%s] %s\n' "$userland_ui_plain_state" "$userland_ui_text"
  fi
}

userland_ui_elapsed() {
  userland_ui_elapsed_seconds=0
  if [ -n "${userland_ui_started_at:-}" ]; then
    userland_ui_finished_at=$(date +%s)
    userland_ui_elapsed_seconds=$((userland_ui_finished_at - userland_ui_started_at))
  fi
  userland_ui_format_duration "$userland_ui_elapsed_seconds"
  userland_ui_elapsed_text=$userland_ui_duration_text
}

userland_ui_usage() {
  if [ "$userland_ui_active_mode" = rich ]; then
    userland_ui_wordmark
    printf '\n'
    printf '%sPersonal macOS state, kept in sync.%s\n\n' "$userland_ui_dim" "$userland_ui_reset"
    printf '%sUsage%s\n  userland <command>\n\n' "$userland_ui_bold" "$userland_ui_reset"
    printf '%sCommands%s\n' "$userland_ui_bold" "$userland_ui_reset"
  else
    printf 'userland\nPersonal macOS state, kept in sync.\n\nUsage\n  userland <command>\n\nCommands\n'
  fi
  printf '  plan      Preview what would change\n'
  printf '  sync      Update, apply, and verify declared state\n'
  printf '  doctor    Check drift and machine health\n\n'
  printf 'Automation\n  userland doctor --json\n'
}

userland_ui_confirm() {
  userland_ui_redact "$*"
  if [ "${USERLAND_ASSUME_YES:-}" = 1 ]; then
    userland_ui_status info "$userland_ui_text yes"
    return 0
  fi

  if [ "${USERLAND_TESTING:-0}" = 1 ] && [ -n "${USERLAND_UI_TEST_CONFIRMATION+x}" ]; then
    userland_ui_confirm_output=/dev/stdout
    userland_ui_confirmation=$USERLAND_UI_TEST_CONFIRMATION
  elif [ ! -t 0 ] || [ ! -r /dev/tty ]; then
    userland_ui_status error "$userland_ui_text requires an interactive terminal"
    return 1
  else
    userland_ui_confirm_output=/dev/tty
  fi

  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s%s?%s  %s %s[y/N]%s %s›%s ' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset" "$userland_ui_text" "$userland_ui_dim" "$userland_ui_reset" "$userland_ui_cyan" "$userland_ui_reset" >"$userland_ui_confirm_output"
  else
    printf '%s [y/N] ' "$userland_ui_text" >"$userland_ui_confirm_output"
  fi

  if [ "$userland_ui_confirm_output" = /dev/tty ]; then
    IFS= read -r userland_ui_confirmation </dev/tty || userland_ui_confirmation=
  fi

  case "$userland_ui_confirmation" in
    y | Y | yes | YES)
      userland_ui_confirmation_choice=Y
      userland_ui_confirmation_status=0
      ;;
    *)
      userland_ui_confirmation_choice=N
      userland_ui_confirmation_status=3
      ;;
  esac
  if [ "$userland_ui_confirm_output" = /dev/tty ] && [ "$userland_ui_active_mode" = rich ]; then
    printf '%s[1A\r%s[2K' "$userland_ui_escape" "$userland_ui_escape" >"$userland_ui_confirm_output"
    printf '%s%s?%s  %s %s[y/N]%s %s›%s ' "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_reset" "$userland_ui_text" "$userland_ui_dim" "$userland_ui_reset" "$userland_ui_cyan" "$userland_ui_reset" >"$userland_ui_confirm_output"
  fi
  printf '%s\n' "$userland_ui_confirmation_choice" >"$userland_ui_confirm_output"
  return "$userland_ui_confirmation_status"
}

userland_ui_acknowledge() {
  userland_ui_redact "$*"
  if [ "${USERLAND_TESTING:-0}" = 1 ] && [ "${USERLAND_UI_TEST_ACKNOWLEDGEMENT:-0}" = 1 ]; then
    userland_ui_acknowledge_output=/dev/stdout
  elif [ ! -t 0 ] || [ ! -r /dev/tty ]; then
    userland_ui_status error "$userland_ui_text requires an interactive terminal"
    return 1
  else
    userland_ui_acknowledge_output=/dev/tty
  fi

  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s%s?%s  %s %s›%s ' \
      "$userland_ui_margin" \
      "$userland_ui_cyan" \
      "$userland_ui_reset" \
      "$userland_ui_text" \
      "$userland_ui_cyan" \
      "$userland_ui_reset" >"$userland_ui_acknowledge_output"
  else
    printf '%s ' "$userland_ui_text" >"$userland_ui_acknowledge_output"
  fi
  if [ "$userland_ui_acknowledge_output" = /dev/tty ]; then
    IFS= read -r userland_ui_acknowledgement </dev/tty || return 1
    : "$userland_ui_acknowledgement"
  fi
  printf '\n' >"$userland_ui_acknowledge_output"
  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s%s\n' "$userland_ui_margin" "$userland_ui_rail" >"$userland_ui_acknowledge_output"
  fi
}

userland_ui() {
  userland_ui_prepare_stream
  userland_ui_event=${1:-}
  [ "$#" -gt 0 ] && shift
  case "$userland_ui_event" in
    usage)
      [ "$#" -eq 0 ] || return 64
      userland_ui_usage
      ;;
    command)
      [ "$#" -eq 2 ] || return 64
      userland_ui_command=$1
      userland_ui_description=$2
      case "$userland_ui_command" in plan | sync | doctor) ;; *) return 64 ;; esac
      userland_ui_started_at=$(date +%s)
      trap 'userland_ui_exit' EXIT
      trap 'userland_ui_signal 129' HUP
      trap 'userland_ui_signal 130' INT
      trap 'userland_ui_signal 143' TERM
      if [ "$userland_ui_active_mode" = rich ]; then
        userland_ui_wordmark
        if [ "$userland_ui_unicode" != 0 ]; then
          userland_ui_title_rule='────────────────────────────────────────'
        else
          userland_ui_title_rule='----------------------------------------'
        fi
        printf '%s%s%s\n' "$userland_ui_margin" "$userland_ui_open" "$userland_ui_title_rule"
        printf '%s%s  %s%s%s\n' "$userland_ui_margin" "$userland_ui_rail" "$userland_ui_bold" "$userland_ui_command" "$userland_ui_reset"
        printf '%s%s  %s%s%s\n' "$userland_ui_margin" "$userland_ui_rail" "$userland_ui_dim" "$userland_ui_description" "$userland_ui_reset"
      else
        printf 'userland %s: %s\n' "$userland_ui_command" "$userland_ui_description"
      fi
      ;;
    section)
      [ "$#" -eq 1 ] || return 64
      userland_ui_redact "$1"
      if [ "$userland_ui_active_mode" = rich ]; then
        printf '%s%s\n%s%s%s%s  %s\n%s%s\n' "$userland_ui_margin" "$userland_ui_rail" "$userland_ui_margin" "$userland_ui_cyan" "$userland_ui_section" "$userland_ui_reset" "$userland_ui_text" "$userland_ui_margin" "$userland_ui_rail"
      else
        printf '== %s\n' "$userland_ui_text"
      fi
      ;;
    spacer)
      [ "$#" -eq 0 ] || return 64
      if [ "$userland_ui_active_mode" = rich ]; then
        printf '%s%s\n' "$userland_ui_margin" "$userland_ui_rail"
      else
        printf '\n'
      fi
      ;;
    status)
      [ "$#" -ge 2 ] || return 64
      userland_ui_status "$@"
      ;;
    task)
      [ "$#" -ge 3 ] || return 64
      userland_ui_task "$@"
      ;;
    summary)
      [ "$#" -ge 2 ] || return 64
      userland_ui_summary_state=$1
      shift
      case "$userland_ui_summary_state" in ok | attention | cancelled | error) ;; *) return 64 ;; esac
      userland_ui_redact "$*"
      userland_ui_elapsed
      if [ "$userland_ui_active_mode" = rich ]; then
        printf '%s%s\n' "$userland_ui_margin" "$userland_ui_rail"
        case "$userland_ui_summary_state" in
          ok) userland_ui_summary_tint=$userland_ui_green ;;
          attention) userland_ui_summary_tint=$userland_ui_yellow ;;
          error) userland_ui_summary_tint=$userland_ui_red ;;
          cancelled) userland_ui_summary_tint= ;;
        esac
        printf '%s%s%s%s  %s\n' "$userland_ui_margin" "$userland_ui_summary_tint" "$userland_ui_close" "$userland_ui_reset" "$userland_ui_text"
        printf '%s   %s%s%s\n' "$userland_ui_margin" "$userland_ui_dim" "$userland_ui_elapsed_text" "$userland_ui_reset"
      else
        printf '\n'
        userland_ui_status "$userland_ui_summary_state" "$userland_ui_text ($userland_ui_elapsed_text)"
      fi
      ;;
    confirm)
      [ "$#" -ge 1 ] || return 64
      userland_ui_confirm "$@"
      ;;
    acknowledge)
      [ "$#" -ge 1 ] || return 64
      userland_ui_acknowledge "$@"
      ;;
    *) return 64 ;;
  esac
}
