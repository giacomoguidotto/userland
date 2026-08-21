#!/bin/sh

# shellcheck source=common.sh
. "$USERLAND_ROOT/lib/common.sh"
# shellcheck source=repository.sh
. "$USERLAND_ROOT/lib/repository.sh"
# shellcheck source=dotfiles.sh
. "$USERLAND_ROOT/lib/dotfiles.sh"
# shellcheck source=plan-ledger.sh
. "$USERLAND_ROOT/lib/plan-ledger.sh"

userland_plan_mise_resources() {
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap plan --json >"$USERLAND_PLAN_RESULT"
}

userland_plan_mise_dotfiles() {
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap dotfiles status --json >"$USERLAND_PLAN_RESULT"
}

userland_plan_macos_defaults() {
  "$USERLAND_MISE" -C "$USERLAND_ROOT" bootstrap macos defaults status --json >"$USERLAND_PLAN_RESULT"
}

userland_plan_plist_count() {
  userland_plan_count_path=$1
  userland_plan_count_file=$2
  userland_plan_count=$(/usr/bin/plutil -extract "$userland_plan_count_path" raw -o - "$userland_plan_count_file" 2>/dev/null) || return 1
  case "$userland_plan_count" in *[!0-9]* | '') return 1 ;; esac
  printf '%s\n' "$userland_plan_count"
}

userland_plan_find_command() {
  (
    PATH=$USERLAND_ORIGINAL_PATH
    export PATH
    command -v "$1"
  )
}

userland_plan_formula_command() {
  userland_plan_formula=${1##*/}
  userland_plan_formula=${userland_plan_formula%%@*}
  if userland_plan_find_command "$userland_plan_formula" >/dev/null 2>&1; then
    printf '%s\n' "$userland_plan_formula"
    return 0
  fi
  case "$userland_plan_formula" in
    git-delta) userland_plan_formula_alias='delta' ;;
    kubernetes-cli) userland_plan_formula_alias='kubectl' ;;
    neovim) userland_plan_formula_alias='nvim' ;;
    nushell) userland_plan_formula_alias='nu' ;;
    ripgrep) userland_plan_formula_alias='rg' ;;
    *) return 1 ;;
  esac
  userland_plan_find_command "$userland_plan_formula_alias" >/dev/null 2>&1 || return 1
  printf '%s\n' "$userland_plan_formula_alias"
}

