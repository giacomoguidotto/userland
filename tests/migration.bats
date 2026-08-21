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
  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -d "$USERLAND_HOME/.config/gh" ]
  [ ! -L "$USERLAND_HOME/.config/gh" ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  grep -q 'hosts stay local' "$USERLAND_HOME/.config/gh/hosts.yml"
}

@test "agent migration uses the config layout and removes retired instructions" {
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

  run sh -c '. "$USERLAND_ROOT/lib/common.sh"; . "$USERLAND_ROOT/lib/dotfiles.sh"; . "$USERLAND_ROOT/lib/plan-ledger.sh"; userland_plan_begin; userland_plan_legacy_dotfiles; grep -c "$USERLAND_HOME/.config/opencode/AGENTS.md" "$USERLAND_PLAN_FILE"'
  [ "$status" -eq 0 ]
  [ "$output" -eq 1 ]

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'
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

@test "legacy submodule metadata is not copied into the managed Neovim directory" {
  release_root=$TEST_TMPDIR/release-root
  old_nvim=$TEST_TMPDIR/workspace/cfg/xdg/nvim
  mkdir -p "$old_nvim" "$release_root/lib" "$release_root/config/xdg/nvim"
  cp "$TEST_ROOT/lib/common.sh" "$TEST_ROOT/lib/ui.sh" "$TEST_ROOT/lib/dotfiles.sh" "$release_root/lib/"
  printf '%s\n' 'gitdir: ../../../.git/modules/cfg/xdg/nvim' >"$old_nvim/.git"
  printf '%s\n' 'keep this local note' >"$old_nvim/local-note"
  ln -s "$old_nvim" "$USERLAND_HOME/.config/nvim"

  run env USERLAND_ROOT="$release_root" sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'

  [ "$status" -eq 0 ]
  [ -d "$USERLAND_HOME/.config/nvim" ]
  [ ! -e "$USERLAND_HOME/.config/nvim/.git" ]
  [ -f "$USERLAND_HOME/.config/nvim/local-note" ]
}

@test "v0.1.3 release links migrate without losing local files" {
  old_release=$USERLAND_DATA_DIR/releases/v0.1.3
  mkdir -p "$old_release/cfg/xdg/gh" "$old_release/agents/skills" "$USERLAND_HOME/.agents"
  printf '%s\n' 'local host data' >"$old_release/cfg/xdg/gh/hosts.yml"
  printf '%s\n' 'old managed data' >"$old_release/cfg/xdg/gh/config.yml"
  printf '%s\n' 'local skill' >"$old_release/agents/skills/local-skill.md"
  ln -snf "$old_release/cfg/xdg/gh" "$USERLAND_HOME/.config/gh"
  ln -s "$old_release/agents/skills" "$USERLAND_HOME/.agents/skills"

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  [ -f "$USERLAND_HOME/.agents/skills/local-skill.md" ]
  [ ! -L "$USERLAND_HOME/.config/gh" ]
  [ ! -L "$USERLAND_HOME/.agents/skills" ]
}

@test "current-layout release links are recognized as legacy ownership" {
  old_release=$USERLAND_DATA_DIR/releases/v0.1.11
  old_checkout=$USERLAND_DATA_DIR/repo
  rm "$USERLAND_HOME/.config/gh"
  mkdir -p "$old_release/config/xdg/gh" "$old_checkout/config/home" "$USERLAND_HOME/.config/gh"
  printf '%s\n' managed >"$old_release/config/xdg/gh/config.yml"
  printf '%s\n' managed >"$old_checkout/config/home/zshrc"
  printf '%s\n' local >"$USERLAND_HOME/.config/gh/hosts.yml"
  ln -s "$old_release/config/xdg/gh/config.yml" "$USERLAND_HOME/.config/gh/config.yml"
  ln -s "$old_checkout/config/home/zshrc" "$USERLAND_HOME/.zshrc"

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'

  [ "$status" -eq 0 ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  [ ! -e "$USERLAND_HOME/.zshrc" ]
}

@test "v0.1.3 symlink-each trees release every old source" {
  old_checkout=$USERLAND_DATA_DIR/repo
  rm "$USERLAND_HOME/.config/gh"
  mkdir -p \
    "$old_checkout/cfg/xdg/gh" \
    "$old_checkout/agents/skills/unslop" \
    "$USERLAND_HOME/.config/gh" \
    "$USERLAND_HOME/.agents/skills/unslop"
  printf '%s\n' managed >"$old_checkout/cfg/xdg/gh/config.yml"
  printf '%s\n' managed >"$old_checkout/agents/skills/unslop/SKILL.md"
  printf '%s\n' local >"$USERLAND_HOME/.config/gh/hosts.yml"
  ln -s "$old_checkout/cfg/xdg/gh/config.yml" "$USERLAND_HOME/.config/gh/config.yml"
  ln -s "$old_checkout/agents/skills/unslop/SKILL.md" "$USERLAND_HOME/.agents/skills/unslop/SKILL.md"

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles'
  [ "$status" -eq 0 ]
  [ -f "$USERLAND_HOME/.config/gh/hosts.yml" ]
  [ ! -e "$USERLAND_HOME/.config/gh/config.yml" ]
  [ ! -e "$USERLAND_HOME/.agents/skills/unslop/SKILL.md" ]
  [ ! -L "$USERLAND_HOME/.config/gh/config.yml" ]
  [ ! -L "$USERLAND_HOME/.agents/skills/unslop/SKILL.md" ]
}

@test "legacy checkout cleanup waits for links and moves all local state to Trash" {
  old_checkout=$USERLAND_DATA_DIR/repo
  export USERLAND_TRASH_DIR=$TEST_TMPDIR/trash
  mkdir -p "$old_checkout/config/home" "$USERLAND_CACHE_DIR"
  printf '%s\n' managed >"$old_checkout/config/home/zshrc"
  printf '*.secret\n' >"$old_checkout/.gitignore"
  git -C "$old_checkout" init -b main >/dev/null
  git -C "$old_checkout" add config/home/zshrc .gitignore
  git -C "$old_checkout" -c user.name=Userland -c user.email=userland@example.invalid commit -m fixture >/dev/null
  git -C "$old_checkout" remote add origin https://github.com/giacomoguidotto/userland.git
  printf '%s\n' ignored-local-state >"$old_checkout/machine.secret"
  git -C "$old_checkout" checkout -b local-work >/dev/null
  printf '%s\n' branch-local-state >"$old_checkout/branch-only"
  git -C "$old_checkout" add branch-only
  git -C "$old_checkout" -c user.name=Userland -c user.email=userland@example.invalid commit -m local >/dev/null
  git -C "$old_checkout" checkout main >/dev/null
  ln -s "$old_checkout/config/home/zshrc" "$USERLAND_HOME/.zshrc"

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_trash_legacy_checkout'
  [ "$status" -eq 2 ]
  [ -d "$old_checkout/.git" ]

  run sh -c '. "$USERLAND_ROOT/lib/dotfiles.sh"; userland_prepare_legacy_dotfiles; userland_trash_legacy_checkout'
  [ "$status" -eq 0 ]
  [ ! -e "$old_checkout" ]
  trashed_checkout=$(find "$USERLAND_TRASH_DIR" -type d -name checkout -print -quit)
  [ -n "$trashed_checkout" ]
  [ -f "$trashed_checkout/machine.secret" ]
  grep -q ignored-local-state "$trashed_checkout/machine.secret"
  git -C "$trashed_checkout" show-ref --verify --quiet refs/heads/local-work
  [ "$(git -C "$trashed_checkout" show local-work:branch-only)" = branch-local-state ]
}
