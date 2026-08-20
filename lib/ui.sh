#!/bin/sh

if [ -n "${USERLAND_UI_LOADED:-}" ]; then
  return 0
fi
USERLAND_UI_LOADED=1

: "${USERLAND_UI_MODE:=auto}"
case "$USERLAND_UI_MODE" in auto | rich | plain) ;; *) USERLAND_UI_MODE=plain ;; esac
userland_ui_escape=$(printf '\033')
userland_ui_tab=$(printf '\t')

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
    userland_ui_ok_symbol='✓'
    userland_ui_change_symbol='+'
    userland_ui_attention_symbol='!'
    userland_ui_error_symbol='×'
    userland_ui_info_symbol='·'
    userland_ui_cancel_symbol='–'
  else
    userland_ui_ok_symbol='ok'
    userland_ui_change_symbol='+'
    userland_ui_attention_symbol='!'
    userland_ui_error_symbol='x'
    userland_ui_info_symbol='-'
    userland_ui_cancel_symbol='-'
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
  if [ -n "${USERLAND_UI_RUN_LOG:-}" ]; then
    return 0
  fi
  mkdir -p "$USERLAND_CACHE_DIR" "$USERLAND_STATE_DIR"
  USERLAND_UI_RUN_LOG=$USERLAND_STATE_DIR/last-run.log
  : >"$USERLAND_UI_RUN_LOG"
  chmod 600 "$USERLAND_UI_RUN_LOG"
  export USERLAND_UI_RUN_LOG
}

userland_ui_cleanup() {
  for userland_ui_cleanup_file in \
    "${USERLAND_UI_REPORT_FILE:-}" \
    "${userland_ui_task_log:-}" \
    "${userland_ui_task_status:-}" \
    "${USERLAND_PLAN_RESULT:-}"; do
    [ -n "$userland_ui_cleanup_file" ] && [ -f "$userland_ui_cleanup_file" ] &&
      rm -f "$userland_ui_cleanup_file"
  done
  return 0
}

userland_ui_exit() {
  userland_ui_exit_code=$?
  userland_ui_cleanup
  trap - EXIT HUP INT TERM
  exit "$userland_ui_exit_code"
}

userland_ui_signal() {
  userland_ui_signal_code=$1
  userland_ui_cleanup
  trap - EXIT HUP INT TERM
  exit "$userland_ui_signal_code"
}

userland_ui_report_record() {
  userland_ui_record_state=$1
  userland_ui_record_message=$2
  case "$userland_ui_record_state" in
    ok | info) return 0 ;;
  esac
  userland_ui_ensure_run_log
  printf '%s\t%s\t%s\n' \
    "$userland_ui_record_state" \
    "${USERLAND_UI_REPORT_SECTION:-Plan}" \
    "$userland_ui_record_message" >>"$USERLAND_UI_REPORT_FILE"
  printf '[%s] %s: %s\n' \
    "$userland_ui_record_state" \
    "${USERLAND_UI_REPORT_SECTION:-Plan}" \
    "$userland_ui_record_message" >>"$USERLAND_UI_RUN_LOG"
}

userland_ui_report_begin() {
  [ "$userland_ui_active_mode" = rich ] || return 0
  mkdir -p "$USERLAND_CACHE_DIR"
  USERLAND_UI_REPORT_FILE=$(mktemp "$USERLAND_CACHE_DIR/report.XXXXXX")
  chmod 600 "$USERLAND_UI_REPORT_FILE"
  USERLAND_UI_REPORT_ACTIVE=1
  USERLAND_UI_REPORT_SECTION=Plan
  export USERLAND_UI_REPORT_FILE USERLAND_UI_REPORT_ACTIVE USERLAND_UI_REPORT_SECTION
}

