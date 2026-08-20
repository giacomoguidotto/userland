#!/bin/sh
set -eu

test_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
delivery_dir=$(CDPATH='' cd -- "$test_dir/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-bootstrap-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

tag=v1.2.3
commit=0123456789abcdef0123456789abcdef01234567
fixture="$work/fixture/userland-1.2.3"
mkdir -p "$fixture/bin"

cat >"$fixture/bin/userland" <<'EOF'
#!/bin/sh
readlink "$HOME/.local/bin/userland" >"$TEST_OBSERVATION"
exit "$TEST_SYNC_STATUS"
EOF
cat >"$fixture/bin/mise" <<'EOF'
#!/bin/sh
[ -z "${TEST_TRUST_LOG:-}" ] || printf '%s\n' "$*" >>"$TEST_TRUST_LOG"
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
  "$delivery_dir/bootstrap/bootstrap.sh.in" >"$work/bootstrap"

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
  destination=$5
  mkdir -p "$destination/.git" "$destination/bin"
  cp "$TEST_REPO_COMMAND" "$destination/bin/userland"
  chmod +x "$destination/bin/userland"
  printf 'min_version = "2026.8.9"\n' >"$destination/mise.toml"
  exit 0
fi
if [ "$1" = ls-remote ]; then
  [ "${GIT_CONFIG_GLOBAL:-}" = /dev/null ] || exit 8
  [ "${GIT_CONFIG_NOSYSTEM:-}" = 1 ] || exit 8
  printf '%s\trefs/heads/main\n' "${TEST_GIT_REMOTE_MAIN:-$TEST_COMMIT}"
  exit 0
fi
if [ "$1" = -C ]; then
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
      ;;
    symbolic-ref)
      printf '%s\n' "${TEST_GIT_BRANCH:-main}"
      ;;
    rev-parse)
      case "$4" in
        --is-inside-work-tree) printf 'true\n' ;;
        --abbrev-ref) printf '%s\n' "${TEST_GIT_UPSTREAM:-origin/main}" ;;
        refs/remotes/origin/main*) printf '%s\n' "${TEST_GIT_REMOTE_MAIN:-$TEST_COMMIT}" ;;
        *) printf '%s\n' "${TEST_GIT_HEAD:-$TEST_COMMIT}" ;;
      esac
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

cat >"$work/repo-userland" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$work/repo-userland"

attention_home="$work/attention-home"
prepare_home "$attention_home"
attention_status=0
HOME="$attention_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/attention-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=2 \
  USERLAND_DATA_DIR="$attention_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || attention_status=$?
[ "$attention_status" -eq 0 ] || fail "completed attention run returned $attention_status"
grep -Fq "$attention_home/.local/share/userland/releases/$tag/bin/userland" "$work/attention-observation" ||
  fail "release command was not linked before sync"
[ "$(readlink "$attention_home/.local/bin/userland")" = "$attention_home/.local/share/userland/repo/bin/userland" ] ||
  fail "attention run did not finish the repository link"

rerun_status=0
HOME="$attention_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/rerun-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=0 \
  TEST_TRUST_LOG="$work/rerun-trust" \
  USERLAND_DATA_DIR="$attention_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || rerun_status=$?
[ "$rerun_status" -eq 0 ] || fail "safe rerun returned $rerun_status"
grep -Fq "$attention_home/.local/share/userland/repo/mise.toml" "$work/rerun-trust" ||
  fail "safe checkout was not trusted"
[ "$(readlink "$attention_home/.local/bin/userland")" = "$attention_home/.local/share/userland/repo/bin/userland" ] ||
  fail "safe rerun did not restore the repository link"

prepare_existing_checkout() {
  existing_home=$1
  prepare_home "$existing_home"
  existing_repo="$existing_home/.local/share/userland/repo"
  mkdir -p "$existing_repo/.git" "$existing_repo/bin"
  cp "$work/repo-userland" "$existing_repo/bin/userland"
  chmod +x "$existing_repo/bin/userland"
  printf 'original checkout config\n' >"$existing_repo/mise.toml"
}

assert_checkout_refused() {
  case_name=$1
  case_home=$2
  case_status=$3
  [ "$case_status" -ne 0 ] || fail "$case_name checkout was accepted"
  [ "$(cat "$case_home/.local/share/userland/repo/mise.toml")" = 'original checkout config' ] ||
    fail "$case_name checkout was modified"
  [ "$(readlink "$case_home/.local/bin/userland")" = "$case_home/.local/share/userland/releases/$tag/bin/userland" ] ||
    fail "$case_name checkout replaced the release command"
  if grep -Fq "$case_home/.local/share/userland/repo/mise.toml" "$work/$case_name-trust"; then
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

fatal_home="$work/fatal-home"
prepare_home "$fatal_home"
fatal_status=0
HOME="$fatal_home" \
  TEST_ARCHIVE="$work/userland-v1.2.3.tar.gz" \
  TEST_COMMIT="$commit" \
  TEST_OBSERVATION="$work/fatal-observation" \
  TEST_REPO_COMMAND="$work/repo-userland" \
  TEST_SYNC_STATUS=7 \
  USERLAND_DATA_DIR="$fatal_home/.local/share/userland" \
  USERLAND_NO_TTY=1 \
  sh "$work/bootstrap" >/dev/null 2>&1 || fatal_status=$?
[ "$fatal_status" -eq 7 ] || fail "fatal run returned $fatal_status"
[ "$(readlink "$fatal_home/.local/bin/userland")" = "$fatal_home/.local/share/userland/releases/$tag/bin/userland" ] ||
  fail "fatal run did not preserve the release command"
[ ! -e "$fatal_home/.local/share/userland/repo" ] || fail "fatal run attempted the repository clone"

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
