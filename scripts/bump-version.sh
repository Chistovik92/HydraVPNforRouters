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

# The old version as a regex: dots escaped, and not part of a longer number,
# so addresses such as 172.17.0.2 are never touched (1.2.3 once matched
# inside "1.2.37.0.2").
OLD_RE=$(printf '%s' "$OLD" | sed 's/\./\\./g')
BOUNDED="s/(^|[^0-9.])$OLD_RE([^0-9.]|\.[^0-9]|\.?$)/\1$NEW\2/g"

sed -i "s/Version   = \"$OLD\"/Version   = \"$NEW\"/" "$ROOT/pkg/version/version.go"
# Applied twice: adjacent matches ("1.2.3 … 1.2.3") share a boundary character.
sed -i -E "$BOUNDED; $BOUNDED" "$ROOT/INSTALL.md"
sed -i "s/--version $OLD_RE/--version $NEW/" "$ROOT/scripts/install.sh"
sed -i "s/^:local ver \"$OLD_RE\"/:local ver \"$NEW\"/; s/hydraVersion \"$OLD_RE\"/hydraVersion \"$NEW\"/" "$ROOT/scripts/install.rsc"

echo "version: $OLD -> $NEW"
