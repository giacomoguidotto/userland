#!/bin/sh

release_die() {
  printf 'userland release: %s\n' "$*" >&2
  exit 1
}

release_validate_tag() {
  tag=$1
  version=${tag#v}

  [ "$version" != "$tag" ] || return 1

  case "$version" in
    *+* | *[!0-9A-Za-z.-]*) return 1 ;;
  esac

  core=${version%%-*}
  major=${core%%.*}
  remainder=${core#*.}
  [ "$remainder" != "$core" ] || return 1
  minor=${remainder%%.*}
  patch=${remainder#*.}
  [ "$patch" != "$remainder" ] || return 1

  release_validate_number "$major" || return 1
  release_validate_number "$minor" || return 1
  release_validate_number "$patch" || return 1

  if [ "$version" != "$core" ]; then
    prerelease=${version#*-}
    [ -n "$prerelease" ] || return 1
    old_ifs=$IFS
    IFS=.
    # shellcheck disable=SC2086 # Split prerelease identifiers on dots.
    set -- $prerelease
    IFS=$old_ifs
    [ "$#" -gt 0 ] || return 1
    for identifier; do
      [ -n "$identifier" ] || return 1
      case "$identifier" in
        *[!0-9A-Za-z-]*) return 1 ;;
      esac
      case "$identifier" in
        0 | *[!0-9]*) ;;
        0*) return 1 ;;
      esac
    done
  fi
}

release_validate_number() {
  number=$1
  case "$number" in
    '' | *[!0-9]*) return 1 ;;
    0) return 0 ;;
    0*) return 1 ;;
  esac
}

release_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}
