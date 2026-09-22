#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=release-lib.sh
. "$script_dir/release-lib.sh"

[ "$#" -eq 3 ] || release_die "usage: $0 TAG COMMIT OUTPUT_DIRECTORY"

tag=$1
commit=$2
output=$3

release_validate_tag "$tag" || release_die "invalid SemVer tag: $tag"
[ ! -e "$output" ] || release_die "output already exists: $output"
git cat-file -e "$commit^{commit}" 2>/dev/null || release_die "unknown commit: $commit"
if git cat-file -e "$commit:bin/mise" 2>/dev/null; then
  release_die "bin/mise is release-generated and must not be tracked"
fi
command -v mise >/dev/null 2>&1 || release_die "mise is required to build a release"
command -v go >/dev/null 2>&1 || release_die "Go is required to build a release"
command -v python3 >/dev/null 2>&1 || release_die "python3 is required to build a release"

mise_version=2026.8.9
actual_mise_version=$(mise --version | awk '{ print $1 }')
[ "$actual_mise_version" = "$mise_version" ] ||
  release_die "mise $mise_version is required, found $actual_mise_version"

version=${tag#v}
mkdir -p "$output"
work=$(mktemp -d "${TMPDIR:-/tmp}/userland-release.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$commit")}
case "$epoch" in
  '' | *[!0-9]*) release_die "SOURCE_DATE_EPOCH must be a non-negative integer" ;;
esac

source_tree=$work/source/userland-$version
mkdir -p "$source_tree/bin"
git archive --format=tar "$commit" LICENSE cmd completions cfg go.mod go.sum internal userland.go |
  tar -xf - -C "$source_tree"

if [ -f .gitmodules ]; then
  git config --file .gitmodules --get-regexp '^[^.]+\..*\.path$' |
    while read -r _submodule_key submodule_path; do
      expected_submodule_commit=$(git rev-parse "$commit:$submodule_path")
      [ -d "$submodule_path" ] || release_die "submodule is not initialized: $submodule_path"
      actual_submodule_commit=$(git -C "$submodule_path" rev-parse HEAD)
      [ "$actual_submodule_commit" = "$expected_submodule_commit" ] ||
        release_die "submodule checkout does not match $commit: $submodule_path"
      mkdir -p "$source_tree/$submodule_path"
      git -C "$submodule_path" archive "$expected_submodule_commit" |
        tar -xf - -C "$source_tree/$submodule_path"
    done
fi

archive_darwin_arm64="userland-$tag.tar.gz"
archive_linux_arm64="userland-$tag-linux-arm64.tar.gz"
archive_linux_x64="userland-$tag-linux-x64.tar.gz"
build_target() {
  target=$1
  goos=$2
  goarch=$3
  archive=$4
  release_tree=$work/$target/userland-$version
  mkdir -p "$release_tree"
  cp -pR "$source_tree/." "$release_tree/"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go -C "$release_tree" build \
    -trimpath -buildvcs=false -ldflags='-s -w' -o bin/userland ./cmd/userland
  binary_header=$(od -An -tx1 -N8 "$release_tree/bin/userland" | tr -d ' \n')
  case "$target" in
    darwin-arm64) expected_header=cffaedfe0c000001 ;;
    *) expected_header=7f454c4602010100 ;;
  esac
  [ "$binary_header" = "$expected_header" ] || release_die "$target binary has unexpected header: $binary_header"
  rm -rf "$release_tree/cmd" "$release_tree/internal" "$release_tree/go.mod" "$release_tree/go.sum" "$release_tree/userland.go"
  mise generate bootstrap --version "$mise_version" --write "$release_tree/bin/mise" >/dev/null
  python3 "$script_dir/create-archive.py" "$release_tree" "$output/$archive" "$epoch"
  release_sha256 "$output/$archive"
}
archive_sum_darwin_arm64=$(build_target darwin-arm64 darwin arm64 "$archive_darwin_arm64")
archive_sum_linux_arm64=$(build_target linux-arm64 linux arm64 "$archive_linux_arm64")
archive_sum_linux_x64=$(build_target linux-x64 linux amd64 "$archive_linux_x64")
commit=$(git rev-parse "$commit^{commit}")
sed \
  -e "s|@USERLAND_TAG@|$tag|g" \
  -e "s|@USERLAND_COMMIT@|$commit|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256_DARWIN_ARM64@|$archive_sum_darwin_arm64|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256_LINUX_ARM64@|$archive_sum_linux_arm64|g" \
  -e "s|@USERLAND_ARCHIVE_SHA256_LINUX_X64@|$archive_sum_linux_x64|g" \
  "$script_dir/bootstrap-template.sh" >"$output/bootstrap"
chmod 0755 "$output/bootstrap"

(
  cd "$output"
  bootstrap_sum=$(release_sha256 bootstrap)
  printf '%s  %s\n' "$bootstrap_sum" bootstrap
  printf '%s  %s\n' "$archive_sum_darwin_arm64" "$archive_darwin_arm64"
  printf '%s  %s\n' "$archive_sum_linux_arm64" "$archive_linux_arm64"
  printf '%s  %s\n' "$archive_sum_linux_x64" "$archive_linux_x64"
) >"$output/checksums.txt"
