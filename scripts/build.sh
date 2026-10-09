#!/bin/bash
# Build script for HydraVPN for Router
# Cross-compiles release binaries. Asset names match what scripts/install.sh
# and scripts/update.sh download: hydravpn-router-<version>-<os>-<arch>
#
# Usage: scripts/build.sh [version] [output-dir]
# The version defaults to the one in pkg/version/version.go.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE="github.com/Chistovik92/hydravpn-router"
DEFAULT_VERSION="$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' "$ROOT_DIR/pkg/version/version.go")"
VERSION="${1:-$DEFAULT_VERSION}"
VERSION="${VERSION#v}"
OUTPUT_DIR="${2:-$ROOT_DIR/dist}"

# arch|GOARCH|extra env: every architecture is compiled once ...
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

# ... and published under the name of every OS it is installed on:
# hydravpn-router-<version>-<os>-<arch>. The files of one architecture are
# identical; the OS in the name tells the installer and the web UI updater
# which file belongs to which system.
#   openwrt     OpenWrt (opkg/apk)
#   keeneticos  KeeneticOS with Entware (/opt)
#   routeros    MikroTik RouterOS 7 container (ARM, ARM64, x86)
OS_ARCHES=(
    "openwrt|amd64 arm64 armv7 armv6 mips mipsle 386"
    "keeneticos|arm64 armv7 armv5 mips mipsle amd64"
    "routeros|arm64 armv7 amd64"
)

BUILD_TAGS="netgo,osusergo"
# Debug pre-releases (1.2.2-debug.1) also carry pprof, served on 127.0.0.1 only.
case "$VERSION" in *-debug*) BUILD_TAGS="$BUILD_TAGS,pprof" ;; esac
COMMIT="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w \
    -X $MODULE/pkg/version.Version=$VERSION \
    -X $MODULE/pkg/version.Commit=$COMMIT \
    -X $MODULE/pkg/version.Date=$DATE \
    -X $MODULE/pkg/version.BuiltBy=$(whoami)@$(hostname) \
    -X $MODULE/pkg/version.GoVersion=$(go env GOVERSION)"

mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_DIR/checksums.txt"

echo "Building HydraVPN for Router $VERSION"
echo "Output directory: $OUTPUT_DIR"
echo ""

OBJ_DIR="$(mktemp -d)"
trap 'rm -rf "$OBJ_DIR"' EXIT

for target in "${ARCHES[@]}"; do
    IFS='|' read -r arch goarch extra <<< "$target"
    echo "Building $arch..."
    env_vars=("GOOS=linux" "GOARCH=$goarch" "CGO_ENABLED=0")
    [[ -n "$extra" ]] && env_vars+=("$extra")

    (cd "$ROOT_DIR" && env "${env_vars[@]}" go build         -trimpath         -buildvcs=false         -tags "$BUILD_TAGS"         -ldflags "$LDFLAGS"         -o "$OBJ_DIR/$arch"         ./cmd/hydravpn-router)

    # Optional: UPX=1 compresses the binaries (smaller on flash, slightly more RAM at start).
    if [ "${UPX:-0}" = "1" ] && command -v upx >/dev/null 2>&1; then
        upx --best --lzma -q "$OBJ_DIR/$arch" >/dev/null || true
    fi
done

for entry in "${OS_ARCHES[@]}"; do
    os="${entry%%|*}"
    for arch in ${entry#*|}; do
        cp "$OBJ_DIR/$arch" "$OUTPUT_DIR/hydravpn-router-$VERSION-$os-$arch"
    done
done

echo "Building windows-amd64..."
(cd "$ROOT_DIR" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false     -tags "$BUILD_TAGS" -ldflags "$LDFLAGS" -o "$OUTPUT_DIR/hydravpn-router-$VERSION-windows-amd64.exe" ./cmd/hydravpn-router)

# "sha256sum" on Windows prints "*name" (binary mode); keep the plain form.
(cd "$OUTPUT_DIR" && for f in hydravpn-router-"$VERSION"-*; do
    sha256sum "$f" | sed 's/ \*/  /' >> checksums.txt
done)

# Packages (.ipk for OpenWrt and KeeneticOS) are built from these binaries.
if [ "${PACKAGES:-1}" = "1" ]; then
    sh "$ROOT_DIR/scripts/build-packages.sh" "$VERSION" "$OUTPUT_DIR"
fi

cat > "$OUTPUT_DIR/RELEASE_NOTES.md" <<EOF
# HydraVPN for Router $VERSION

Install or update on the router (OpenWrt, KeeneticOS with Entware):

    curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh

MikroTik RouterOS 7 (container):

    /tool fetch url="https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.rsc" dst-path=hydravpn-install.rsc
    /import hydravpn-install.rsc

Files are named after the OS they are installed on:
\`hydravpn-router-$VERSION-<openwrt|keeneticos|routeros>-<arch>\` (binary),
\`hydravpn-router-<openwrt|keeneticos>_${VERSION}_<arch>.ipk\` (package),
\`hydravpn-router-routeros-$VERSION-<arch>.tar\` (container image).

### Binaries
EOF

for f in "$OUTPUT_DIR"/hydravpn-router-"$VERSION"-*; do
    size=$(du -h "$f" | cut -f1)
    echo "- \`$(basename "$f")\` ($size)" >> "$OUTPUT_DIR/RELEASE_NOTES.md"
done

echo ""
echo "Release notes: $OUTPUT_DIR/RELEASE_NOTES.md"
echo "Checksums:     $OUTPUT_DIR/checksums.txt"
echo "Upload all files from $OUTPUT_DIR to the GitHub release v$VERSION."
