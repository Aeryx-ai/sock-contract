#!/usr/bin/env bash
# release-patch.sh: tags HEAD with the next patch version and publishes a GitHub release.
# CI runs it (make release) after make check passes on a push to main. A HEAD that is
# already tagged, by a minor or major bump made by hand, is left alone.
set -euo pipefail

git fetch --tags --force --quiet

if tag=$(git describe --exact-match --tags HEAD 2>/dev/null); then
  echo "HEAD is already $tag"
  exit 0
fi

last=$(git tag --list 'v*.*.*' --sort=-v:refname | head -1)
if [ -z "$last" ]; then
  echo "no vX.Y.Z tag to bump from" >&2
  exit 1
fi

IFS=. read -r major minor patch <<<"$last"
next="$major.$minor.$((patch + 1))"

git tag -a "$next" -m "$next"
git push origin "$next"
gh release create "$next" --verify-tag --generate-notes --title "$next"
