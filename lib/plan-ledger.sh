#!/bin/sh
# shellcheck disable=SC2154 # UI state is initialized by common.sh before this module is sourced.

if [ -n "${USERLAND_PLAN_LEDGER_LOADED:-}" ]; then
  return 0
fi
USERLAND_PLAN_LEDGER_LOADED=1
userland_plan_tab=$(printf '\t')
userland_plan_newline='
'

userland_plan_begin() {
  userland_ui_ensure_run_log
  mkdir -p "$USERLAND_CACHE_DIR"
  USERLAND_PLAN_FILE=$(mktemp "$USERLAND_CACHE_DIR/plan-ledger.XXXXXX")
  chmod 600 "$USERLAND_PLAN_FILE"
  USERLAND_PLAN_ACTIVE=1
  USERLAND_PLAN_COLLECTING=1
  USERLAND_PLAN_BLOCKED=0
  export USERLAND_PLAN_FILE USERLAND_PLAN_ACTIVE USERLAND_PLAN_COLLECTING USERLAND_PLAN_BLOCKED
}

userland_plan_valid_text() {
  [ -n "$1" ] || return 1
  case "$1" in
    *"$userland_plan_tab"* | *"$userland_plan_newline"*) return 1 ;;
  esac
}

userland_plan_add() {
  [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ] || return 64
  [ "$#" -eq 7 ] || return 64
  userland_plan_area=$1
  userland_plan_action=$2
  userland_plan_handling=$3
  userland_plan_ownership=$4
  userland_plan_target=$5
  userland_plan_detail=$6
  userland_plan_proof=$7

  case "$userland_plan_area" in os | fs | apps | cleanup) ;; *) return 64 ;; esac
  case "$userland_plan_action" in set | create | link | clone | update | install | upgrade | configure | remove | release | review) ;; *) return 64 ;; esac
  case "$userland_plan_handling" in automatic | attended | blocked) ;; *) return 64 ;; esac
  case "$userland_plan_ownership" in declared | userland | external | unmanaged) ;; *) return 64 ;; esac
  userland_plan_valid_text "$userland_plan_target" || return 64
  case "$userland_plan_detail$userland_plan_proof" in
    *"$userland_plan_tab"* | *"$userland_plan_newline"*) return 64 ;;
  esac
  if [ "$userland_plan_area" = cleanup ]; then
    case "$userland_plan_ownership" in declared | userland) ;; *) return 64 ;; esac
    [ -n "$userland_plan_proof" ] || return 64
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$userland_plan_area" \
    "$userland_plan_action" \
    "$userland_plan_handling" \
    "$userland_plan_ownership" \
    "$userland_plan_target" \
    "$userland_plan_detail" \
    "$userland_plan_proof" >>"$USERLAND_PLAN_FILE"
  printf '[plan] %s: %s: %s%s%s\n' \
    "$userland_plan_area" \
    "$userland_plan_action" \
    "$userland_plan_target" \
    "${userland_plan_detail:+: }" \
    "$userland_plan_detail" >>"$USERLAND_UI_RUN_LOG"
  [ "$userland_plan_handling" != blocked ] || USERLAND_PLAN_BLOCKED=1
  export USERLAND_PLAN_BLOCKED
}

userland_plan_capture_log() {
  userland_plan_log_state=$1
  shift
  case "$userland_plan_log_state" in ok | changed | info) return 0 ;; esac
  userland_plan_log_handling=automatic
  case "$userland_plan_log_state" in
    manual) userland_plan_log_handling=attended ;;
    attention | warning | error) userland_plan_log_handling=${USERLAND_PLAN_ATTENTION_HANDLING:-blocked} ;;
  esac
  userland_plan_add \
    "${USERLAND_PLAN_AREA:-fs}" \
    "${USERLAND_PLAN_ACTION:-update}" \
    "$userland_plan_log_handling" \
    "${USERLAND_PLAN_OWNERSHIP:-declared}" \
    "$*" \
    "${USERLAND_PLAN_DETAIL:-}" \
    "${USERLAND_PLAN_PROOF:-}"
}