userland_plan_resolve_command() {
  userland_plan_resolved=$1
  userland_plan_resolve_depth=0
  while [ -L "$userland_plan_resolved" ]; do
    userland_plan_resolve_depth=$((userland_plan_resolve_depth + 1))
    [ "$userland_plan_resolve_depth" -le 40 ] || return 1
    userland_plan_resolve_target=$(readlink "$userland_plan_resolved") || return 1
    case "$userland_plan_resolve_target" in
      /*) userland_plan_resolved=$userland_plan_resolve_target ;;
      *)
        userland_plan_resolve_directory=${userland_plan_resolved%/*}
        [ "$userland_plan_resolve_directory" != "$userland_plan_resolved" ] || userland_plan_resolve_directory=.
        userland_plan_resolved=$userland_plan_resolve_directory/$userland_plan_resolve_target
        ;;
    esac
  done
  printf '%s\n' "$userland_plan_resolved"
}

userland_plan_command_provider() {
  userland_plan_command_path=$(userland_plan_find_command "$1" 2>/dev/null) || return 1
  userland_plan_command_resolved=$(userland_plan_resolve_command "$userland_plan_command_path" 2>/dev/null) || userland_plan_command_resolved=$userland_plan_command_path
  userland_plan_command_locations=$userland_plan_command_path:$userland_plan_command_resolved
  case "$userland_plan_command_locations" in
    *"/nix/store/"* | *"/etc/profiles/per-user/"* | *"/.nix-profile/"*) printf 'Nix\n' ;;
    *"/opt/homebrew/"* | *"/usr/local/Homebrew/"* | *"/usr/local/Cellar/"*) printf 'Homebrew\n' ;;
    *"/.local/share/mise/"*) printf 'mise\n' ;;
    *"/opt/local/"*) printf 'MacPorts\n' ;;
    *"/.cargo/"* | *"/.rustup/"*) printf 'Cargo\n' ;;
    *"/.bun/"*) printf 'Bun\n' ;;
    *"/node_modules/.bin/"* | *"/.npm/"*) printf 'npm\n' ;;
    *"/.local/share/uv/"*) printf 'uv\n' ;;
    *"/pipx/"*) printf 'pipx\n' ;;
    *"/.asdf/"*) printf 'asdf\n' ;;
    /usr/bin/*:* | /bin/*:* | /usr/sbin/*:* | /sbin/*:* | /Library/Apple/usr/bin/*:*) printf 'macOS\n' ;;
    *) return 1 ;;
  esac
}

userland_plan_package_detail() {
  userland_plan_package=$1
  userland_plan_package_change=$2
  if [ "$userland_plan_package_change" = upgrade ]; then
    printf 'upgrade with Homebrew\n'
    return 0
  fi
  if userland_plan_package_command=$(userland_plan_formula_command "$userland_plan_package") &&
    userland_plan_package_provider=$(userland_plan_command_provider "$userland_plan_package_command"); then
    printf 'migrate from %s to Homebrew\n' "$userland_plan_package_provider"
  else
    printf 'migrate to Homebrew\n'
  fi
}

userland_plan_import_mise() {
  userland_plan_json=$1
  userland_plan_resource_count=$(userland_plan_plist_count resources "$userland_plan_json") || return 1
  userland_plan_resource_index=0
  while [ "$userland_plan_resource_index" -lt "$userland_plan_resource_count" ]; do
    userland_plan_resource="resources.$userland_plan_resource_index"
    userland_plan_resource_action=$(/usr/bin/plutil -extract "$userland_plan_resource.action" raw -o - "$userland_plan_json" 2>/dev/null) || return 1
    userland_plan_resource_kind=$(/usr/bin/plutil -extract "$userland_plan_resource.id.kind" raw -o - "$userland_plan_json" 2>/dev/null) || return 1
    userland_plan_resource_name=$(/usr/bin/plutil -extract "$userland_plan_resource.id.name" raw -o - "$userland_plan_json" 2>/dev/null) || return 1
    userland_plan_resource_current=$(/usr/bin/plutil -extract "$userland_plan_resource.current" raw -o - "$userland_plan_json" 2>/dev/null) || return 1
    userland_plan_resource_desired=$(/usr/bin/plutil -extract "$userland_plan_resource.desired" raw -o - "$userland_plan_json" 2>/dev/null) || return 1

    case "$userland_plan_resource_action" in
      unchanged) ;;
      remove)
        userland_plan_add cleanup remove automatic declared \
          "$userland_plan_resource_name" \
          "$userland_plan_resource_current to absent" \
          "mise:$userland_plan_resource_kind:$userland_plan_resource_name" || return 1
        ;;
      create | update)
        case "$userland_plan_resource_kind" in
          package)
            userland_plan_package_name=${userland_plan_resource_name#brew:}
            if [ "$userland_plan_resource_action" = create ]; then
              userland_plan_package_action=install
            else
              userland_plan_package_action=upgrade
            fi
            userland_plan_package_description=$(userland_plan_package_detail "$userland_plan_package_name" "$userland_plan_package_action")
            userland_plan_add apps "$userland_plan_package_action" automatic declared \
              "$userland_plan_package_name" \
              "$userland_plan_package_description" \
              "mise:$userland_plan_resource_kind:$userland_plan_resource_name" || return 1
            ;;
          file | directory)
            userland_plan_add fs "$userland_plan_resource_action" automatic declared \
              "$userland_plan_resource_name" \
              "$userland_plan_resource_current to $userland_plan_resource_desired" \
              "mise:$userland_plan_resource_kind:$userland_plan_resource_name" || return 1
            ;;
          *)
            userland_plan_add os set automatic declared \
              "$userland_plan_resource_name" \
              "$userland_plan_resource_current to $userland_plan_resource_desired" \
              "mise:$userland_plan_resource_kind:$userland_plan_resource_name" || return 1
            ;;
        esac
        ;;
      *)
        userland_plan_add os review blocked declared \
          "$userland_plan_resource_name" \
          "mise reported unknown action: $userland_plan_resource_action" \
          "mise:$userland_plan_resource_kind:$userland_plan_resource_name" || return 1
        ;;
    esac
    userland_plan_resource_index=$((userland_plan_resource_index + 1))
  done
}

userland_plan_import_dotfiles() {
  userland_dotfiles_json=$1
  userland_dotfile_count=$(userland_plan_plist_count files "$userland_dotfiles_json") || return 1
  userland_dotfile_index=0
  while [ "$userland_dotfile_index" -lt "$userland_dotfile_count" ]; do
    userland_dotfile="files.$userland_dotfile_index"
    userland_dotfile_state=$(/usr/bin/plutil -extract "$userland_dotfile.state" raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
    userland_dotfile_target=$(/usr/bin/plutil -extract "$userland_dotfile.target" raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
    case "$userland_dotfile_state" in
      applied) ;;
      source_missing)
        userland_dotfile_source=$(/usr/bin/plutil -extract "$userland_dotfile.source" raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
        userland_plan_add fs review blocked declared \
          "$userland_dotfile_target" \
          "managed source is missing: $userland_dotfile_source" \
          "dotfile:$userland_dotfile_target" || return 1
        ;;
      missing | differs)
        userland_dotfile_source=$(/usr/bin/plutil -extract "$userland_dotfile.source" raw -o - "$userland_dotfiles_json" 2>/dev/null) || return 1
        userland_plan_add fs link automatic declared \
          "$userland_dotfile_target" \
          "from $userland_dotfile_source" \
          "dotfile:$userland_dotfile_target" || return 1
        ;;
      *)
        userland_plan_add fs review blocked declared \
          "$userland_dotfile_target" \
          "unknown managed-path state: $userland_dotfile_state" \
          "dotfile:$userland_dotfile_target" || return 1
        ;;
    esac
    userland_dotfile_index=$((userland_dotfile_index + 1))
  done
}

userland_plan_macos_domain_name() {
  case "$1" in
    NSGlobalDomain) printf 'Global' ;;
    com.apple.finder) printf 'Finder' ;;
    com.apple.dock) printf 'Dock' ;;
    com.apple.spaces) printf 'Spaces' ;;
    com.apple.AppleMultitouchTrackpad) printf 'Trackpad' ;;
    com.apple.HIToolbox) printf 'Keyboard' ;;
    *) printf '%s' "$1" ;;
  esac
}

userland_plan_import_macos_defaults() {
  userland_defaults_json=$1
  userland_defaults_available=$(/usr/bin/plutil -extract macos_defaults.available raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
  [ "$userland_defaults_available" = true ] || return 0
  userland_default_count=$(userland_plan_plist_count macos_defaults.entries "$userland_defaults_json") || return 1
  userland_default_index=0
  while [ "$userland_default_index" -lt "$userland_default_count" ]; do
    userland_default="macos_defaults.entries.$userland_default_index"
    userland_default_state=$(/usr/bin/plutil -extract "$userland_default.state" raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
    case "$userland_default_state" in
      set) ;;
      differs | missing | unset)
        userland_default_domain=$(/usr/bin/plutil -extract "$userland_default.domain" raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
        userland_default_key=$(/usr/bin/plutil -extract "$userland_default.key" raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
        userland_default_current=$(/usr/bin/plutil -extract "$userland_default.current" raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
        userland_default_value=$(/usr/bin/plutil -extract "$userland_default.value" raw -o - "$userland_defaults_json" 2>/dev/null) || return 1
        userland_default_domain_name=$(userland_plan_macos_domain_name "$userland_default_domain")
        userland_plan_add os set automatic declared \
          "$userland_default_domain_name · $userland_default_key" \
          "$userland_default_current to $userland_default_value" \
          "macos-default:$userland_default_domain:$userland_default_key" || return 1
        ;;
      *)
        userland_plan_add os review blocked declared \
          "macOS defaults" \
          "unknown defaults state: $userland_default_state" \
          "macos-defaults-status" || return 1
        ;;
    esac
    userland_default_index=$((userland_default_index + 1))
  done
}

userland_plan() {
  userland_plan_mode=${1:-standalone}
  userland_require_schema
  userland_require_mise
  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui command plan "Preview declared state without applying it."
  fi
  userland_mkdirs
  userland_plan_begin
  userland_ui task inspect "Refreshing repository index" userland_refresh_repository_snapshot

  USERLAND_PLAN_RESULT=$(mktemp "$USERLAND_CACHE_DIR/plan.XXXXXX")
  chmod 600 "$USERLAND_PLAN_RESULT"
  export USERLAND_PLAN_RESULT

  userland_ui task inspect "Inspecting applications and resources" userland_plan_mise_resources
  userland_plan_import_mise "$USERLAND_PLAN_RESULT" ||
    userland_die "mise returned an unreadable plan; no approval was requested"

  userland_ui task inspect "Inspecting managed files" userland_plan_mise_dotfiles
  userland_plan_import_dotfiles "$USERLAND_PLAN_RESULT" ||
    userland_die "mise returned unreadable managed-path status; no approval was requested"

  if userland_is_macos; then
    userland_ui task inspect "Inspecting macOS settings" userland_plan_macos_defaults
    userland_plan_import_macos_defaults "$USERLAND_PLAN_RESULT" ||
      userland_die "mise returned unreadable macOS-defaults status; no approval was requested"
  fi
  rm -f "$USERLAND_PLAN_RESULT"
  unset USERLAND_PLAN_RESULT

  userland_plan_legacy_dotfiles
  userland_ui task collect "Inspecting personal state" userland_run_adapters plan
  # The adapter dispatcher runs in the task child. Its typed records are written
  # to the shared ledger while native logs remain in the private run log.
  userland_plan_render

  if [ "$userland_plan_mode" = standalone ]; then
    userland_ui summary ok "Plan complete. No changes were applied."
  fi
}