userland_ui_report_render() {
  [ "${USERLAND_UI_REPORT_ACTIVE:-0}" = 1 ] || return 0
  userland_ui_report_file=$USERLAND_UI_REPORT_FILE
  USERLAND_UI_REPORT_ACTIVE=0
  export USERLAND_UI_REPORT_ACTIVE

  userland_ui_report_render_file=$(mktemp "$USERLAND_CACHE_DIR/report-render.XXXXXX")
  chmod 600 "$userland_ui_report_render_file"
  awk -F '\t' '
    {
      if (!seen_section[$2]++) sections[++section_count] = $2
      state[NR] = $1
      section[NR] = $2
      message[NR] = $3
      section_total[$2]++
    }
    END {
      limit = 4
      for (s = 1; s <= section_count; s++) {
        name = sections[s]
        shown = 0
        for (priority = 1; priority <= 2; priority++) {
          for (i = 1; i <= NR && shown < limit; i++) {
            if (section[i] != name) continue
            risky = state[i] == "error" || state[i] == "warning" || \
              state[i] == "manual" || state[i] == "attention"
            if ((priority == 1 && risky) || (priority == 2 && !risky)) {
              print state[i] "\t" section[i] "\t" message[i]
              shown++
            }
          }
        }
        if (section_total[name] > shown) {
          print "more\t" name "\t" section_total[name] - shown " more in details"
        }
      }
    }
  ' "$userland_ui_report_file" >"$userland_ui_report_render_file"

  userland_ui_report_counts=$(awk -F '\t' '
    $1 == "change" || $1 == "changed" { changes++ }
    $1 == "manual" || $1 == "attention" { manual++ }
    $1 == "warning" || $1 == "error" { warnings++ }
    END { printf "%d %d %d %d", changes + 0, manual + 0, warnings + 0, NR + 0 }
  ' "$userland_ui_report_file")
  set -- $userland_ui_report_counts
  userland_ui_report_changes=$1
  userland_ui_report_manual=$2
  userland_ui_report_warnings=$3
  userland_ui_report_total=$4

  printf '\n%sPlan%s\n' "$userland_ui_bold" "$userland_ui_reset"
  userland_ui_report_group=
  USERLAND_UI_NESTED=1
  while IFS="$userland_ui_tab" read -r userland_ui_report_state userland_ui_report_section userland_ui_report_message; do
    [ -n "$userland_ui_report_state" ] || continue
    if [ "$userland_ui_report_section" != "$userland_ui_report_group" ]; then
      printf '\n  %s%s%s\n' "$userland_ui_bold" "$userland_ui_report_section" "$userland_ui_reset"
      userland_ui_report_group=$userland_ui_report_section
    fi
    case "$userland_ui_report_state" in
      change | changed)
        userland_ui_status change "$userland_ui_report_message"
        ;;
      manual | attention)
        userland_ui_status manual "$userland_ui_report_message"
        ;;
      warning)
        userland_ui_status warning "$userland_ui_report_message"
        ;;
      error)
        userland_ui_status error "$userland_ui_report_message"
        ;;
      more) userland_ui_status info "$userland_ui_report_message" ;;
    esac
  done <"$userland_ui_report_render_file"
  unset USERLAND_UI_NESTED

  if [ "$userland_ui_report_total" -eq 0 ]; then
    printf '\n'
    userland_ui_status ok "No changes found"
  else
    userland_ui_report_summary=
    if [ "$userland_ui_report_changes" -gt 0 ]; then
      userland_ui_report_summary="$userland_ui_report_changes change groups"
    fi
    if [ "$userland_ui_report_manual" -gt 0 ]; then
      [ -z "$userland_ui_report_summary" ] || userland_ui_report_summary="$userland_ui_report_summary · "
      userland_ui_report_summary="$userland_ui_report_summary$userland_ui_report_manual manual"
    fi
    if [ "$userland_ui_report_warnings" -gt 0 ]; then
      [ -z "$userland_ui_report_summary" ] || userland_ui_report_summary="$userland_ui_report_summary · "
      userland_ui_report_summary="$userland_ui_report_summary$userland_ui_report_warnings warnings"
    fi
    printf '\n  %s%s%s\n' "$userland_ui_dim" "$userland_ui_report_summary" "$userland_ui_reset"
  fi
  userland_ui_status info "No declared state has been applied"
  if [ -n "${USERLAND_UI_RUN_LOG:-}" ]; then
    userland_ui_status info "Details: $USERLAND_UI_RUN_LOG"
  fi

  rm -f "$userland_ui_report_file"
  rm -f "$userland_ui_report_render_file"
  if [ -n "${userland_ui_task_log:-}" ] && [ -f "$userland_ui_task_log" ]; then
    rm -f "$userland_ui_task_log"
  fi
  unset USERLAND_UI_REPORT_FILE USERLAND_UI_REPORT_ACTIVE USERLAND_UI_REPORT_SECTION
}

userland_ui_task_excerpt() {
  tail -n 6 "$1" | LC_ALL=C tr -cd '\11\12\40-\176' | awk '
    {
      gsub(/\r/, "")
      if (length($0) > 72) $0 = substr($0, 1, 69) "..."
      print "    | " $0
    }
  '
}

