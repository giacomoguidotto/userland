#!/usr/bin/env bats

setup() {
  export TEST_ROOT
  TEST_ROOT=$(CDPATH= cd -- "$BATS_TEST_DIRNAME/.." && pwd)
  export TEST_TMPDIR
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/userland-dotfiles.XXXXXX")
  export USERLAND_ROOT=$TEST_ROOT
  export USERLAND_HOME=$TEST_TMPDIR/home
  export USERLAND_CACHE_DIR=$TEST_TMPDIR/cache
  export USERLAND_DATA_DIR=$TEST_TMPDIR/data
  export USERLAND_STATE_DIR=$TEST_TMPDIR/state
  mkdir -p "$USERLAND_HOME/.config" "$TEST_TMPDIR/workspace/cfg/xdg/gh"
  printf '%s\n' 'hosts stay local' >"$TEST_TMPDIR/workspace/cfg/xdg/gh/hosts.yml"
  printf '%s\n' 'old tracked config' >"$TEST_TMPDIR/workspace/cfg/xdg/gh/config.yml"
  ln -s "$TEST_TMPDIR/workspace/cfg/xdg/gh" "$USERLAND_HOME/.config/gh"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "legacy directory migration preserves only unmanaged children" {
  run sh -c '. "$USERLAND_ROOT/libexec/userland/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -d "$USERLAND_HOME/.config/gh" ]
  [ ! -L "$USERLAND_HOME/.config/gh" ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  grep -q 'hosts stay local' "$USERLAND_HOME/.config/gh/hosts.yml"
}

@test "agent migration uses the root layout and removes retired instructions" {
  mkdir -p \
    "$TEST_TMPDIR/workspace/cfg/home/agents/skills/example" \
    "$TEST_TMPDIR/workspace/cfg/home/claude" \
    "$USERLAND_HOME/.agents" \
    "$USERLAND_HOME/.claude" \
    "$USERLAND_HOME/.codex" \
    "$USERLAND_HOME/.config/opencode"
  printf '%s\n' 'old skill' >"$TEST_TMPDIR/workspace/cfg/home/agents/skills/example/SKILL.md"
  printf '%s\n' '{}' >"$TEST_TMPDIR/workspace/cfg/home/claude/settings.json"
  : >"$TEST_TMPDIR/workspace/cfg/home/agents/AGENT.md"

  ln -s "$TEST_TMPDIR/workspace/cfg/home/agents/skills" "$USERLAND_HOME/.agents/skills"
  ln -s "$TEST_TMPDIR/workspace/cfg/home/agents/skills" "$USERLAND_HOME/.claude/skills"
  ln -s "$TEST_TMPDIR/workspace/cfg/home/claude/settings.json" "$USERLAND_HOME/.claude/settings.json"
  ln -s "$TEST_TMPDIR/workspace/cfg/home/agents/AGENT.md" "$USERLAND_HOME/.codex/AGENTS.md"
  ln -s "$USERLAND_ROOT/cfg/home/agents/opencode/AGENTS.md" "$USERLAND_HOME/.config/opencode/AGENTS.md"

  run sh -c '. "$USERLAND_ROOT/libexec/userland/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -d "$USERLAND_HOME/.agents/skills" ]
  [ ! -L "$USERLAND_HOME/.agents/skills" ]
  [ -d "$USERLAND_HOME/.claude/skills" ]
  [ ! -L "$USERLAND_HOME/.claude/skills" ]
  [ ! -e "$USERLAND_HOME/.claude/settings.json" ]
  [ ! -L "$USERLAND_HOME/.claude/settings.json" ]
  [ ! -e "$USERLAND_HOME/.codex/AGENTS.md" ]
  [ ! -L "$USERLAND_HOME/.codex/AGENTS.md" ]
  [ ! -e "$USERLAND_HOME/.config/opencode/AGENTS.md" ]
  [ ! -L "$USERLAND_HOME/.config/opencode/AGENTS.md" ]
  [[ "$output" == *"removed retired agent instructions"* ]]
}
