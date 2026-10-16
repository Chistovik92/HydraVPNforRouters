#!/bin/bash
# Builds the cores that have no router builds upstream, for every router
# architecture the service is built for, as release files
#   core-<binary>-linux-<arch>
# The service installs them from the release (POST /api/v1/cores/<name>/install).
# Versions are pinned here and in docs/components.tsv; bump both together.
#
# Usage: scripts/build-cores.sh [output-dir]

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${1:-$ROOT_DIR/dist}"

# name|repository|ref (tag or commit)|package
CORES=(
    "amneziawg-go|https://github.com/amnezia-vpn/amneziawg-go.git|v3.1.20260828|."
    "openflux|https://github.com/p1neappleXpress/OpenFlux.git|v0.13.0|."
    # Upstream is archived; the commit the Hydra app builds.
    "olcrtc|https://github.com/openlibrecommunity/olcrtc.git|f3ad8fb7c0d0fa981423f83269526fa94077c23d|./cmd/olcrtc"
)

# Same targets as scripts/build.sh.
ARCHES=(
    "amd64|amd64|"
    "arm64|arm64|"
    "armv7|arm|GOARM=7"
    "armv6|arm|GOARM=6"
    "armv5|arm|GOARM=5"
    "mips|mips|GOMIPS=softfloat"
    "mipsle|mipsle|GOMIPS=softfloat"
    "386|386|"
)

mkdir -p "$OUTPUT_DIR"
SRC_DIR="$(mktemp -d)"
trap 'rm -rf "$SRC_DIR"' EXIT

for core in "${CORES[@]}"; do
    IFS='|' read -r name repo ref pkg <<< "$core"
    echo "Fetching $name $ref"
    git -C "$SRC_DIR" init -q "$name"
    git -C "$SRC_DIR/$name" remote add origin "$repo"
    git -C "$SRC_DIR/$name" fetch -q --depth 1 origin "$ref"
    git -C "$SRC_DIR/$name" checkout -q FETCH_HEAD

    for target in "${ARCHES[@]}"; do
        IFS='|' read -r arch goarch extra <<< "$target"
        out="$OUTPUT_DIR/core-$name-linux-$arch"
        env_vars=("GOOS=linux" "GOARCH=$goarch" "CGO_ENABLED=0")
        [[ -n "$extra" ]] && env_vars+=("$extra")
        if (cd "$SRC_DIR/$name" && env "${env_vars[@]}" go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out" "$pkg"); then
            echo "  $arch ok"
        else
            # A core that does not build for one CPU must not stop the release:
            # that architecture just has no such core.
            echo "  $arch FAILED (skipped)" >&2
            rm -f "$out"
        fi
    done
done

(cd "$OUTPUT_DIR" && for f in core-*-linux-*; do
    [ -f "$f" ] && sha256sum "$f" | sed 's/ \*/  /' >> checksums.txt
done)
echo "Cores: $OUTPUT_DIR/core-*"
