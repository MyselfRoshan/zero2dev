#!/usr/bin/env bash
# Checks that the repository is ready to release the version in VERSION:
#   - VERSION is a version like 1.2.3 (or 1.2.3-rc.1);
#   - CHANGELOG.md has a "## <version>" section (it becomes the release notes);
#   - web/package.json and the default in cmd/zero2dev/main.go carry the same version;
#   - cmd/zero2dev/rsrc_windows_amd64.syso (the exe's icon and version info) is up to date;
#   - whether the tag v<version> already exists on origin.
#
# Prints "version=<v>" and "released=<true|false>" lines (ready for $GITHUB_OUTPUT).
# With --require-new it fails when v<version> is already released, so a pull request into
# the release branch cannot be merged without a version bump.
#
# Usage: scripts/release-check.sh [--require-new]
set -euo pipefail
cd "$(dirname "$0")/.."

fail() {
  echo "::error::$*" >&2
  exit 1
}

VERSION="$(tr -d ' \r\n' < VERSION)"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || fail "VERSION '$VERSION' is not a version like 1.2.3"

grep -qx "## ${VERSION}" CHANGELOG.md || fail "CHANGELOG.md has no '## ${VERSION}' section (it becomes the release notes)"

pkg="$(sed -n 's/^  "version": "\([^"]*\)",*$/\1/p' web/package.json | head -n1)"
[[ "$pkg" == "$VERSION" ]] || fail "web/package.json has version '$pkg', VERSION has '$VERSION'"

def="$(sed -n 's/^var version = "\([^"]*\)"$/\1/p' cmd/zero2dev/main.go | head -n1)"
[[ "$def" == "$VERSION" ]] || fail "cmd/zero2dev/main.go has version '$def', VERSION has '$VERSION'"

if command -v node > /dev/null; then
  node scripts/winres.mjs --check > /dev/null || fail "cmd/zero2dev/rsrc_windows_amd64.syso is out of date: run node scripts/winres.mjs"
fi

released=false
if git ls-remote --exit-code --tags origin "refs/tags/v${VERSION}" > /dev/null 2>&1; then
  released=true
fi

if [[ "${1:-}" == "--require-new" && "$released" == true ]]; then
  fail "v${VERSION} is already released. Bump the version first: scripts/bump-version.sh <new version>"
fi

echo "version=${VERSION}"
echo "released=${released}"
