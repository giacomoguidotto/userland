#!/usr/bin/env bats

setup() {
  ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export ROOT
  export HOME="$BATS_TEST_TMPDIR/home"
  export XDG_CONFIG_HOME="$HOME/config with spaces"
  export XDG_CACHE_HOME="$HOME/cache"
  mkdir -p "$HOME" "$XDG_CONFIG_HOME/userland" "$XDG_CACHE_HOME/userland/zsh"
}

@test "Trellis can append normal Zsh setup beside Userland" {
  command -v zsh >/dev/null || skip "zsh is required"
  cp "$ROOT/cfg/home/zshrc" "$XDG_CONFIG_HOME/userland/zshrc"
  printf 'if [ -r "${XDG_CONFIG_HOME:-$HOME/.config}/userland/zshrc" ]; then source "${XDG_CONFIG_HOME:-$HOME/.config}/userland/zshrc"; fi\n_trellis() { :; }; compdef _trellis trellis\n' >"$HOME/.zshrc"
  run env PATH=/usr/bin:/bin zsh -f -i -c 'source "$ROOT/cfg/home/zshenv"; source "$HOME/.zshrc"; print -r -- "${_comps[trellis]}"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"_trellis"* ]]
  grep -q '_trellis' "$HOME/.zshrc"
}

@test "Zsh skips Userland's interactive fragment in noninteractive shells" {
  command -v zsh >/dev/null || skip "zsh is required"
  cp "$ROOT/cfg/home/zshrc" "$XDG_CONFIG_HOME/userland/zshrc"
  run zsh -f -i -c 'source "$1"' -- "$ROOT/cfg/home/zshenv"
  [ "$status" -eq 0 ]
  printf 'exit 72\n' >>"$XDG_CONFIG_HOME/userland/zshrc"
  run zsh -f -c 'source "$1"; print ok' -- "$ROOT/cfg/home/zshenv"
  [ "$status" -eq 0 ]
  [ "$output" = ok ]
}

@test "SSH loads local hosts without leaking their settings into Userland hosts" {
  command -v ssh >/dev/null || skip "ssh is required"
  mkdir -p "$HOME/.ssh" "$HOME/.config/userland/ssh"
  # OpenSSH expands ~ using passwd rather than the fixture HOME.
  {
    printf 'Include %s\n' "$HOME/.config/userland/ssh/config"
    cat "$ROOT/cfg/home/ssh/config"
    printf '%s\n' 'Host trellis-remote-dev' '  HostName vm.example.invalid' '  User trellis' '  Port 2222'
  } >"$HOME/.ssh/config"
  cp "$ROOT/cfg/home/ssh/config" "$HOME/.config/userland/ssh/config"
  run ssh -G -F "$HOME/.ssh/config" trellis-remote-dev
  [ "$status" -eq 0 ]
  [[ "$output" == *"hostname vm.example.invalid"* ]]
  [[ "$output" == *"user trellis"* ]]
  [[ "$output" == *"port 2222"* ]]
  run ssh -G -F "$HOME/.ssh/config" life-github
  [ "$status" -eq 0 ]
  [[ "$output" == *"hostname github.com"* ]]
  [[ "$output" == *"user git"* ]]
  [[ "$output" != *"port 2222"* ]]
}
