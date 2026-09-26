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

# target|GOOS|GOARCH|extra env
TARGETS=(
    "linux-amd64|linux|amd64|"
    "linux-arm64|linux|arm64|"
    "linux-armv7|linux|arm|GOARM=7"
    "linux-armv6|linux|arm|GOARM=6"
    "linux-mips|linux|mips|GOMIPS=softfloat"
    "linux-mipsle|linux|mipsle|GOMIPS=softfloat"
    "linux-mips64|linux|mips64|GOMIPS64=softfloat"
    "linux-mips64le|linux|mips64le|GOMIPS64=softfloat"
    "linux-386|linux|386|"
    "windows-amd64|windows|amd64|"
)

BUILD_TAGS="netgo,osusergo"
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

for target in "${TARGETS[@]}"; do
    IFS='|' read -r name goos goarch extra <<< "$target"
    output_name="hydravpn-router-$VERSION-$name"
    [[ "$goos" == "windows" ]] && output_name="$output_name.exe"

    echo "Building $name..."
    env_vars=("GOOS=$goos" "GOARCH=$goarch" "CGO_ENABLED=0")
    [[ -n "$extra" ]] && env_vars+=("$extra")

    (cd "$ROOT_DIR" && env "${env_vars[@]}" go build \
        -trimpath \
        -tags "$BUILD_TAGS" \
        -ldflags "$LDFLAGS" \
        -o "$OUTPUT_DIR/$output_name" \
        ./cmd/hydravpn-router)

    # "sha256sum" on Windows prints "*name" (binary mode); keep the plain form.
    (cd "$OUTPUT_DIR" && sha256sum "$output_name" | sed 's/ \*/  /' >> checksums.txt)
    echo "  -> $OUTPUT_DIR/$output_name"
done

cat > "$OUTPUT_DIR/RELEASE_NOTES.md" <<EOF
# HydraVPN for Router $VERSION

Install or update on the router:

    curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh

### Binaries
EOF

for target in "${TARGETS[@]}"; do
    IFS='|' read -r name goos _ _ <<< "$target"
    output_name="hydravpn-router-$VERSION-$name"
    [[ "$goos" == "windows" ]] && output_name="$output_name.exe"
    if [[ -f "$OUTPUT_DIR/$output_name" ]]; then
        size=$(du -h "$OUTPUT_DIR/$output_name" | cut -f1)
        echo "- \`$output_name\` ($size)" >> "$OUTPUT_DIR/RELEASE_NOTES.md"
    fi
done

echo ""
echo "Release notes: $OUTPUT_DIR/RELEASE_NOTES.md"
echo "Checksums:     $OUTPUT_DIR/checksums.txt"
echo "Upload all files from $OUTPUT_DIR to the GitHub release v$VERSION."
