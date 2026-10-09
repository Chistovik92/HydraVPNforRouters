#!/bin/sh
# Builds the .ipk packages of a release from the binaries that
# scripts/build.sh put in the output directory. No OpenWrt SDK and no Docker.
#
#   scripts/build-packages.sh [version] [dist-dir]
#
# Packages are named after the OS they are installed on:
#   hydravpn-router-openwrt_<ver>_<openwrt arch>.ipk      (opkg print-architecture)
#   hydravpn-router-keeneticos_<ver>_<Entware arch>.ipk   (opkg print-architecture)
# The RouterOS image (hydravpn-router-routeros-<ver>-<arch>.tar) is a container
# image and is built with Docker: see the release workflow and INSTALL.md.
#
# Every package also gets a line in <dist-dir>/sizes.md (compressed package
# size and installed size).
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
VERSION="${1:-$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' "$ROOT/pkg/version/version.go")}"
VERSION="${VERSION#v}"
DIST="${2:-$ROOT/dist}"

# OpenWrt package architecture | binary arch (suffix of the binary name)
OPENWRT="x86_64|amd64
aarch64_generic|arm64
aarch64_cortex-a53|arm64
aarch64_cortex-a72|arm64
arm_cortex-a7_neon-vfpv4|armv7
arm_cortex-a9|armv7
arm_cortex-a9_vfpv3-d16|armv7
arm_cortex-a15_neon-vfpv4|armv7
mipsel_24kc|mipsle
mipsel_74kc|mipsle
mips_24kc|mips"

# Entware architecture (Keenetic) | binary arch
KEENETIC="mipsel-3.4|mipsle
mips-3.4|mips
aarch64-3.10|arm64
armv7sf-3.2|armv7
armv5sf-3.2|armv5
x64-3.2|amd64"

command -v tar >/dev/null 2>&1 || { echo "tar is required" >&2; exit 1; }
TAR="tar --numeric-owner --owner=0 --group=0 --sort=name"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
SIZES="$DIST/sizes.md"
{
    echo "| Package | Binary | Package (.ipk) | Installed |"
    echo "|---|---|---|---|"
} > "$SIZES"

kib() { echo "$(( ($1 + 1023) / 1024 )) KiB"; }
size_of() { wc -c < "$1" | tr -d ' '; }

# build_ipk OS ARCH BINARY-ARCH DEPENDS CONFLICTS
build_ipk() {
    os="$1"; arch="$2"; barch="$3"; depends="$4"; conflicts="$5"
    bin="$DIST/hydravpn-router-$VERSION-$os-$barch"
    [ -f "$bin" ] || { echo "skip $os $arch: $bin not built" >&2; return 0; }

    pkg="$WORK/$os-$arch"
    rm -rf "$pkg"
    mkdir -p "$pkg/data" "$pkg/control"
    cp -R "$ROOT/build/$os/files/." "$pkg/data/"
    cp "$ROOT/build/$os/control/postinst" "$ROOT/build/$os/control/prerm" "$pkg/control/"
    case "$os" in
        openwrt)
            install -D -m 0755 "$bin" "$pkg/data/usr/bin/hydravpn-router"
            install -D -m 0600 "$ROOT/configs/config.yaml" "$pkg/data/etc/hydravpn-router/config.yaml"
            conf="/etc/hydravpn-router/config.yaml"
            initd="$pkg/data/etc/init.d/hydravpn-router"
            ;;
        keeneticos)
            install -D -m 0755 "$bin" "$pkg/data/opt/bin/hydravpn-router"
            chmod 0600 "$pkg/data/opt/etc/hydravpn-router/config.yaml"
            conf="/opt/etc/hydravpn-router/config.yaml"
            initd="$pkg/data/opt/etc/init.d/S99hydravpn-router"
            chmod 0755 "$pkg/data/opt/etc/ndm/netfilter.d/50-hydravpn-router.sh"
            ;;
    esac
    chmod 0755 "$initd" "$pkg/control/postinst" "$pkg/control/prerm"

    # Installed size in KiB (the Installed-Size field of opkg).
    bytes=$(find "$pkg/data" -type f -exec cat {} + | wc -c | tr -d ' ')
    {
        echo "Package: hydravpn-router"
        echo "Version: $VERSION"
        echo "Depends: $depends"
        [ -n "$conflicts" ] && echo "Conflicts: $conflicts"
        echo "License: GPL-3.0-or-later"
        echo "Section: net"
        echo "URL: https://github.com/Chistovik92/HydraVPNforRouters"
        echo "Maintainer: Chistovik92 <chistovik92@users.noreply.github.com>"
        echo "Architecture: $arch"
        echo "Installed-Size: $(( (bytes + 1023) / 1024 ))"
        echo "Description: HydraVPN for Router - DPI bypass with sing-box, zapret and ByeDPI ($os)"
    } > "$pkg/control/control"
    echo "$conf" > "$pkg/control/conffiles"

    (cd "$pkg/data" && $TAR -czf ../data.tar.gz .)
    (cd "$pkg/control" && $TAR -czf ../control.tar.gz .)
    echo "2.0" > "$pkg/debian-binary"
    out="$DIST/hydravpn-router-${os}_${VERSION}_${arch}.ipk"
    (cd "$pkg" && $TAR -czf "$out" ./debian-binary ./data.tar.gz ./control.tar.gz)

    echo "| \`hydravpn-router-${os}_${VERSION}_${arch}.ipk\` | $(kib "$(size_of "$bin")") | $(kib "$(size_of "$out")") | $(kib "$bytes") |" >> "$SIZES"
    (cd "$DIST" && sha256sum "$(basename "$out")" >> checksums.txt)
    echo "  -> $out"
}

echo "$OPENWRT" | while IFS='|' read -r arch barch; do
    build_ipk openwrt "$arch" "$barch" \
        "libc, ca-bundle, nftables, kmod-nft-tproxy, ip-full, sing-box" \
        "https-dns-proxy, nextdns, luci-app-passwall, luci-app-passwall2"
done
echo "$KEENETIC" | while IFS='|' read -r arch barch; do
    build_ipk keeneticos "$arch" "$barch" "libc, ca-certificates, sing-box-go" ""
done

echo "Sizes: $SIZES"