userland_ui_task() {
  userland_ui_task_kind=$1
  userland_ui_task_label=$2
  shift 2
  case "$userland_ui_task_kind" in inspect | apply | check) ;; *) return 64 ;; esac
  [ "$#" -gt 0 ] || return 64

  userland_ui_ensure_run_log
  if [ -n "${userland_ui_task_log:-}" ] && [ -f "$userland_ui_task_log" ]; then
    rm -f "$userland_ui_task_log"
  fi
  userland_ui_task_log=$(mktemp "$USERLAND_CACHE_DIR/task.XXXXXX")
  userland_ui_task_status=$userland_ui_task_log.status
  : >"$userland_ui_task_status"
  chmod 600 "$userland_ui_task_log" "$userland_ui_task_status"

  if [ "$userland_ui_active_mode" = rich ]; then
    printf '\r%s[2K  %s%s%s %s... ' \
      "$userland_ui_escape" \
      "$userland_ui_dim" \
      "$userland_ui_info_symbol" \
      "$userland_ui_reset" \
      "$userland_ui_task_label"
    (
      set +e
      if [ "$userland_ui_task_kind" = inspect ]; then
        USERLAND_UI_REPORT_ACTIVE=0
        export USERLAND_UI_REPORT_ACTIVE
      fi
      "$@"
      userland_ui_child_code=$?
      printf '%s\n' "$userland_ui_child_code" >"$userland_ui_task_status"
      exit 0
    ) 2>&1 | tee "$userland_ui_task_log" >/dev/null
    printf '\r%s[2K' "$userland_ui_escape"
  else
    userland_ui_status info "$userland_ui_task_label"
    if [ "$userland_ui_task_kind" = inspect ]; then
      USERLAND_UI_REPORT_ACTIVE=0 "$@"
    else
      "$@"
    fi
    userland_ui_task_code=$?
    rm -f "$userland_ui_task_log" "$userland_ui_task_status"
    unset userland_ui_task_log userland_ui_task_status
    return "$userland_ui_task_code"
  fi

  {
    printf '\n## %s\n' "$userland_ui_task_label"
    cat "$userland_ui_task_log"
  } >>"$USERLAND_UI_RUN_LOG"

  userland_ui_task_code=1
  IFS= read -r userland_ui_task_code <"$userland_ui_task_status" || userland_ui_task_code=1
  rm -f "$userland_ui_task_status"

  if [ "$userland_ui_task_code" -eq 0 ]; then
    if [ "$userland_ui_task_kind" != inspect ] || [ "${USERLAND_UI_REPORT_ACTIVE:-0}" != 1 ]; then
      userland_ui_status ok "$userland_ui_task_label"
    fi
    return 0
  fi
  if { [ "$userland_ui_task_kind" = check ] && { [ "$userland_ui_task_code" -eq 1 ] || [ "$userland_ui_task_code" -eq 2 ]; }; } ||
    { [ "$userland_ui_task_kind" = apply ] && [ "$userland_ui_task_code" -eq 2 ]; }; then
    userland_ui_status attention "$userland_ui_task_label"
    userland_ui_task_excerpt "$userland_ui_task_log"
    userland_ui_status info "Log: $USERLAND_UI_RUN_LOG"
    return "$userland_ui_task_code"
  fi

  userland_ui_saved_report=${USERLAND_UI_REPORT_ACTIVE:-0}
  USERLAND_UI_REPORT_ACTIVE=0
  userland_ui_status error "$userland_ui_task_label failed (exit $userland_ui_task_code)"
  userland_ui_task_excerpt "$userland_ui_task_log"
  userland_ui_status info "Log: $USERLAND_UI_RUN_LOG"
  USERLAND_UI_REPORT_ACTIVE=$userland_ui_saved_report
  return "$userland_ui_task_code"
}

