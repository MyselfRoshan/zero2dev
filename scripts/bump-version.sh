#!/usr/bin/env bash
# Sets the version everywhere it is written down and opens a CHANGELOG.md section for it:
# VERSION, web/package.json, the default in cmd/zero2dev/main.go and the installer's default.
# Then fill in the new "## <version>" section and open a pull request into the release branch.
#
# Usage: scripts/bump-version.sh 0.0.5
set -euo pipefail
cd "$(dirname "$0")/.."

NEW="${1:-}"
[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || {
  echo "usage: $0 <version like 1.2.3>" >&2
  exit 2
}
OLD="$(tr -d ' \r\n' < VERSION)"

printf '%s\n' "$NEW" > VERSION
sed -i "s/^  \"version\": \"${OLD//./\\.}\",/  \"version\": \"${NEW}\",/" web/package.json
sed -i "s/^var version = \"${OLD//./\\.}\"$/var version = \"${NEW}\"/" cmd/zero2dev/main.go
sed -i "s/Version: \"${OLD//./\\.}\",/Version: \"${NEW}\",/" internal/installer/installer.go

if ! grep -qx "## ${NEW}" CHANGELOG.md; then
  # The new section goes above the first existing "## " heading.
  awk -v v="$NEW" '!done && /^## / { print "## " v "\n\n- Describe the changes of " v " here.\n"; done = 1 } { print }' CHANGELOG.md > CHANGELOG.md.tmp
  mv CHANGELOG.md.tmp CHANGELOG.md
fi

echo "Version ${OLD} -> ${NEW}. Now write the '## ${NEW}' section in CHANGELOG.md."
bash scripts/release-check.sh > /dev/null
