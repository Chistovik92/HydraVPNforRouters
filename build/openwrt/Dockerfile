# Builds an OpenWrt .ipk for one architecture without the OpenWrt SDK.
#
#   docker build -f build/openwrt/Dockerfile \
#       --build-arg GOARCH=mipsle --build-arg PKG_ARCH=mipsel_24kc \
#       --output type=local,dest=dist/openwrt .
#
# PKG_ARCH must be the OpenWrt package architecture of the router
# (opkg print-architecture), GOARCH/GOARM/GOMIPS the matching Go target.
# VERSION defaults to pkg/version/version.go.

FROM golang:alpine AS build
ARG VERSION=
ARG GOARCH=amd64
ARG GOARM=
ARG GOMIPS=softfloat
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN set -eu; \
    V="${VERSION:-$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' pkg/version/version.go)}"; \
    V="${V#v}"; \
    echo "$V" > /version; \
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" GOARM="$GOARM" GOMIPS="$GOMIPS" \
    go build -trimpath -buildvcs=false -tags netgo,osusergo \
        -ldflags "-s -w -X github.com/Chistovik92/hydravpn-router/pkg/version.Version=$V" \
        -o /hydravpn-router ./cmd/hydravpn-router

FROM alpine:3.22 AS package
ARG PKG_ARCH=x86_64
RUN apk add --no-cache tar
COPY --from=build /hydravpn-router /version /tmp/
COPY build/openwrt/files/ /pkg/data/
COPY build/openwrt/control/ /pkg/control/
COPY configs/config.yaml /pkg/data/etc/hydravpn-router/config.yaml
RUN set -eu; \
    V="$(cat /tmp/version)"; \
    install -D -m 0755 /tmp/hydravpn-router /pkg/data/usr/bin/hydravpn-router; \
    chmod 0755 /pkg/data/etc/init.d/hydravpn-router /pkg/control/postinst /pkg/control/prerm; \
    SIZE="$(du -sk /pkg/data | cut -f1)"; \
    printf '%s\n' \
        "Package: hydravpn-router" \
        "Version: $V" \
        "Depends: libc, ca-bundle, nftables, kmod-nft-tproxy, ip-full, sing-box" \
        "Conflicts: https-dns-proxy, nextdns, luci-app-passwall, luci-app-passwall2" \
        "License: GPL-3.0-or-later" \
        "Section: net" \
        "URL: https://github.com/Chistovik92/HydraVPNforRouters" \
        "Maintainer: Chistovik92 <chistovik92@users.noreply.github.com>" \
        "Architecture: $PKG_ARCH" \
        "Installed-Size: $SIZE" \
        "Description: HydraVPN for Router - DPI bypass with sing-box, zapret and ByeDPI" \
        > /pkg/control/control; \
    echo "/etc/hydravpn-router/config.yaml" > /pkg/control/conffiles; \
    TAR="tar --numeric-owner --owner=0 --group=0 --sort=name -czf"; \
    (cd /pkg/data && $TAR ../data.tar.gz .); \
    (cd /pkg/control && $TAR ../control.tar.gz .); \
    echo "2.0" > /pkg/debian-binary; \
    mkdir -p /output; \
    (cd /pkg && $TAR "/output/hydravpn-router_${V}_${PKG_ARCH}.ipk" ./debian-binary ./data.tar.gz ./control.tar.gz)

FROM scratch AS output
COPY --from=package /output/ /
