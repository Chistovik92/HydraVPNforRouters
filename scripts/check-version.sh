#!/bin/sh
# Fails if a release tag does not match the version in the source tree.
#
#   scripts/check-version.sh v1.0.6
#
# The version lives in pkg/version/version.go; docs, the web UI and scripts
# are checked by "go test ./pkg/version".
set -eu

TAG="${1:-}"
[ -n "$TAG" ] || { echo "usage: $0 vX.Y.Z" >&2; exit 2; }

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC=$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' "$ROOT/pkg/version/version.go")

case "$TAG" in
    v[0-9]*) ;;
    *) echo "tag '$TAG' must look like vX.Y.Z" >&2; exit 2 ;;
esac

# Pre-release suffixes (v1.0.6-debug.1) share the base version.
BASE="${TAG#v}"
BASE="${BASE%%-*}"

if [ "$BASE" != "$SRC" ]; then
    echo "tag $TAG does not match pkg/version/version.go ($SRC)" >&2
    exit 1
fi
echo "ok: $TAG matches version $SRC"
