#!/bin/sh
set -eu

test_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH='' cd -- "$test_dir/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-bootstrap-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

tag=v1.2.3
commit=0123456789abcdef0123456789abcdef01234567
fixture="$work/fixture/userland-1.2.3"
mkdir -p "$fixture/bin" "$fixture/lib"
cp "$repository_root/lib/ui.sh" "$fixture/lib/ui.sh"

cat >"$fixture/bin/userland" <<'EOF'
#!/bin/sh
entry_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
printf 'root=%s\n' "$entry_root" >"$TEST_OBSERVATION"
printf 'link=%s\n' "$(readlink "$HOME/.local/bin/userland")" >>"$TEST_OBSERVATION"
printf 'original-path=%s\n' "${USERLAND_ORIGINAL_PATH:-}" >>"$TEST_OBSERVATION"
printf 'created=%s\n' "${USERLAND_BOOTSTRAP_CREATED:-0}" >>"$TEST_OBSERVATION"
if [ "${TEST_MARK_APPLY_STARTED:-0}" = 1 ]; then
  [ -d "$USERLAND_BOOTSTRAP_CONTROL" ] || exit 81
  [ "$(cat "$USERLAND_BOOTSTRAP_CONTROL/owner")" = "$USERLAND_BOOTSTRAP_TOKEN" ] || exit 82
  printf '%s\n' "$USERLAND_BOOTSTRAP_TOKEN" >"$USERLAND_BOOTSTRAP_CONTROL/.apply-started.$$"
  mv "$USERLAND_BOOTSTRAP_CONTROL/.apply-started.$$" "$USERLAND_BOOTSTRAP_CONTROL/apply-started"
fi
if [ "${TEST_SIGNAL_PARENT:-0}" = 1 ]; then
  kill -INT "$PPID"
  exit 130
fi
if [ "${TEST_DIRTY_DURING_SYNC:-0}" = 1 ]; then
  : >"$entry_root/.git/test-dirty"
  printf 'user edit\n' >>"$entry_root/mise.toml"
fi
exit "$TEST_SYNC_STATUS"
EOF
cat >"$fixture/bin/mise" <<'EOF'
#!/bin/sh
[ -z "${TEST_TRUST_LOG:-}" ] || printf '%s\n' "$*" >>"$TEST_TRUST_LOG"
[ "${MISE_QUIET:-0}" = 1 ] || printf 'mise trusted %s\n' "$*"
exit 0
EOF
chmod +x "$fixture/bin/userland" "$fixture/bin/mise"
printf 'min_version = "2026.8.9"\n' >"$fixture/mise.toml"
tar -czf "$work/userland-v1.2.3.tar.gz" -C "$work/fixture" userland-1.2.3
archive_sha=$(shasum -a 256 "$work/userland-v1.2.3.tar.gz" | awk '{ print $1 }')

sed \
  -e "s|@USERLAND_TAG@|$tag|g" \
  -e "s|@USERLAND_COMMIT@|$commit|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256@|$archive_sha|g" \
  "$repository_root/release/bootstrap-template.sh" >"$work/bootstrap"

prepare_home() {
  home=$1
  mkdir -p "$home/.local/bin"

  cat >"$home/.local/bin/curl" <<'EOF'
#!/bin/sh
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --output ]; then
    output=$2
    shift 2
  else
    shift
  fi
done
cp "$TEST_ARCHIVE" "$output"
EOF

  cat >"$home/.local/bin/caffeinate" <<'EOF'
#!/bin/sh
shift
exec "$@"
EOF

  cat >"$home/.local/bin/git" <<'EOF'
#!/bin/sh
while [ "${1:-}" = -c ]; do
  shift 2
