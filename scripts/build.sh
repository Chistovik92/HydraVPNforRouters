#!/bin/bash
# Build script for Podkop Plus
# Cross-compiles for all supported platforms

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${1:-1.0.0}"
OUTPUT_DIR="${2:-$ROOT_DIR/dist}"

# Platforms to build for
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "linux/arm/v7"
    "linux/arm/v6"
    "linux/mips"
    "linux/mipsle"
    "linux/mips64"
    "linux/mips64le"
    "linux/386"
    "linux/ppc64le"
    "linux/s390x"
)

# Build tags
BUILD_TAGS="netgo,osusergo"

# LDFLAGS
LDFLAGS="-s -w \
    -X github.com/Chistovik92/podkop-plus/pkg/version.Version=$VERSION \
    -X github.com/Chistovik92/podkop-plus/pkg/version.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown') \
    -X github.com/Chistovik92/podkop-plus/pkg/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
    -X github.com/Chistovik92/podkop-plus/pkg/version.BuiltBy=$(whoami)@$(hostname) \
    -X github.com/Chistovik92/podkop-plus/pkg/version.GoVersion=$(go version | awk '{print $3}')"

mkdir -p "$OUTPUT_DIR"

echo "Building Podkop Plus $VERSION"
echo "Output directory: $OUTPUT_DIR"
echo ""

# Build for each platform
for platform in "${PLATFORMS[@]}"; do
    GOOS="${platform%/*}"
    GOARCH="${platform#*/}"
    
    # Handle ARM variants
    GOARM=""
    if [[ "$GOARCH" == "arm/v7" ]]; then
        GOARCH="arm"
        GOARM="7"
    elif [[ "$GOARCH" == "arm/v6" ]]; then
        GOARCH="arm"
        GOARM="6"
    fi
    
    output_name="podkop-plus-$VERSION-$GOOS-$GOARCH"
    if [[ -n "$GOARM" ]]; then
        output_name="podkop-plus-$VERSION-$GOOS-$GOARCH$GOARM"
    fi
    
    echo "Building for $GOOS/$GOARCH${GOARM:+$GOARM}..."
    
    env_vars=(
        "GOOS=$GOOS"
        "GOARCH=$GOARCH"
        "CGO_ENABLED=0"
    )
    
    if [[ -n "$GOARM" ]]; then
        env_vars+=("GOARM=$GOARM")
    fi
    
    if ! "${env_vars[@]}" go build \
        -tags "$BUILD_TAGS" \
        -ldflags "$LDFLAGS" \
        -o "$OUTPUT_DIR/$output_name" \
        "$ROOT_DIR/cmd/podkop-plus"; then
        echo "Failed to build for $GOOS/$GOARCH"
        exit 1
    fi
    
    # Create checksums
    (cd "$OUTPUT_DIR" && sha256sum "$output_name" >> checksums.txt)
    
    echo "  -> $OUTPUT_DIR/$output_name"
done

echo ""
echo "Build complete!"
echo "Artifacts:"
ls -la "$OUTPUT_DIR"/podkop-plus-*

# Create package archives
echo ""
echo "Creating package archives..."

# OpenWRT IPK/APK
if command -v docker &> /dev/null; then
    echo "Building OpenWRT packages..."
    docker build -f "$ROOT_DIR/build/openwrt/Dockerfile" \
        --build-arg VERSION="$VERSION" \
        -t "podkop-plus-openwrt:$VERSION" \
        "$ROOT_DIR"
    
    # Extract packages
    docker run --rm "podkop-plus-openwrt:$VERSION" tar -czf - -C /output . | tar -xzf - -C "$OUTPUT_DIR/openwrt"
fi

# KeeneticOS KNP
echo "Building KeeneticOS package..."
mkdir -p "$OUTPUT_DIR/keenetic"
# The KNP package would be built by the keenetic platform code

# MikroTik NPK
echo "Building MikroTik package..."
mkdir -p "$OUTPUT_DIR/mikrotik"
# The NPK package would be built by the mikrotik platform code

# Create release notes
cat > "$OUTPUT_DIR/RELEASE_NOTES.md" <<EOF
# Podkop Plus $VERSION

## Multi-platform DPI Bypass Solution

### Supported Platforms
- **OpenWRT** (IPK/APK packages)
- **KeeneticOS** (KNP packages, Entware)
- **MikroTik RouterOS** (NPK packages, Docker containers)

### Features
- sing-box proxy core with multi-protocol support (VMess, VLESS, Trojan, Shadowsocks, Hysteria2, etc.)
- zapret/zapret2 DPI bypass (NFQWS/NFQWS2)
- ByeDPI (ciadpi) DPI bypass
- Subscription management with auto-update
- DNS management with failover and FakeIP
- Flexible routing rules (domain, IP, geoip, geosite, process, etc.)
- Clash API compatible web UI
- LuCI web interface for OpenWRT
- Native web UI for KeeneticOS and MikroTik

### Binaries
EOF

for platform in "${PLATFORMS[@]}"; do
    GOOS="${platform%/*}"
    GOARCH="${platform#*/}"
    GOARM=""
    if [[ "$GOARCH" == "arm/v7" ]]; then
        GOARCH="arm"
        GOARM="7"
    elif [[ "$GOARCH" == "arm/v6" ]]; then
        GOARCH="arm"
        GOARM="6"
    fi
    
    output_name="podkop-plus-$VERSION-$GOOS-$GOARCH"
    if [[ -n "$GOARM" ]]; then
        output_name="podkop-plus-$VERSION-$GOOS-$GOARCH$GOARM"
    fi
    
    if [[ -f "$OUTPUT_DIR/$output_name" ]]; then
        size=$(du -h "$OUTPUT_DIR/$output_name" | cut -f1)
        echo "- \`$output_name\` ($size) - $GOOS/$GOARCH${GOARM:+$GOARM}" >> "$OUTPUT_DIR/RELEASE_NOTES.md"
    fi
done

echo ""
echo "Release notes: $OUTPUT_DIR/RELEASE_NOTES.md"
echo "Done!"