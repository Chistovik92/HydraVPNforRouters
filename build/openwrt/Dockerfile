# OpenWRT SDK build environment for HydraVPN for Router
FROM ubuntu:24.04 AS builder

ARG VERSION=1.0.0
ARG OPENWRT_VERSION=24.10.6
ARG TARGET=x86/64

ENV DEBIAN_FRONTEND=noninteractive

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    clang \
    cmake \
    curl \
    file \
    g++ \
    gcc \
    git \
    libncurses5-dev \
    libssl-dev \
    make \
    patch \
    perl \
    python3 \
    python3-distutils \
    rsync \
    unzip \
    wget \
    zstd \
    && rm -rf /var/lib/apt/lists/*

# Download and extract OpenWRT SDK
WORKDIR /opt
RUN SDK_URL="https://downloads.openwrt.org/releases/${OPENWRT_VERSION}/targets/${TARGET}/openwrt-sdk-${OPENWRT_VERSION}-${TARGET//\//-}_gcc-13.3.0_musl.Linux-x86_64.tar.zst" && \
    wget -q "$SDK_URL" -O sdk.tar.zst && \
    tar --zstd -xf sdk.tar.zst && \
    mv openwrt-sdk-* openwrt-sdk && \
    rm sdk.tar.zst

WORKDIR /opt/openwrt-sdk

# Update feeds
RUN ./scripts/feeds update -a && ./scripts/feeds install -a

# Copy HydraVPN for Router source
COPY . /opt/hydravpn-router

# Build hydravpn-router binary for target
WORKDIR /opt/hydravpn-router
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -tags "netgo,osusergo" \
    -ldflags "-s -w -X github.com/Chistovik92/hydravpn-router/pkg/version.Version=${VERSION}" \
    -o /opt/openwrt-sdk/hydravpn-router \
    ./cmd/hydravpn-router

# Create package structure
RUN mkdir -p /opt/package/{etc/init.d,etc/config,usr/bin,usr/lib/hydravpn-router,www/luci-static/resources/view/hydravpn-router}

# Copy files to package
RUN cp /opt/openwrt-sdk/hydravpn-router /opt/package/usr/bin/hydravpn-router && \
    chmod +x /opt/package/usr/bin/hydravpn-router

# Copy init script
RUN cat > /opt/package/etc/init.d/hydravpn-router <<'INITEOF'
#!/bin/sh /etc/rc.common

START=99
USE_PROCD=1
EXTRA_COMMANDS="reload status"
EXTRA_HELP="        reload  Reload configuration\n        status  Show service status"

NAME=hydravpn-router
PROG=/usr/bin/hydravpn-router
CONFIG_FILE=/etc/hydravpn-router/config.yaml

start_service() {
    procd_open_instance
    procd_set_param command $PROG start -c $CONFIG_FILE
    procd_set_param respawn 3600 5 0
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param user root
    procd_close_instance
}

stop_service() {
    $PROG stop
}

reload_service() {
    $PROG reload -c $CONFIG_FILE
}

status_service() {
    $PROG status
}
INITEOF
    chmod +x /opt/package/etc/init.d/hydravpn-router

# Copy UCI config
RUN cat > /opt/package/etc/config/hydravpn-router <<'UCIEOF'
config hydravpn-router 'main'
    option enabled '1'
    option config_version '1.0.0'
    option config_file '/etc/hydravpn-router/config.yaml'
    option log_level 'warn'
    option log_file '/var/log/hydravpn-router.log'

config section 'subscription'
    option label 'Subscription'
    option enabled '1'
    option action 'connection'

config settings 'dns'
    option dns_type 'udp'
    list dns_server '77.88.8.8'
    list dns_server '77.88.8.1'
    option dns_strategy 'prefer_ipv4'
    option dns_check_interval '10s'
    option dns_recovery_check_interval '60s'

config settings 'firewall'
    option enabled '1'
    list source_interface 'br-lan'
    option mark_value '0x08000000'
    option table_name 'podkop'
    option chain_name 'podkop-chain'

config settings 'singbox'
    option enabled '1'
    option binary '/usr/bin/sing-box'
    option config_dir '/etc/hydravpn-router/sing-box'
    option runtime_dir '/var/run/hydravpn-router/sing-box'

config settings 'zapret'
    option enabled '0'
    option type 'zapret2'
    option binary '/opt/zapret2/nfq2/nfqws2'
    option strategy_file '/etc/hydravpn-router/zapret/strategy.json'
    option queue_num '4000'
    option mark '0x01000000'

config settings 'byedpi'
    option enabled '0'
    option binary '/usr/bin/ciadpi'
    option listen_address '127.0.0.1'
    option port_base '1080'
    option options '-o 2 --auto=t,r,a,s -d 2'

config settings 'subscription'
    option enabled '1'
    option update_interval '1h'
    option cache_dir '/etc/hydravpn-router/subscription-cache'

config settings 'ui'
    option enabled '1'
    option luci_enabled '1'
    option port '8080'
    option bind_address '0.0.0.0'
UCIEOF

# Copy default config
RUN mkdir -p /opt/package/etc/hydravpn-router && \
    cat > /opt/package/etc/hydravpn-router/config.yaml <<'CONFIGEOF'
settings:
  config_version: "1.0.0"
  dns_type: "udp"
  dns_server:
    - "77.88.8.8"
    - "77.88.8.1"
  bootstrap_dns_server:
    - "77.88.8.8"
    - "77.88.8.1"
  dns_check_interval: "10s"
  dns_recovery_check_interval: "60s"
  dns_check_timeout: "2s"
  dns_rewrite_ttl: 60
  dns_strategy: "prefer_ipv4"
  dns_detour_enabled: false
  source_network_interfaces:
    - "br-lan"
  enable_output_network_interface: false
  enable_badwan_interface_monitoring: false
  enable_yacd: false
  disable_quic: false
  list_update_enabled: true
  update_interval: "24h"
  component_update_check_enabled: true
  component_update_check_interval: "24h"
  latency_test_url: "https://www.gstatic.com/generate_204"
  download_lists_via_proxy: false
  download_components_via_proxy: false
  dont_touch_dhcp: false
  config_path: "/etc/hydravpn-router/sing-box/config.json"
  cache_path: "/tmp/hydravpn-router/cache.db"
  log_level: "warn"
  exclude_ntp: false
  shutdown_correctly: false

sections: []
interfaces: []
subscription_urls: []
urltests: []
servers: []
rules: []
rule_sets: []
community_lists: []
CONFIGEOF

# Build IPK package
RUN mkdir -p /opt/package/CONTROL && \
    cat > /opt/package/CONTROL/control <<CONTROLEOF
Package: hydravpn-router
Version: ${VERSION}
Depends: libc, ca-bundle, kmod-inet-diag, kmod-netlink-diag, kmod-tun, curl, ucode, ucode-mod-fs, ucode-mod-uci, kmod-nft-tproxy, coreutils-base64, bind-dig, nftables, kmod-nft-nat, ip-full, sing-box
Conflicts: https-dns-proxy, nextdns, luci-app-passwall, luci-app-passwall2
License: GPL-3.0-or-later
Section: net
URL: https://github.com/Chistovik92/hydravpn-router
Maintainer: Chistovik92 <chistovik92@users.noreply.github.com>
Architecture: all
Installed-Size: 5000
Description: HydraVPN for Router - Multi-platform DPI bypass solution with sing-box, zapret, and ByeDPI support
CONTROLEOF

RUN cat > /opt/package/CONTROL/conffiles <<CONFFILESEOF
/etc/config/hydravpn-router
/etc/hydravpn-router/config.yaml
CONFFILESEOF

RUN cat > /opt/package/CONTROL/postinst <<'POSTINSTEOF'
#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/hydravpn-router enable
/etc/init.d/hydravpn-router start
POSTINSTEOF
    chmod +x /opt/package/CONTROL/postinst

RUN cat > /opt/package/CONTROL/prerm <<'PRERMEOF'
#!/bin/sh
/etc/init.d/hydravpn-router stop
/etc/init.d/hydravpn-router disable
PRERMEOF
    chmod +x /opt/package/CONTROL/prerm

# Build the IPK
RUN cd /opt && \
    ../openwrt-sdk/scripts/ipkg-build -o root -g root package /output

# Build APK package (for OpenWRT 24.10+)
RUN mkdir -p /opt/apk-package && \
    cp -r /opt/package/* /opt/apk-package/ && \
    rm -rf /opt/apk-package/CONTROL && \
    mkdir -p /opt/apk-package/lib/apk/packages && \
    cd /opt/apk-package && \
    find . -type f ! -path './lib/apk/packages/*' | LC_ALL=C sort | sed 's#^\./#/#' > lib/apk/packages/hydravpn-router.list && \
    echo "/etc/config/hydravpn-router" > lib/apk/packages/hydravpn-router.conffiles && \
    sha256sum etc/config/hydravpn-router | awk '{print $1}' > /tmp/hash && \
    echo "/etc/config/hydravpn-router $(cat /tmp/hash)" > lib/apk/packages/hydravpn-router.conffiles_static && \
    /opt/openwrt-sdk/staging_dir/host/bin/apk build \
        --files /opt/apk-package \
        --output /output/podkop-plus_${VERSION}.apk \
        -I "name:hydravpn-router" \
        -I "version:${VERSION}" \
        -I "description:HydraVPN for Router - Multi-platform DPI bypass solution" \
        -I "arch:noarch" \
        -I "license:GPL-3.0-or-later" \
        -I "origin:hydravpn-router" \
        -I "maintainer:Chistovik92 <chistovik92@users.noreply.github.com>" \
        -I "url:https://github.com/Chistovik92/hydravpn-router" \
        -I "depends:libc ca-bundle curl ip-full kmod-inet-diag kmod-netlink-diag kmod-nft-nat kmod-nft-tproxy kmod-tun nftables sing-box ucode ucode-mod-fs ucode-mod-uci !https-dns-proxy !nextdns !luci-app-passwall !luci-app-passwall2" \
        -s "pre-install:/opt/package/CONTROL/prerm" \
        -s "post-install:/opt/package/CONTROL/postinst"

# Output stage
FROM scratch AS output
COPY --from=builder /output /output