userland_plan_render() {
  [ "${USERLAND_PLAN_ACTIVE:-0}" = 1 ] || return 64
  userland_plan_render_file=$(mktemp "$USERLAND_CACHE_DIR/plan-render.XXXXXX")
  chmod 600 "$userland_plan_render_file"
  awk -F '\t' '
    function title(area) {
      if (area == "os") return "OS changes"
      if (area == "fs") return "Filesystem changes"
      if (area == "apps") return "Application additions"
      return "Cleanup"
    }
    function glyph(action, handling) {
      if (handling != "automatic") return "!"
      if (action == "remove" || action == "release") return "-"
      if (action == "create" || action == "link" || action == "clone" || action == "install") return "+"
      return "~"
    }
    {
      key = $1 SUBSEP $2 SUBSEP $5
      if (seen[key]++) next
      area[NR] = $1
      action[NR] = $2
      handling[NR] = $3
      target[NR] = $5
      detail[NR] = $6
      count[$1]++
      if ($3 == "automatic" && $1 != "cleanup") automatic++
      if ($3 == "attended") attended++
      if ($3 == "blocked") blocked++
      if ($1 == "cleanup") cleanup++
    }
    END {
      order[1] = "os"; order[2] = "fs"; order[3] = "apps"; order[4] = "cleanup"
      for (o = 1; o <= 4; o++) {
        a = order[o]
        print "section\t" a "\t" title(a) "\t" count[a] + 0
        for (priority = 1; priority <= 2; priority++) {
          for (i = 1; i <= NR; i++) {
            if (area[i] != a) continue
            risky = handling[i] != "automatic" || a == "cleanup"
            if ((priority == 1 && !risky) || (priority == 2 && risky)) continue
            rendered_detail = detail[i] == "" ? "-" : detail[i]
            print "item\t" glyph(action[i], handling[i]) "\t" target[i] "\t" rendered_detail "\t" handling[i]
          }
        }
      }
      print "summary\t" automatic + 0 "\t" attended + 0 "\t" blocked + 0 "\t" cleanup + 0
    }
  ' "$USERLAND_PLAN_FILE" >"$userland_plan_render_file"

  USERLAND_PLAN_ACTIVE=0
  USERLAND_PLAN_COLLECTING=0
  export USERLAND_PLAN_ACTIVE USERLAND_PLAN_COLLECTING
  if [ "$userland_ui_active_mode" = rich ]; then
    printf '%s%s%s  Plan\n%s\n' "$userland_ui_cyan" "$userland_ui_section" "$userland_ui_reset" "$userland_ui_rail"
  fi

  userland_plan_section_index=0
  while IFS="$userland_plan_tab" read -r userland_plan_row userland_plan_a userland_plan_b userland_plan_c userland_plan_d; do
    case "$userland_plan_row" in
      section)
        userland_plan_section_index=$((userland_plan_section_index + 1))
        userland_plan_current_area=$userland_plan_a
        userland_plan_current_count=$userland_plan_c
        if [ "$userland_ui_active_mode" = rich ]; then
          if [ "$userland_plan_section_index" -lt 4 ]; then
            userland_plan_branch='├─'
            userland_plan_item_prefix="$userland_ui_rail  "
          else
            userland_plan_branch='└─'
            userland_plan_item_prefix='   '
          fi
          printf '%s%s%s %s%s%s\n' "$userland_ui_cyan" "$userland_plan_branch" "$userland_ui_reset" "$userland_ui_bold" "$userland_plan_b" "$userland_ui_reset"
          if [ "$userland_plan_current_count" -eq 0 ]; then
            if [ "$userland_plan_current_area" = cleanup ]; then
              printf '%s%sNo stale userland-owned items%s\n' "$userland_plan_item_prefix" "$userland_ui_dim" "$userland_ui_reset"
            else
              printf '%s%sNo changes%s\n' "$userland_plan_item_prefix" "$userland_ui_dim" "$userland_ui_reset"
            fi
          fi
        else
          printf '== %s\n' "$userland_plan_b"
          if [ "$userland_plan_current_count" -eq 0 ]; then
            if [ "$userland_plan_current_area" = cleanup ]; then
              printf '[ok] No stale userland-owned items\n'
            else
              printf '[ok] No changes\n'
            fi
          fi
        fi
        ;;
      item)
        userland_plan_redacted_target=$userland_plan_b
        userland_ui_redact "$userland_plan_redacted_target"
        userland_plan_redacted_target=$userland_ui_text
        userland_ui_redact "$userland_plan_c"
        userland_plan_redacted_detail=$userland_ui_text
        [ "$userland_plan_redacted_detail" != - ] || userland_plan_redacted_detail=
        if [ "$userland_ui_active_mode" = rich ]; then
          case "$userland_plan_d" in
            blocked | attended) userland_plan_item_tint=$userland_ui_yellow ;;
            *) userland_plan_item_tint=$userland_ui_cyan ;;
          esac
          printf '%s%s%s%s  %s' "$userland_plan_item_prefix" "$userland_plan_item_tint" "$userland_plan_a" "$userland_ui_reset" "$userland_plan_redacted_target"
          [ -z "$userland_plan_redacted_detail" ] || printf '  %s%s%s' "$userland_ui_dim" "$userland_plan_redacted_detail" "$userland_ui_reset"
          printf '\n'
        else
          case "$userland_plan_d" in
            blocked) userland_plan_plain_state=error ;;
            attended) userland_plan_plain_state=manual ;;
            *) userland_plan_plain_state=change ;;
          esac
          printf '[%s] %s' "$userland_plan_plain_state" "$userland_plan_redacted_target"
          [ -z "$userland_plan_redacted_detail" ] || printf ': %s' "$userland_plan_redacted_detail"
          printf '\n'
        fi
        ;;
      summary)
        userland_plan_automatic=$userland_plan_a
        userland_plan_attended=$userland_plan_b
        userland_plan_blocked=$userland_plan_c
        userland_plan_cleanup=$userland_plan_d
        USERLAND_PLAN_BLOCKED=$userland_plan_blocked
        export USERLAND_PLAN_BLOCKED
        if [ "$userland_ui_active_mode" = rich ]; then
          printf '%s\n%s%s%s  %s automatic · %s attended · %s cleanup' "$userland_ui_rail" "$userland_ui_green" "$userland_ui_done" "$userland_ui_reset" "$userland_plan_automatic" "$userland_plan_attended" "$userland_plan_cleanup"
          [ "$userland_plan_blocked" -eq 0 ] || printf ' · %s blocked' "$userland_plan_blocked"
          printf '%s\n%s\n' "$userland_ui_reset" "$userland_ui_rail"
        else
          printf '[info] %s automatic; %s attended; %s cleanup; %s blocked\n' "$userland_plan_automatic" "$userland_plan_attended" "$userland_plan_cleanup" "$userland_plan_blocked"
        fi
        userland_ui_redact "$USERLAND_UI_RUN_LOG"
        if [ "$userland_ui_active_mode" = rich ]; then
          printf '%s  %sDetails %s%s\n%s\n' "$userland_ui_rail" "$userland_ui_dim" "$userland_ui_text" "$userland_ui_reset" "$userland_ui_rail"
        else
          printf '[info] Details: %s\n' "$userland_ui_text"
        fi
        ;;
    esac
  done <"$userland_plan_render_file"
  rm -f "$userland_plan_render_file"
}

userland_plan_require_applicable() {
  if [ "${USERLAND_PLAN_BLOCKED:-0}" -ne 0 ]; then
    userland_log error "Resolve the blocked plan items before syncing"
    return 2
  fi
  return 0
}