done
if [ "$1" = clone ]; then
  for destination do :; done
  if [ "${TEST_GIT_CLONE_FAIL:-0}" = 1 ]; then
    mkdir -p "$destination"
    exit 12
  fi
  mkdir -p "$destination/.git" "$destination/bin"
  cp "$TEST_REPO_COMMAND" "$destination/bin/userland"
  chmod +x "$destination/bin/userland"
  printf 'min_version = "2026.8.9"\n' >"$destination/mise.toml"
  printf '%s\n' "$TEST_COMMIT" >"$destination/.git/test-head"
  printf '%s\n' "${TEST_GIT_REMOTE_MAIN:-$TEST_COMMIT}" >"$destination/.git/test-remote-main"
  printf '%s\n' "$TEST_COMMIT" >"$destination/.git/test-fetched-commit"
  exit 0
fi
if [ "$1" = ls-remote ]; then
  [ "${GIT_CONFIG_GLOBAL:-}" = /dev/null ] || exit 8
  [ "${GIT_CONFIG_NOSYSTEM:-}" = 1 ] || exit 8
  printf '%s\trefs/heads/main\n' "${TEST_GIT_REMOTE_MAIN:-$TEST_COMMIT}"
  exit 0
fi
if [ "$1" = -C ]; then
  checkout_path=$2
  case "$3" in
    config)
      if [ "${7:-}" = core.worktree ]; then
        [ "${TEST_GIT_EXTERNAL_WORKTREE:-0}" = 0 ] && exit 1
        printf '../outside\n'
      else
        printf '%s\n' "${TEST_GIT_ORIGIN:-https://github.com/giacomoguidotto/userland.git}"
      fi
      ;;
    status)
      [ "${GIT_OPTIONAL_LOCKS:-}" = 0 ] || exit 9
      [ "${GIT_CONFIG_GLOBAL:-}" = /dev/null ] || exit 9
      [ "${GIT_CONFIG_NOSYSTEM:-}" = 1 ] || exit 9
      [ "${TEST_GIT_DIRTY:-0}" = 0 ] || printf ' M mise.toml\n'
      [ ! -e "$checkout_path/.git/test-dirty" ] || printf ' M mise.toml\n'
      ;;
    symbolic-ref)
      printf '%s\n' "${TEST_GIT_BRANCH:-main}"
      ;;
    rev-parse)
      case "$4" in
        --is-inside-work-tree) printf 'true\n' ;;
        --abbrev-ref) printf '%s\n' "${TEST_GIT_UPSTREAM:-origin/main}" ;;
        refs/remotes/origin/main*) cat "$checkout_path/.git/test-remote-main" ;;
        refs/userland/bootstrap/*) cat "$checkout_path/.git/test-fetched-commit" ;;
        *) cat "$checkout_path/.git/test-head" ;;
      esac
      ;;
    fetch)
      printf '%s\n' "$TEST_COMMIT" >"$checkout_path/.git/test-fetched-commit"
      printf '%s\n' "${TEST_GIT_REMOTE_MAIN:-$TEST_COMMIT}" >"$checkout_path/.git/test-remote-main"
      ;;
    checkout | merge)
      printf '%s\n' "$TEST_COMMIT" >"$checkout_path/.git/test-head"
      ;;
    reset)
      for reset_target do :; done
      printf '%s\n' "$reset_target" >"$checkout_path/.git/test-head"
      ;;
    submodule)
      if [ "$4" = update ] && [ "${TEST_GIT_SUBMODULE_FAIL:-0}" = 1 ]; then
        exit 13
      fi
      ;;
    cat-file)
      [ "${TEST_GIT_MISSING_COMMIT:-0}" = 0 ] && exit 0
      exit 1
      ;;
    merge-base)
      [ "${TEST_GIT_ANCESTRY_FAIL:-0}" = 0 ] && exit 0
      exit 1
      ;;
  esac
fi
exit 0
EOF
  chmod +x "$home/.local/bin/curl" "$home/.local/bin/caffeinate" "$home/.local/bin/git"
}

cp "$fixture/bin/userland" "$work/repo-userland"
chmod +x "$work/repo-userland"

attention_home="$work/attention-home"
prepare_home "$attention_home"
attention_original_path="$attention_home/original-bin:$PATH"
attention_status=0
HOME="$attention_home" \
  PATH="$attention_original_path" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/attention-observation" \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=2 \
  USERLAND_DATA_DIR="$attention_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >"$work/attention-output" 2>&1 || attention_status=$?
[ "$attention_status" -eq 0 ] || fail "completed attention run returned $attention_status"
grep -Fq 'created=1' "$work/attention-observation" ||
  fail "first run did not hand checkout creation to the Preflight UI"
if grep -Fq 'userland: created ~/.userland' "$work/attention-output"; then
  fail "first run printed checkout creation before the Preflight UI"
fi
if grep -Fq 'mise trusted' "$work/attention-output"; then
  fail "first run exposed mise trust logs"
fi
attention_root=$(CDPATH='' cd -- "$attention_home/.userland" && pwd)
grep -Fq "root=$attention_root" "$work/attention-observation" ||
  fail "sync did not run from the canonical userland path"
grep -Fq "link=$attention_home/.userland/bin/userland" "$work/attention-observation" ||
  fail "canonical userland command was not linked before sync"
grep -Fq "original-path=$attention_original_path" "$work/attention-observation" ||
  fail "bootstrap did not preserve the caller PATH"
[ "$(readlink "$attention_home/.local/bin/userland")" = "$attention_home/.userland/bin/userland" ] ||
  fail "attention run did not finish the repository link"
[ -d "$attention_home/.userland/.git" ] || fail "attention run did not promote the canonical checkout"
[ ! -e "$attention_home/.local/share/userland/repo" ] || fail "attention run created the legacy checkout path"
[ "$(readlink "$attention_home/.local/share/userland/current")" = "$attention_home/.local/share/userland/releases/$tag" ] ||
  fail "attention run did not retain the pinned mise release"

rerun_status=0
HOME="$attention_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/rerun-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_TRUST_LOG="$work/rerun-trust" \
  USERLAND_DATA_DIR="$attention_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || rerun_status=$?
[ "$rerun_status" -eq 0 ] || fail "safe rerun returned $rerun_status"
grep -Fq "$attention_home/.userland/mise.toml" "$work/rerun-trust" ||
  fail "safe checkout was not trusted"
[ "$(readlink "$attention_home/.local/bin/userland")" = "$attention_home/.userland/bin/userland" ] ||
  fail "safe rerun did not restore the repository link"
[ "$(readlink "$attention_home/.local/share/userland/current")" = "$attention_home/.local/share/userland/releases/$tag" ] ||
  fail "safe rerun did not retain the pinned mise release"

ln -s "$attention_home/.local/share/userland/releases/$tag" \
  "$attention_home/.local/share/userland/releases/$tag/.current.new.managed"
ln -s /tmp/personal-current-link \
  "$attention_home/.local/share/userland/releases/$tag/.current.new.personal"

upgrade_tag=v1.2.4
upgrade_commit=1123456789abcdef0123456789abcdef01234567
upgrade_fixture="$work/upgrade-fixture/userland-1.2.4"
mkdir -p "$upgrade_fixture"
cp -R "$fixture/." "$upgrade_fixture"
tar -czf "$work/userland-v1.2.4.tar.gz" -C "$work/upgrade-fixture" userland-1.2.4
upgrade_archive_sha=$(shasum -a 256 "$work/userland-v1.2.4.tar.gz" | awk '{ print $1 }')
sed \
  -e "s|@USERLAND_TAG@|$upgrade_tag|g" \
  -e "s|@USERLAND_COMMIT@|$upgrade_commit|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256@|$upgrade_archive_sha|g" \
  "$repository_root/release/bootstrap-template.sh" >"$work/upgrade-bootstrap"

upgrade_status=0
HOME="$attention_home" \
  TEST_ARCHIVE="$work/userland-v1.2.4.tar.gz" \
  TEST_COMMIT="$upgrade_commit" \
  TEST_GIT_REMOTE_MAIN="$upgrade_commit" \
  TEST_OBSERVATION="$work/upgrade-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_TRUST_LOG="$work/upgrade-trust" \
  USERLAND_DATA_DIR="$attention_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/upgrade-bootstrap" >/dev/null 2>&1 || upgrade_status=$?
[ "$upgrade_status" -eq 0 ] || fail "upgrade run returned $upgrade_status"
[ "$(cat "$attention_home/.userland/.git/test-head")" = "$upgrade_commit" ] ||
  fail "upgrade run did not fast-forward the canonical checkout"
[ "$(readlink "$attention_home/.local/share/userland/current")" = "$attention_home/.local/share/userland/releases/$upgrade_tag" ] ||
  fail "upgrade run did not move the current release pointer"
[ ! -e "$attention_home/.local/share/userland/releases/$tag/.current.new.managed" ] ||
  fail "upgrade run retained a userland-owned stale current link"
[ -L "$attention_home/.local/share/userland/releases/$tag/.current.new.personal" ] ||
  fail "upgrade run removed an unmanaged stale-link lookalike"

prepare_existing_checkout() {
  existing_home=$1
  prepare_home "$existing_home"
  existing_repo="$existing_home/.userland"
  mkdir -p "$existing_repo/.git" "$existing_repo/bin"
  cp "$work/repo-userland" "$existing_repo/bin/userland"
  chmod +x "$existing_repo/bin/userland"
  printf 'original checkout config\n' >"$existing_repo/mise.toml"
  printf '%s\n' "$commit" >"$existing_repo/.git/test-head"
  printf '%s\n' "$commit" >"$existing_repo/.git/test-remote-main"
}

interrupted_upgrade_home="$work/interrupted-upgrade-home"
prepare_existing_checkout "$interrupted_upgrade_home"
interrupted_upgrade_status=0
HOME="$interrupted_upgrade_home" \
  TEST_ARCHIVE="$work/userland-v1.2.4.tar.gz" \
  TEST_COMMIT="$upgrade_commit" \
  TEST_GIT_REMOTE_MAIN="$upgrade_commit" \
  TEST_GIT_SUBMODULE_FAIL=1 \
  TEST_OBSERVATION="$work/interrupted-upgrade-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$interrupted_upgrade_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/upgrade-bootstrap" >/dev/null 2>&1 || interrupted_upgrade_status=$?
[ "$interrupted_upgrade_status" -eq 13 ] ||
  fail "interrupted checkout upgrade returned $interrupted_upgrade_status"
[ "$(cat "$interrupted_upgrade_home/.userland/.git/test-head")" = "$upgrade_commit" ] ||
  fail "interrupted checkout upgrade rewound the verified commit"

recovered_upgrade_status=0
HOME="$interrupted_upgrade_home" \
  TEST_ARCHIVE="$work/userland-v1.2.4.tar.gz" \
  TEST_COMMIT="$upgrade_commit" \
  TEST_GIT_REMOTE_MAIN="$upgrade_commit" \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_OBSERVATION="$work/recovered-upgrade-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$interrupted_upgrade_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/upgrade-bootstrap" >/dev/null 2>&1 || recovered_upgrade_status=$?
[ "$recovered_upgrade_status" -eq 0 ] || fail "recovered checkout upgrade returned $recovered_upgrade_status"
[ "$(cat "$interrupted_upgrade_home/.userland/.git/test-head")" = "$upgrade_commit" ] ||
  fail "recovered checkout upgrade did not reach the released commit"

edited_upgrade_home="$work/edited-upgrade-home"
prepare_existing_checkout "$edited_upgrade_home"
edited_upgrade_status=0
HOME="$edited_upgrade_home" \
  TEST_ARCHIVE="$work/userland-v1.2.4.tar.gz" \
  TEST_COMMIT="$upgrade_commit" \
  TEST_DIRTY_DURING_SYNC=1 \
  TEST_GIT_REMOTE_MAIN="$upgrade_commit" \
  TEST_OBSERVATION="$work/edited-upgrade-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=3 \
  USERLAND_DATA_DIR="$edited_upgrade_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/upgrade-bootstrap" >"$work/edited-upgrade-output" 2>&1 || edited_upgrade_status=$?
[ "$edited_upgrade_status" -eq 3 ] ||
  fail "edited checkout cancellation returned $edited_upgrade_status"
[ "$(cat "$edited_upgrade_home/.userland/.git/test-head")" = "$upgrade_commit" ] ||
  fail "edited checkout cancellation rolled back the prepared commit"
grep -Fq 'user edit' "$edited_upgrade_home/.userland/mise.toml" ||
  fail "edited checkout cancellation discarded the user edit"
[ -d "$edited_upgrade_home/.userland" ] ||
  fail "edited checkout cancellation removed the canonical checkout"
[ "$(readlink "$edited_upgrade_home/.local/bin/userland")" = "$edited_upgrade_home/.userland/bin/userland" ] ||
  fail "edited checkout cancellation replaced the canonical command"

if grep -Fq 'reset --hard' "$repository_root/release/bootstrap-template.sh"; then
  fail "bootstrap contains a destructive checkout rollback"
fi

assert_checkout_refused() {
  case_name=$1
  case_home=$2
  case_status=$3
  [ "$case_status" -ne 0 ] || fail "$case_name checkout was accepted"
  [ "$(cat "$case_home/.userland/mise.toml")" = 'original checkout config' ] ||
    fail "$case_name checkout was modified"
  [ "$(readlink "$case_home/.local/bin/userland")" = "$case_home/.local/share/userland/releases/$tag/bin/userland" ] ||
    fail "$case_name checkout replaced the release command"
  if grep -Fq "$case_home/.userland/mise.toml" "$work/$case_name-trust"; then
    fail "$case_name checkout was trusted"
  fi
}

origin_home="$work/origin-home"
prepare_existing_checkout "$origin_home"
origin_status=0
HOME="$origin_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_GIT_ORIGIN=https://example.invalid/arbitrary.git \
  TEST_OBSERVATION="$work/origin-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_TRUST_LOG="$work/origin-trust" \
  USERLAND_DATA_DIR="$origin_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || origin_status=$?
assert_checkout_refused origin "$origin_home" "$origin_status"

dirty_home="$work/dirty-home"
prepare_existing_checkout "$dirty_home"
dirty_status=0
HOME="$dirty_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_GIT_DIRTY=1 \
  TEST_OBSERVATION="$work/dirty-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_TRUST_LOG="$work/dirty-trust" \
  USERLAND_DATA_DIR="$dirty_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || dirty_status=$?
assert_checkout_refused dirty "$dirty_home" "$dirty_status"

ancestry_home="$work/ancestry-home"
prepare_existing_checkout "$ancestry_home"
ancestry_status=0
HOME="$ancestry_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_GIT_ANCESTRY_FAIL=1 \
  TEST_OBSERVATION="$work/ancestry-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_TRUST_LOG="$work/ancestry-trust" \
  USERLAND_DATA_DIR="$ancestry_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || ancestry_status=$?
assert_checkout_refused ancestry "$ancestry_home" "$ancestry_status"

cancel_home="$work/cancel-home"
prepare_home "$cancel_home"
cancel_status=0
HOME="$cancel_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/cancel-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=3 \
  USERLAND_DATA_DIR="$cancel_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  USERLAND_UI_MODE=rich \
  USERLAND_UNICODE=1 \
  NO_COLOR=1 \
  sh "$work/bootstrap" >"$work/cancel-output" 2>&1 || cancel_status=$?
[ "$cancel_status" -eq 3 ] || fail "cancelled run returned $cancel_status"
grep -Fq '◇  Deleting ~/.userland' "$work/cancel-output" ||
  fail "cancelled run did not put checkout deletion in the UI"
grep -Fq '└  Cancelled. No changes were applied.' "$work/cancel-output" ||
  fail "cancelled run did not restore the final cancellation message"
expected_cancel_flow='◇  Deleting ~/.userland
 │
 └  Cancelled. No changes were applied.'
grep -Fq "$expected_cancel_flow" "$work/cancel-output" ||
  fail "cancelled run did not pad checkout deletion before the summary"
delete_line=$(grep -nF '◇  Deleting ~/.userland' "$work/cancel-output" | cut -d: -f1)
cancel_line=$(grep -nF '└  Cancelled. No changes were applied.' "$work/cancel-output" | cut -d: -f1)
[ "$delete_line" -lt "$cancel_line" ] || fail "cancelled run closed before deleting the checkout"
if grep -Fq 'userland: remov' "$work/cancel-output"; then
  fail "cancelled run leaked raw checkout cleanup logs"
fi
[ "$(readlink "$cancel_home/.local/bin/userland")" = "$cancel_home/.local/share/userland/releases/$tag/bin/userland" ] ||
  fail "cancelled run did not restore the release command"
[ ! -e "$cancel_home/.userland" ] || fail "cancelled run retained its provisional checkout"

signal_home="$work/signal-home"
prepare_home "$signal_home"
signal_status=0
HOME="$signal_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/signal-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SIGNAL_PARENT=1 \
  TEST_SYNC_STATUS=130 \
  USERLAND_DATA_DIR="$signal_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >"$work/signal-output" 2>&1 || signal_status=$?
[ "$signal_status" -eq 130 ] || fail "interrupted run returned $signal_status"
grep -Fq '[ok] Deleting ~/.userland' "$work/signal-output" ||
  fail "interrupted run did not put checkout deletion in the UI"
grep -Fq '[cancelled] Cancelled. No changes were applied.' "$work/signal-output" ||
  fail "interrupted run did not restore the final cancellation message"
[ ! -e "$signal_home/.userland" ] || fail "interrupted run retained its provisional checkout"
[ "$(readlink "$signal_home/.local/bin/userland")" = "$signal_home/.local/share/userland/releases/$tag/bin/userland" ] ||
  fail "interrupted run did not restore the release command"

retained_home="$work/retained-home"
prepare_home "$retained_home"
retained_status=0
HOME="$retained_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_OBSERVATION="$work/retained-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SIGNAL_PARENT=1 \
  TEST_SYNC_STATUS=130 \
  USERLAND_DATA_DIR="$retained_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  USERLAND_UI_MODE=rich \
  USERLAND_UNICODE=1 \
  NO_COLOR=1 \
  sh "$work/bootstrap" >"$work/retained-output" 2>&1 || retained_status=$?
[ "$retained_status" -eq 130 ] || fail "post-approval interruption returned $retained_status"
[ -f "$retained_home/.userland/.userland-stage" ] ||
  fail "post-approval failure did not retain the canonical stage"
[ "$(readlink "$retained_home/.local/bin/userland")" = "$retained_home/.userland/bin/userland" ] ||
  fail "post-approval failure did not retain the canonical command"
grep -Fq '└  Cancelled. Applied progress was preserved.' "$work/retained-output" ||
  fail "post-approval interruption did not render the retained-progress summary"
if grep -Fq 'Deleting ~/.userland' "$work/retained-output"; then
  fail "post-approval interruption claimed to delete the retained checkout"
fi

cross_release_status=0
HOME="$retained_home" \
  TEST_ARCHIVE="$work/userland-v1.2.4.tar.gz" \
  TEST_COMMIT="$upgrade_commit" \
  TEST_GIT_REMOTE_MAIN="$upgrade_commit" \
  TEST_OBSERVATION="$work/cross-release-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$retained_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/upgrade-bootstrap" >"$work/cross-release-output" 2>&1 || cross_release_status=$?
[ "$cross_release_status" -ne 0 ] || fail "new release accepted an unfinished older stage"
grep -Fq "https://userland.guidotto.dev/$tag" "$work/cross-release-output" ||
  fail "unfinished older stage did not provide its pinned recovery command"

promotion_home="$work/promotion-home"
prepare_home "$promotion_home"
promotion_status=0
HOME="$promotion_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_GIT_CLONE_FAIL=1 \
  TEST_MARK_APPLY_STARTED=1 \
  TEST_OBSERVATION="$work/promotion-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$promotion_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || promotion_status=$?
[ "$promotion_status" -eq 12 ] || fail "failed promotion returned $promotion_status"
[ -f "$promotion_home/.userland/.userland-stage" ] ||
  fail "failed promotion discarded the applied archive stage"
[ ! -d "$promotion_home/.userland/.git" ] || fail "failed promotion published an invalid Git checkout"

printf '%s\n' tampered >>"$promotion_home/.userland/mise.toml"
tampered_status=0
HOME="$promotion_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/tampered-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$promotion_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || tampered_status=$?
[ "$tampered_status" -ne 0 ] || fail "tampered archive stage was accepted"
[ ! -e "$work/tampered-observation" ] || fail "tampered archive stage was executed"

existing_cancel_home="$work/existing-cancel-home"
prepare_existing_checkout "$existing_cancel_home"
printf '%s\n' keep >"$existing_cancel_home/.userland/.git/userland-test-sentinel"
existing_cancel_status=0
HOME="$existing_cancel_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/existing-cancel-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=3 \
  USERLAND_DATA_DIR="$existing_cancel_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || existing_cancel_status=$?
[ "$existing_cancel_status" -eq 3 ] || fail "existing checkout cancellation returned $existing_cancel_status"
[ "$(cat "$existing_cancel_home/.userland/.git/userland-test-sentinel")" = keep ] ||
  fail "existing checkout cancellation modified the checkout"

personal_home="$work/personal-home"
prepare_home "$personal_home"
mkdir "$personal_home/.userland"
printf '%s\n' personal >"$personal_home/.userland/keep"
personal_status=0
HOME="$personal_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/personal-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$personal_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || personal_status=$?
[ "$personal_status" -ne 0 ] || fail "personal ~/.userland directory was accepted"
[ "$(cat "$personal_home/.userland/keep")" = personal ] || fail "personal ~/.userland directory was modified"
[ ! -e "$work/personal-observation" ] || fail "sync ran with a personal ~/.userland directory"

locked_home="$work/locked-home"
prepare_home "$locked_home"
mkdir -p "$locked_home/.local/share/userland/bootstrap.lock"
printf '%s\n' other-run >"$locked_home/.local/share/userland/bootstrap.lock/owner"
locked_status=0
HOME="$locked_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/locked-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$locked_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || locked_status=$?
[ "$locked_status" -ne 0 ] || fail "concurrent bootstrap lock was ignored"
[ "$(cat "$locked_home/.local/share/userland/bootstrap.lock/owner")" = other-run ] ||
  fail "concurrent bootstrap lock was modified"
[ ! -e "$locked_home/.userland" ] || fail "locked bootstrap published a checkout"

unmanaged_home="$work/unmanaged-home"
prepare_home "$unmanaged_home"
ln -s /tmp/unmanaged-userland "$unmanaged_home/.local/bin/userland"
unmanaged_status=0
HOME="$unmanaged_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/unmanaged-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$unmanaged_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || unmanaged_status=$?
[ "$unmanaged_status" -ne 0 ] || fail "unmanaged command was accepted"
[ "$(readlink "$unmanaged_home/.local/bin/userland")" = /tmp/unmanaged-userland ] ||
  fail "unmanaged command was overwritten"
[ ! -e "$work/unmanaged-observation" ] || fail "sync ran after unmanaged command refusal"

regular_home="$work/regular-home"
prepare_home "$regular_home"
printf 'personal command\n' >"$regular_home/.local/bin/userland"
regular_status=0
HOME="$regular_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/regular-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  USERLAND_DATA_DIR="$regular_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || regular_status=$?
[ "$regular_status" -ne 0 ] || fail "regular command was accepted"
[ "$(cat "$regular_home/.local/bin/userland")" = 'personal command' ] ||
  fail "regular command was overwritten"
[ ! -e "$work/regular-observation" ] || fail "sync ran after regular command refusal"

grep -Fq '/opt/homebrew/bin' "$work/bootstrap" || fail "Homebrew bin path is missing"
grep -Fq '/opt/homebrew/sbin' "$work/bootstrap" || fail "Homebrew sbin path is missing"

printf 'bootstrap behavior tests passed\n'
