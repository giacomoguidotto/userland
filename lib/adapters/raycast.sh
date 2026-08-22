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
  # Raycast has used both opaque binary exports and gzip-compressed JSON
  # envelopes. The JSON envelope is eligible only when it declares ciphertext
  # and the authenticated-encryption parameters, never clear settings data.
  userland_raycast_header=$(od -An -tx1 -N2 "$userland_raycast_export" | tr -d ' \n')
  [ "$userland_raycast_header" = 1f8b ] || return 0
  gzip -t "$userland_raycast_export" 2>/dev/null || return 1
  for userland_raycast_marker in '"data":"' '"encryption":{' '"salt":"' '"iv":"' '"authTag":"'; do
    gzip -dc "$userland_raycast_export" 2>/dev/null | LC_ALL=C grep -aFq "$userland_raycast_marker" || return 1
  done
}

userland_raycast_application() {
  for userland_raycast_candidate in "Raycast Beta" Raycast; do
    [ -d "/Applications/$userland_raycast_candidate.app" ] ||
      [ -d "$USERLAND_HOME/Applications/$userland_raycast_candidate.app" ] || continue
    printf '%s\n' "$userland_raycast_candidate"
    return 0
  done
  return 1
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
      userland_log manual "install Raycast Beta, then rerun sync to import its configuration"
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
