#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_raycast_export=${USERLAND_RAYCAST_EXPORT:-$USERLAND_ROOT/config/raycast.rayconfig}
userland_raycast_receipt=$USERLAND_STATE_DIR/receipts/raycast-import.sha256

userland_raycast_export_is_eligible() {
  [ -f "$userland_raycast_export" ] || return 1
  case "$userland_raycast_export" in *.rayconfig) ;; *) return 1 ;; esac
  [ "$(wc -c <"$userland_raycast_export")" -gt 1024 ] || return 1
  # Raycast has used legacy opaque exports, gzip-compressed JSON envelopes,
  # and the RAYCFG3 container introduced by Raycast 2. Validate every format
  # that exposes its encrypted-envelope metadata.
  userland_raycast_header=$(od -An -tx1 -N8 "$userland_raycast_export" | tr -d ' \n')
  case "$userland_raycast_header" in
    1f8b*)
      gzip -t "$userland_raycast_export" 2>/dev/null || return 1
      for userland_raycast_marker in '"data":"' '"encryption":{' '"salt":"' '"iv":"' '"authTag":"'; do
        gzip -dc "$userland_raycast_export" 2>/dev/null | LC_ALL=C grep -aFq "$userland_raycast_marker" || return 1
      done
      ;;
    524159434647330a)
      userland_raycast_metadata_length=$(od -An -tu4 -j8 -N4 "$userland_raycast_export" | tr -d ' \n')
      case "$userland_raycast_metadata_length" in '' | *[!0-9]*) return 1 ;; esac
      [ "$userland_raycast_metadata_length" -gt 0 ] || return 1
      [ "$((12 + userland_raycast_metadata_length))" -lt "$(wc -c <"$userland_raycast_export")" ] || return 1
      dd if="$userland_raycast_export" bs=1 skip=12 count="$userland_raycast_metadata_length" 2>/dev/null |
        gzip -t 2>/dev/null || return 1
      for userland_raycast_marker in '"schemaVersion":3' '"encryption":{' '"salt":"' '"iv":"'; do
        dd if="$userland_raycast_export" bs=1 skip=12 count="$userland_raycast_metadata_length" 2>/dev/null |
          gzip -dc 2>/dev/null | LC_ALL=C grep -aFq "$userland_raycast_marker" || return 1
      done
      ;;
  esac
}

userland_raycast_application() {
  [ -d "/Applications/Raycast.app" ] ||
    [ -d "$USERLAND_HOME/Applications/Raycast.app" ] || return 1
  printf '%s\n' Raycast
}

userland_raycast_is_current() {
  userland_raycast_export_is_eligible || return 1
  [ -f "$userland_raycast_receipt" ] || return 1
  [ "$(userland_sha256 "$userland_raycast_export")" = "$(sed -n '1p' "$userland_raycast_receipt")" ]
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    if userland_raycast_is_current; then
      userland_log current "Raycast configuration import has a matching receipt"
    elif userland_raycast_export_is_eligible; then
      userland_log manual "Raycast will open its encrypted configuration import"
    else
      userland_log manual "Raycast has no encrypted configuration export"
    fi
    ;;
  apply)
    userland_is_macos || exit 0
    userland_raycast_is_current && exit 0
    userland_raycast_export_is_eligible || {
      userland_log manual "export an encrypted Raycast configuration before importing it"
      exit 2
    }
    userland_raycast_app=$(userland_raycast_application) || {
      userland_log manual "install Raycast, then rerun sync to import its configuration"
      exit 2
    }
    [ -t 0 ] || {
      userland_log manual "Raycast import requires an interactive terminal"
      exit 2
    }

    userland_log manual "opening Raycast configuration import; enter its export passphrase in Raycast"
    open -a "$userland_raycast_app" "$userland_raycast_export"
    userland_ui acknowledge "Press Enter after Raycast reports a successful import." || {
      userland_log manual "Raycast import was not confirmed; no receipt was recorded"
      exit 2
    }
    userland_mkdirs
    userland_sha256 "$userland_raycast_export" >"$userland_raycast_receipt"
    userland_log changed "recorded the confirmed Raycast import"
    ;;
  doctor)
    userland_is_macos || exit 0
    if ! userland_raycast_export_is_eligible; then
      userland_log attention "Raycast configuration is missing or is not encrypted"
      exit 2
    elif userland_raycast_is_current; then
      userland_log healthy "Raycast import acknowledgement matches the encrypted export"
    else
      userland_log attention "Raycast configuration needs an attended import"
      exit 2
    fi
    ;;
  *)
    userland_die "raycast adapter expects plan, apply, or doctor" 64
    ;;
esac
