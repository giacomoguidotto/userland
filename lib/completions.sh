#!/bin/sh

userland_completions() {
  [ "$#" -eq 1 ] || userland_die "completions expects one shell: bash, fish, nushell, or zsh" 64
  case "$1" in
    bash | fish | nushell | zsh) ;;
    *) userland_die "completions supports bash, fish, nushell, or zsh" 64 ;;
  esac
  cat "$USERLAND_ROOT/completions/$1"
}
