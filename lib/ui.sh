#!/bin/sh

if [ -n "${USERLAND_UI_LOADED:-}" ]; then
  return 0
fi
USERLAND_UI_LOADED=1

: "${USERLAND_UI_MODE:=auto}"
case "$USERLAND_UI_MODE" in auto | rich | plain) ;; *) USERLAND_UI_MODE=plain ;; esac
userland_ui_escape=$(printf '\033')

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

userland_ui_status() {
  userland_ui_prepare_stream
  userland_ui_state=$1
  shift
  userland_ui_redact "$*"

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
    printf '  %s%s%s %s\n' "$userland_ui_tint" "$userland_ui_symbol" "$userland_ui_reset" "$userland_ui_text"
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