userland_ui_status() {
  userland_ui_prepare_stream
  userland_ui_state=$1
  shift
  userland_ui_redact "$*"

  if [ "${USERLAND_UI_REPORT_ACTIVE:-0}" = 1 ] && [ "$userland_ui_active_mode" = rich ]; then
    userland_ui_report_record "$userland_ui_state" "$userland_ui_text"
    return 0
  fi
  if [ "${USERLAND_UI_HIDE_OK:-0}" = 1 ] && [ "$userland_ui_active_mode" = rich ]; then
    case "$userland_ui_state" in ok | info) return 0 ;; esac
  fi

  case "$userland_ui_state" in
    ok | changed)
      userland_ui_symbol=$userland_ui_ok_symbol
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
    if [ "${USERLAND_UI_NESTED:-0}" = 1 ]; then
      userland_ui_status_indent='    '
    else
      userland_ui_status_indent='  '
    fi
    printf '%s%s%s%s %s\n' "$userland_ui_status_indent" "$userland_ui_tint" "$userland_ui_symbol" "$userland_ui_reset" "$userland_ui_text"
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
  if [ "$userland_ui_elapsed_seconds" -ge 3600 ]; then
    userland_ui_elapsed_text="$((userland_ui_elapsed_seconds / 3600))h $(((userland_ui_elapsed_seconds % 3600) / 60))m"
  elif [ "$userland_ui_elapsed_seconds" -ge 60 ]; then
    userland_ui_elapsed_text="$((userland_ui_elapsed_seconds / 60))m $((userland_ui_elapsed_seconds % 60))s"
  elif [ "$userland_ui_elapsed_seconds" -gt 0 ]; then
    userland_ui_elapsed_text="${userland_ui_elapsed_seconds}s"
  else
    userland_ui_elapsed_text='<1s'
  fi
}

userland_ui_usage() {
  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%suserland%s\n' "$userland_ui_bold" "$userland_ui_reset"
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
  if [ ! -t 0 ] || [ ! -r /dev/tty ]; then
    userland_ui_status error "$userland_ui_text requires an interactive terminal"
    return 1
  fi

  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s?%s %s %s[y/N]%s %s›%s ' "$userland_ui_cyan" "$userland_ui_reset" "$userland_ui_text" "$userland_ui_dim" "$userland_ui_reset" "$userland_ui_cyan" "$userland_ui_reset" >/dev/tty
  else
    printf '%s [y/N] ' "$userland_ui_text" >/dev/tty
  fi
  IFS= read -r userland_ui_confirmation </dev/tty
  case "$userland_ui_confirmation" in
    y | Y | yes | YES) return 0 ;;
    *) return 3 ;;
  esac
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
        printf '%suserland%s %s·%s %s%s%s\n' "$userland_ui_bold" "$userland_ui_reset" "$userland_ui_dim" "$userland_ui_reset" "$userland_ui_cyan" "$userland_ui_command" "$userland_ui_reset"
        printf '%s%s%s\n' "$userland_ui_dim" "$userland_ui_description" "$userland_ui_reset"
      else
        printf 'userland %s: %s\n' "$userland_ui_command" "$userland_ui_description"
      fi
      ;;
    section)
      [ "$#" -eq 1 ] || return 64
      userland_ui_redact "$1"
      if [ "${USERLAND_UI_REPORT_ACTIVE:-0}" = 1 ] && [ "$userland_ui_active_mode" = rich ]; then
        USERLAND_UI_REPORT_SECTION=$userland_ui_text
        export USERLAND_UI_REPORT_SECTION
        return 0
      fi
      if [ "$userland_ui_active_mode" = rich ]; then
        printf '\n%s%s%s\n' "$userland_ui_bold" "$userland_ui_text" "$userland_ui_reset"
      else
        printf '== %s\n' "$userland_ui_text"
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
    report)
      [ "$#" -eq 1 ] || return 64
      case "$1" in
        begin) userland_ui_report_begin ;;
        render) userland_ui_report_render ;;
        *) return 64 ;;
      esac
      ;;
    summary)
      [ "$#" -ge 2 ] || return 64
      userland_ui_summary_state=$1
      shift
      case "$userland_ui_summary_state" in ok | attention | cancelled | error) ;; *) return 64 ;; esac
      userland_ui_redact "$*"
      userland_ui_elapsed
      if [ "$userland_ui_active_mode" = rich ]; then
        printf '\n'
        userland_ui_status "$userland_ui_summary_state" "$userland_ui_text"
        printf '    %s%s%s\n' "$userland_ui_dim" "$userland_ui_elapsed_text" "$userland_ui_reset"
      else
        userland_ui_status "$userland_ui_summary_state" "$userland_ui_text ($userland_ui_elapsed_text)"
      fi
      ;;
    confirm)
      [ "$#" -ge 1 ] || return 64
      userland_ui_confirm "$@"
      ;;
    *) return 64 ;;
  esac
}
