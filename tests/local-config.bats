#!/usr/bin/env bats

setup() {
  ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export HOME="$BATS_TEST_TMPDIR/home"
  export XDG_CONFIG_HOME="$HOME/config with spaces"
  export XDG_CACHE_HOME="$HOME/cache"
  mkdir -p "$HOME" "$XDG_CONFIG_HOME/zsh/conf.d" "$XDG_CACHE_HOME/userland/zsh"
}

@test "local Zsh snippets load after completion setup without modifying managed files" {
  command -v zsh >/dev/null || skip "zsh is required"
  cp "$ROOT/cfg/home/zshrc" "$BATS_TEST_TMPDIR/baseline"
  ln -s "$ROOT/cfg/home/zshrc" "$HOME/.zshrc"
  printf 'autoload -Uz compinit; compinit -d "$HOME/.zcompdump"\n' >"$XDG_CACHE_HOME/userland/zsh/init.zsh"
  printf '_trellis() { :; }; compdef _trellis trellis\n' >"$XDG_CONFIG_HOME/zsh/conf.d/trellis.zsh"
  run zsh -f -i -c 'source "$HOME/.zshrc"; print -r -- "${_comps[trellis]}"'
  [ "$status" -eq 0 ]
  [[ "$output" == *"_trellis"* ]]
  cmp "$BATS_TEST_TMPDIR/baseline" "$ROOT/cfg/home/zshrc"
  [ -L "$HOME/.zshrc" ]
}

@test "Zsh starts with no local snippets and skips them in noninteractive shells" {
  command -v zsh >/dev/null || skip "zsh is required"
  run zsh -f -i -c 'source "$1"' -- "$ROOT/cfg/home/zshrc"
  [ "$status" -eq 0 ]
  printf 'exit 72\n' >"$XDG_CONFIG_HOME/zsh/conf.d/trellis.zsh"
  run zsh -f -c 'source "$1"' -- "$ROOT/cfg/home/zshrc"
  [ "$status" -eq 0 ]
}

@test "SSH loads local hosts without leaking their settings into Userland hosts" {
  command -v ssh >/dev/null || skip "ssh is required"
  mkdir -p "$HOME/.ssh/config.d"
  # OpenSSH expands ~ using passwd rather than the fixture HOME.
  sed "s|~/.ssh/config.d|$HOME/.ssh/config.d|" "$ROOT/cfg/home/ssh/config" >"$HOME/.ssh/config"
  printf 'Host trellis-remote-dev\n  HostName vm.example.invalid\n  User trellis\n  Port 2222\n' >"$HOME/.ssh/config.d/trellis.conf"
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
