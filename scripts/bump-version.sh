#!/bin/sh
# Sets the version everywhere it is duplicated, then checks consistency.
#
#   scripts/bump-version.sh 1.0.6
#
# Add the CHANGELOG entry ("## 1.0.6") yourself before running the tests.
set -eu

NEW="${1:-}"
case "$NEW" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) echo "usage: $0 X.Y.Z" >&2; exit 2 ;;
esac

ROOT=$(cd "$(dirname "$0")/.." && pwd)
OLD=$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' "$ROOT/pkg/version/version.go")

sed -i "s/Version   = \"$OLD\"/Version   = \"$NEW\"/" "$ROOT/pkg/version/version.go"
sed -i "s/$OLD/$NEW/g" "$ROOT/INSTALL.md"
sed -i "s/--version $OLD/--version $NEW/; s/v$OLD/v$NEW/" "$ROOT/scripts/install.sh"

echo "version: $OLD -> $NEW"
