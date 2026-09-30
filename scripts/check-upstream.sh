#!/bin/sh
# Compares the versions in docs/components.tsv with the latest GitHub releases.
# Prints one line per component; exit status 1 when something is newer.
#
#   scripts/check-upstream.sh          # needs curl (or gh) and internet
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
newer=0

latest() {
    if command -v gh >/dev/null 2>&1; then
        gh api "repos/$1/releases/latest" --jq .tag_name 2>/dev/null
    else
        curl -fsSL "https://api.github.com/repos/$1/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
    fi
}

grep -v '^#' "$ROOT/docs/components.tsv" | while IFS="$(printf '\t')" read -r name repo tested role; do
    [ -n "$name" ] || continue
    tag=$(latest "$repo" || true)
    cur=${tag#v}
    if [ -z "$cur" ]; then
        echo "?      $name: cannot read the latest release of $repo"
    elif [ "$cur" = "$tested" ]; then
        echo "ok     $name $tested"
    else
        echo "NEWER  $name: tested $tested, latest $cur ($repo)"
        exit 1
    fi
done || newer=1

exit $newer
