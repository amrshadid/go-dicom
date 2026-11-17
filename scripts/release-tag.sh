#!/usr/bin/env bash
# Decides whether a run of the release workflow builds, and whether it
# publishes, and under which tag.
#
#   release-tag.sh tag <ref-name>     a tag was pushed: release it
#   release-tag.sh main               main was pushed: release its version, if it is new
#   release-tag.sh dispatch           started by hand: build, publish nothing
#
# It prints three lines, tag=, build= and publish=, in the form GitHub Actions
# reads as step outputs, and says why on standard error.
#
# Why "main" exists at all: the workflow used to run only for a pushed tag,
# so a release merged into main stayed unpublished until someone tagged the
# right commit by hand (#287). A job cannot fix that by pushing the tag
# itself, since a tag pushed with the workflow's own token starts no
# workflow. So the same run that sees the merge publishes, and the release
# it creates carries the tag.
#
# main publishes only when both hold:
#   - main.go's Version has no tag yet, which is what makes it new;
#   - CHANGELOG.md has a section for it, which is what makes it a release
#     and not a version bumped ahead of its notes.
# Any other push to main builds nothing.
set -euo pipefail

event="${1:-}"
root="$(git rev-parse --show-toplevel)"

say() { echo "release-tag: $*" >&2; }

version_in_source() {
  sed -n 's/^var Version = "\(.*\)"$/\1/p' "$root/main.go" | head -1
}

case "$event" in
  tag)
    tag="${2:?release-tag.sh tag <ref-name>}"
    say "tag $tag was pushed"
    echo "tag=$tag"; echo "build=true"; echo "publish=true"
    ;;

  main)
    version="$(version_in_source)"
    if [ -z "$version" ]; then
      say "main.go declares no Version"
      exit 1
    fi
    tag="v$version"
    echo "tag=$tag"
    if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
      say "$tag is already tagged: nothing to release"
      echo "build=false"; echo "publish=false"
    elif ! grep -q "^## \[$version\]" "$root/CHANGELOG.md"; then
      say "CHANGELOG.md has no section for $version: not a release"
      echo "build=false"; echo "publish=false"
    else
      say "$tag is new and has its notes: releasing"
      echo "build=true"; echo "publish=true"
    fi
    ;;

  dispatch)
    version="$(version_in_source)"
    say "started by hand: building v$version, publishing nothing"
    echo "tag=v$version"; echo "build=true"; echo "publish=false"
    ;;

  *)
    say "usage: release-tag.sh tag <ref-name> | main | dispatch"
    exit 2
    ;;
esac
