#!/bin/sh
# Compares the versions in docs/components.tsv with the latest GitHub releases.
# Prints one line per component (every component is checked); exit status 1
# when something is newer than the tested version.
#
#   scripts/check-upstream.sh          # needs curl (or gh) and internet
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)

# Repos that label every release "pre-release" (Xray-core has since June
# 2026), so /releases/latest returns an old one: take the newest release.
ANY_RELEASE_REPOS="XTLS/Xray-core"

# tag REPO PATH JQ: the tag_name of a release from the GitHub API.
tag() {
    if command -v gh >/dev/null 2>&1; then
        gh api "repos/$1/$2" --jq "$3" 2>/dev/null
    else
        curl -fsSL "https://api.github.com/repos/$1/$2" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
    fi
}

latest() {
    case " $ANY_RELEASE_REPOS " in
    *" $1 "*) tag "$1" "releases?per_page=1" '.[0].tag_name' ;;
    *) tag "$1" releases/latest .tag_name ;;
    esac
}

# newer A B: true when version A sorts after B.
newer() {
    [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" = "$1" ]
}

# The loop runs in a pipeline subshell, so it reports through its output
# instead of exiting: an early exit used to skip every later component.
report=$(grep -v '^#' "$ROOT/docs/components.tsv" | while IFS="$(printf '\t')" read -r name repo tested _role; do
    [ -n "$name" ] || continue
    t=$(latest "$repo" || true)
    cur=${t#v}
    if [ -z "$cur" ]; then
        echo "?      $name: cannot read the latest release of $repo"
    elif newer "$cur" "$tested"; then
        echo "NEWER  $name: tested $tested, latest $cur ($repo)"
    else
        echo "ok     $name $tested (latest $cur)"
    fi
done)

echo "$report"
case "$report" in
*NEWER*) exit 1 ;;
esac
