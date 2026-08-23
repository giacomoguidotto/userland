#!/bin/sh

set -eu

# shellcheck source=../common.sh
. "$USERLAND_ROOT/lib/common.sh"

userland_android_home=${ANDROID_HOME:-$USERLAND_HOME/Library/Android/sdk}
userland_android_packages='platform-tools emulator platforms;android-36 build-tools;36.0.0'

userland_android_sdkmanager_command() {
  if [ -n "${USERLAND_SDKMANAGER:-}" ] && [ -x "$USERLAND_SDKMANAGER" ]; then
    printf '%s\n' "$USERLAND_SDKMANAGER"
  else
    command -v sdkmanager
  fi
}

userland_android_java_home() {
  [ -n "${USERLAND_MISE:-}" ] && [ -x "$USERLAND_MISE" ] || return 1
  "$USERLAND_MISE" -C "$USERLAND_ROOT" where java 2>/dev/null
}

userland_android_runtime_check() {
  userland_android_sdkmanager_path=$(userland_android_sdkmanager_command 2>/dev/null) || return 1
  userland_android_java=$(userland_android_java_home) || return 1
  [ -x "$userland_android_java/bin/java" ] || return 1
  JAVA_HOME="$userland_android_java" PATH="$userland_android_java/bin:$PATH" \
    "$userland_android_sdkmanager_path" --version </dev/null >/dev/null 2>&1
}

userland_android_sdkmanager() {
  userland_android_java=$(userland_android_java_home) || return 1
  userland_android_sdkmanager_path=$(userland_android_sdkmanager_command 2>/dev/null) || return 1
  JAVA_HOME="$userland_android_java" PATH="$userland_android_java/bin:$PATH" \
    "$userland_android_sdkmanager_path" "$@"
}

userland_android_package_is_installed() {
  case "$1" in
    platform-tools) [ -x "$userland_android_home/platform-tools/adb" ] ;;
    emulator) [ -x "$userland_android_home/emulator/emulator" ] ;;
    platforms\;android-36) [ -d "$userland_android_home/platforms/android-36" ] ;;
    build-tools\;36.0.0) [ -d "$userland_android_home/build-tools/36.0.0" ] ;;
    *) return 1 ;;
  esac
}

userland_android_check() {
  userland_android_missing=0
  for userland_android_package in $userland_android_packages; do
    if ! userland_android_package_is_installed "$userland_android_package"; then
      userland_android_missing=$((userland_android_missing + 1))
    fi
  done
  [ "$userland_android_missing" -eq 0 ]
}

case "${1:-}" in
  plan)
    userland_is_macos || exit 0
    if ! userland_android_runtime_check; then
      userland_log change 'Android sdkmanager will use pinned Java 21 and JAVA_HOME'
    fi
    if userland_android_check; then
      userland_log current "Android SDK, emulator, platform tools, and API 36 are installed"
    else
      userland_log manual "Android SDK packages and licenses need installation"
    fi
    ;;
  apply)
    userland_is_macos || exit 0
    userland_android_runtime_check || {
      userland_log attention "sdkmanager cannot execute with pinned Java 21 after tool convergence"
      exit 2
    }
    userland_android_check && exit 0
    [ -t 0 ] || {
      userland_log manual "Android licenses require an interactive terminal"
      exit 2
    }
    mkdir -p "$userland_android_home"
    userland_log manual "review and accept the Android SDK licenses"
    ANDROID_HOME="$userland_android_home" userland_android_sdkmanager --licenses </dev/tty
    # The quoted arguments retain sdkmanager's semicolon-separated package IDs.
    ANDROID_HOME="$userland_android_home" userland_android_sdkmanager \
      "platform-tools" "emulator" "platforms;android-36" "build-tools;36.0.0"
    userland_log changed "installed Android command-line development packages"
    ;;
  doctor)
    userland_is_macos || exit 0
    if ! userland_android_runtime_check; then
      userland_log attention "sdkmanager cannot execute with pinned Java 21 and JAVA_HOME"
      exit 2
    elif userland_android_check; then
      userland_log healthy "Android command-line development environment is complete"
    else
      userland_log attention "Android command-line development packages are missing"
      exit 2
    fi
    ;;
  *)
    userland_die "android-sdk adapter expects plan, apply, or doctor" 64
    ;;
esac
