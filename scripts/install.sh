#!/bin/sh
# HydraVPN for Router - install / update script
#
# Downloads the release binary for this router from GitHub Releases,
# installs the service (procd on OpenWrt, rc.func on Keenetic Entware,
# systemd on Linux) and keeps the existing configuration.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
#   sh install.sh --yes --version 1.2.2
#
# MikroTik RouterOS has no POSIX shell: use the container instructions in
# INSTALL.md instead.

set -eu

REPO_OWNER="Chistovik92"
REPO_NAME="HydraVPNforRouters"
GITHUB_API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"
RELEASE_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download"
IMAGE="ghcr.io/chistovik92/hydravpn-router"

if [ -t 1 ]; then
    RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; NC=''
fi

log_info() { printf "${BLUE}[INFO]${NC} %s\n" "$*"; }
log_ok()   { printf "${GREEN}[OK]${NC} %s\n" "$*"; }
log_warn() { printf "${YELLOW}[WARN]${NC} %s\n" "$*"; }
log_err()  { printf "${RED}[ERR]${NC} %s\n" "$*" >&2; }
die()      { log_err "$*"; exit 1; }

# ---------------------------------------------------------------- helpers

# fetch URL FILE - download with curl, wget or uclient-fetch
fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 2 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    elif command -v uclient-fetch >/dev/null 2>&1; then
        uclient-fetch -q -O "$2" "$1"
    else
        die "curl or wget is required"
    fi
}

# fetch_stdout URL
fetch_stdout() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O - "$1"
    else
        uclient-fetch -q -O - "$1"
    fi
}

# ask QUESTION - yes/no prompt that also works with "curl | sh"
ask() {
    [ "$AUTO_YES" = "1" ] && return 0
    # Subshell: a failed redirection on a special builtin would exit dash.
    if ! (: </dev/tty) 2>/dev/null; then
        die "No terminal to ask for confirmation. Re-run with --yes."
    fi
    printf "%s [y/N]: " "$1" >/dev/tty
    read -r REPLY </dev/tty || REPLY=""
    case "$REPLY" in
        [Yy]*) return 0 ;;
        *) return 1 ;;
    esac
}

# strip_v VERSION - "v1.2.2" -> "1.0.5"
strip_v() { echo "${1#v}"; }

# ident_gt A B - true when the dot-separated pre-release identifiers A > B
# (numbers compare as numbers, anything else as text; a longer list wins a tie)
ident_gt() {
    a="$1"; b="$2"
    while [ -n "$a" ] || [ -n "$b" ]; do
        [ -z "$a" ] && return 1
        [ -z "$b" ] && return 0
        x="${a%%.*}"; y="${b%%.*}"
        if [ "$x" != "$y" ]; then
            case "$x$y" in
                *[!0-9]*) [ "$(printf '%s\n%s\n' "$x" "$y" | LC_ALL=C sort | head -n 1)" = "$y" ] && return 0
                          return 1 ;;
                *) [ "$x" -gt "$y" ] && return 0
                   return 1 ;;
            esac
        fi
        case "$a" in *.*) a="${a#*.}" ;; *) a="" ;; esac
        case "$b" in *.*) b="${b#*.}" ;; *) b="" ;; esac
    done
    return 1
}

# version_gt A B - true when A > B. x.y.z is compared numerically; a
# pre-release suffix ("1.3.0-debug.2") sorts before its release ("1.3.0").
version_gt() {
    [ "$1" = "$2" ] && return 1
    a="${1%%-*}"; b="${2%%-*}"
    pa=""; pb=""
    case "$1" in *-*) pa="${1#*-}" ;; esac
    case "$2" in *-*) pb="${2#*-}" ;; esac
    while [ -n "$a" ] || [ -n "$b" ]; do
        x="${a%%.*}"; y="${b%%.*}"
        x="${x:-0}"; y="${y:-0}"
        [ "$x" -gt "$y" ] 2>/dev/null && return 0
        [ "$x" -lt "$y" ] 2>/dev/null && return 1
        case "$a" in *.*) a="${a#*.}" ;; *) a="" ;; esac
        case "$b" in *.*) b="${b#*.}" ;; *) b="" ;; esac
    done
    [ -z "$pa" ] && [ -n "$pb" ] && return 0
    [ -n "$pa" ] && [ -z "$pb" ] && return 1
    ident_gt "$pa" "$pb"
}

# ---------------------------------------------------------------- detection

# is_little_endian - reads the ELF data byte of /bin/sh (1 = LE, 2 = BE)
is_little_endian() {
    byte=$(dd if=/bin/sh bs=1 skip=5 count=1 2>/dev/null | od -b | awk 'NR==1 {print $2}')
    [ "$byte" = "001" ]
}

# go_arch - maps the machine to the Go release suffix
go_arch() {
    if [ "$PLATFORM" = "openwrt" ] && [ -n "${DISTRIB_ARCH:-}" ]; then
        case "$DISTRIB_ARCH" in
            x86_64*) echo amd64; return ;;
            i386*|i486*|i686*) echo 386; return ;;
            aarch64*) echo arm64; return ;;
            arm_arm1176*|arm_arm926*|arm_fa526*|arm_xscale*) echo armv6; return ;;
            arm*) echo armv7; return ;;
            mipsel*) echo mipsle; return ;;
            mips64el*) echo mips64le; return ;;
            mips64*) echo mips64; return ;;
            mips*) echo mips; return ;;
        esac
    fi

    m=$(uname -m)
    case "$m" in
        x86_64|amd64) echo amd64 ;;
        i386|i486|i586|i686) echo 386 ;;
        aarch64|arm64) echo arm64 ;;
        armv7*|armv8l) echo armv7 ;;
        armv6*|armv5*) echo armv6 ;;
        mips64*) if is_little_endian; then echo mips64le; else echo mips64; fi ;;
        # uname reports "mips" for both endiannesses (e.g. MT7621 is little-endian)
        mips*) if is_little_endian; then echo mipsle; else echo mips; fi ;;
        *) die "Unsupported architecture: $m" ;;
    esac
}

detect_platform() {
    if [ "$METHOD" = "docker" ]; then
        PLATFORM="docker"
    elif [ -f /etc/openwrt_release ]; then
        # shellcheck disable=SC1091
        . /etc/openwrt_release
        PLATFORM="openwrt"
    elif [ -x /opt/bin/opkg ] && [ -d /opt/etc ]; then
        PLATFORM="keenetic-entware"
    elif command -v ndmc >/dev/null 2>&1 || [ -d /etc/ndm ]; then
        # KeeneticOS: the system partition is read-only, everything lives in
        # the OPKG storage (USB drive or the built-in memory) mounted at /opt.
        die "KeeneticOS detected, but Entware (OPKG) is not installed. Enable the OPKG component and choose a storage (USB drive or built-in memory) in the router web UI, install Entware, then run this script again. See INSTALL.md."
    elif [ "$(uname -s)" = "Linux" ]; then
        PLATFORM="linux"
    else
        die "Unsupported system: $(uname -s). For MikroTik see INSTALL.md."
    fi

    case "$PLATFORM" in
        openwrt)
            BIN="/usr/bin/hydravpn-router"
            CONFIG_DIR="/etc/hydravpn-router"
            RUNTIME_DIR="/var/run/hydravpn-router"
            SERVICE="/etc/init.d/hydravpn-router"
            LAN_IF="br-lan"
            ;;
        keenetic-entware)
            BIN="/opt/bin/hydravpn-router"
            CONFIG_DIR="/opt/etc/hydravpn-router"
            RUNTIME_DIR="/opt/var/run/hydravpn-router"
            SERVICE="/opt/etc/init.d/S99hydravpn-router"
            LAN_IF="br0"
            ;;
        linux)
            BIN="/usr/local/bin/hydravpn-router"
            CONFIG_DIR="/etc/hydravpn-router"
            RUNTIME_DIR="/var/run/hydravpn-router"
            SERVICE="systemd"
            LAN_IF="eth0"
            ;;
        docker)
            BIN=""; CONFIG_DIR="/etc/hydravpn-router"; RUNTIME_DIR=""; SERVICE=""; LAN_IF="eth0"
            ;;
    esac

    if [ "$PLATFORM" != "docker" ]; then
        ARCH=$(go_arch)
    else
        ARCH=""
    fi
    log_info "Platform: $PLATFORM${ARCH:+ (arch: $ARCH)}"
}

get_latest_version() {
    if [ -n "$WANT_VERSION" ]; then
        LATEST_VERSION=$(strip_v "$WANT_VERSION")
        return 0
    fi
    log_info "Fetching latest release from GitHub..."
    tag=$(fetch_stdout "${GITHUB_API}/releases/latest" 2>/dev/null \
        | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1) || true
    [ -n "$tag" ] || die "Failed to fetch the latest release (GitHub API unreachable or rate-limited). Use --version X.Y.Z."
    LATEST_VERSION=$(strip_v "$tag")
    log_ok "Latest version: $LATEST_VERSION"
}

get_installed_version() {
    INSTALLED_VERSION=""
    if [ -n "${BIN:-}" ] && [ -x "$BIN" ]; then
        INSTALLED_VERSION=$("$BIN" version 2>/dev/null | grep -o '[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\(-[0-9A-Za-z.]*\)\{0,1\}' | head -n 1) || true
    fi
    if [ -n "$INSTALLED_VERSION" ]; then
        log_info "Installed version: $INSTALLED_VERSION"
    fi
}

# ---------------------------------------------------------------- install

download_binary() {
    asset="hydravpn-router-${LATEST_VERSION}-linux-${ARCH}"
    url="${RELEASE_BASE}/v${LATEST_VERSION}/${asset}"
    TMP_BIN="${BIN}.new"

    log_info "Downloading $url"
    mkdir -p "$(dirname "$BIN")"
    fetch "$url" "$TMP_BIN" || { rm -f "$TMP_BIN"; die "Download failed: $url"; }

    if command -v sha256sum >/dev/null 2>&1; then
        sums=$(fetch_stdout "${RELEASE_BASE}/v${LATEST_VERSION}/checksums.txt" 2>/dev/null) || sums=""
        # Names may carry sha256sum's binary-mode "*" prefix.
        expected=$(printf '%s\n' "$sums" | awk -v f="$asset" '{n = $2; sub(/^\*/, "", n)} n == f {print $1}')
        if [ -n "$expected" ]; then
            actual=$(sha256sum "$TMP_BIN" | awk '{print $1}')
            if [ "$expected" != "$actual" ]; then
                rm -f "$TMP_BIN"
                die "Checksum mismatch for $asset"
            fi
            log_ok "Checksum verified"
        else
            log_warn "No checksum published for $asset, skipping verification"
        fi
    fi

    chmod 0755 "$TMP_BIN"
    "$TMP_BIN" version >/dev/null 2>&1 || { rm -f "$TMP_BIN"; die "Downloaded binary does not run on this CPU (arch $ARCH)"; }
}

write_default_config() {
    [ -f "$CONFIG_DIR/config.yaml" ] && return 0
    mkdir -p "$CONFIG_DIR"
    cat > "$CONFIG_DIR/config.yaml" <<EOF
# HydraVPN for Router configuration. Unset values use built-in defaults,
# see https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/configs/config.yaml
settings:
  dns_server:
    - "77.88.8.8"
    - "77.88.8.1"
  bootstrap_dns_server:
    - "77.88.8.8"
    - "77.88.8.1"
  source_network_interfaces:
    - "${LAN_IF}"
  config_path: "${CONFIG_DIR}/sing-box/config.json"
  cache_path: "${RUNTIME_DIR:-/tmp/hydravpn-router}/cache.db"
  log_level: "warn"

sections: []
subscription_urls: []
EOF
    chmod 0600 "$CONFIG_DIR/config.yaml"
    log_ok "Default config created: $CONFIG_DIR/config.yaml"
}

install_service() {
    case "$PLATFORM" in
        openwrt)
            cat > "$SERVICE" <<'EOF'
#!/bin/sh /etc/rc.common
# HydraVPN for Router - procd init script

START=99
STOP=10
USE_PROCD=1

PROG=/usr/bin/hydravpn-router
CONFIG_FILE=/etc/hydravpn-router/config.yaml

start_service() {
    procd_open_instance
    procd_set_param command $PROG start -c $CONFIG_FILE
    procd_set_param respawn 3600 5 5
    procd_set_param term_timeout 20
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param file $CONFIG_FILE
    procd_close_instance
}

reload_service() {
    $PROG reload -c $CONFIG_FILE
}
EOF
            chmod 0755 "$SERVICE"
            "$SERVICE" enable
            ;;
        keenetic-entware)
            cat > "$SERVICE" <<'EOF'
#!/bin/sh
ENABLED=yes
PROCS=hydravpn-router
ARGS="start -c /opt/etc/hydravpn-router/config.yaml --runtime-dir /opt/var/run/hydravpn-router"
PREARGS=""
DESC="HydraVPN for Router"
PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin

. /opt/etc/init.d/rc.func
EOF
            chmod 0755 "$SERVICE"
            install_ndm_hook
            ;;
        linux)
            if command -v systemctl >/dev/null 2>&1; then
                cat > /etc/systemd/system/hydravpn-router.service <<EOF
[Unit]
Description=HydraVPN for Router
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN} start -c ${CONFIG_DIR}/config.yaml --runtime-dir ${RUNTIME_DIR}
ExecReload=${BIN} reload -c ${CONFIG_DIR}/config.yaml --runtime-dir ${RUNTIME_DIR}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF
                systemctl daemon-reload
                systemctl enable hydravpn-router >/dev/null
            else
                log_warn "systemd not found: start manually with '$BIN start -c $CONFIG_DIR/config.yaml'"
            fi
            ;;
    esac
}

# install_ndm_hook - KeeneticOS rebuilds its firewall on network events and
# runs every script in /opt/etc/ndm/netfilter.d/ afterwards; the hook asks the
# service to put its rules back (an unchanged config only re-applies rules).
install_ndm_hook() {
    hook_dir=/opt/etc/ndm/netfilter.d
    mkdir -p "$hook_dir"
    cat > "$hook_dir/50-hydravpn-router.sh" <<'EOF'
#!/bin/sh
# Installed by HydraVPN for Router. NDM passes $type (iptables|ip6tables) and $table.
[ "$type" = "ip6tables" ] && exit 0
[ "$table" = "mangle" ] || exit 0
[ -x /opt/bin/hydravpn-router ] || exit 0

STAMP=/tmp/hydravpn-router.ndm
now=$(date +%s)
last=$(cat "$STAMP" 2>/dev/null || echo 0)
[ $((now - last)) -lt 3 ] && exit 0
echo "$now" > "$STAMP"

/opt/bin/hydravpn-router reload -c /opt/etc/hydravpn-router/config.yaml --runtime-dir /opt/var/run/hydravpn-router >/dev/null 2>&1 &
exit 0
EOF
    chmod 0755 "$hook_dir/50-hydravpn-router.sh"
    log_ok "NDM netfilter hook installed: $hook_dir/50-hydravpn-router.sh"
}

service_ctl() {
    case "$PLATFORM" in
        openwrt|keenetic-entware) [ -x "$SERVICE" ] && "$SERVICE" "$1" || true ;;
        linux) command -v systemctl >/dev/null 2>&1 && systemctl "$1" hydravpn-router || true ;;
    esac
}

# pkg_installed NAME - OpenWrt package check for opkg and apk
pkg_installed() {
    if command -v apk >/dev/null 2>&1; then
        apk info -e "$1" >/dev/null 2>&1
    else
        opkg list-installed "$1" 2>/dev/null | grep -q "^$1 "
    fi
}

install_dependencies() {
    case "$PLATFORM" in
        openwrt)
            missing=""
            for pkg in sing-box nftables kmod-nft-tproxy ip-full; do
                pkg_installed "$pkg" || missing="$missing $pkg"
            done
            [ -z "$missing" ] && return 0
            log_info "Installing dependencies:$missing"
            if command -v apk >/dev/null 2>&1; then
                # shellcheck disable=SC2086
                apk update && apk add $missing || log_warn "Could not install:$missing"
            else
                # shellcheck disable=SC2086
                opkg update && opkg install $missing || log_warn "Could not install:$missing"
            fi
            ;;
        keenetic-entware)
            command -v sing-box >/dev/null 2>&1 || \
                { opkg update && opkg install sing-box-go; } || \
                log_warn "sing-box is not installed; install it from Entware before starting the service"
            ;;
        linux)
            command -v sing-box >/dev/null 2>&1 || \
                log_warn "sing-box is not installed: see https://sing-box.sagernet.org/installation/package-manager/"
            ;;
    esac
}

do_install_binary() {
    download_binary
    was_running=0
    if [ -x "$BIN" ]; then
        service_ctl stop
        was_running=1
    fi
    mv -f "$TMP_BIN" "$BIN"
    log_ok "Installed $BIN ($LATEST_VERSION)"

    install_dependencies
    write_default_config
    install_service

    if [ "$was_running" = "1" ] || [ "$UPDATE" = "0" ]; then
        service_ctl start
    fi
}

do_install_docker() {
    command -v docker >/dev/null 2>&1 || die "docker is not installed"
    docker pull "${IMAGE}:${LATEST_VERSION}"
    log_ok "Image pulled: ${IMAGE}:${LATEST_VERSION}"
    log_info "Run it with:"
    log_info "  docker run -d --name hydravpn-router --restart unless-stopped \\"
    log_info "    --network host --cap-add NET_ADMIN --cap-add NET_RAW \\"
    log_info "    -v /etc/hydravpn-router:/etc/hydravpn-router \\"
    log_info "    ${IMAGE}:${LATEST_VERSION}"
}

# ---------------------------------------------------------------- main

show_help() {
    cat <<EOF
HydraVPN for Router - install / update script

Usage: install.sh [OPTIONS]

Options:
  -y, --yes            Do not ask for confirmation
  -v, --version X.Y.Z  Install a specific version instead of the latest
  -m, --method METHOD  binary (default) or docker
  -u, --update         Update an existing installation only
  -c, --check-only     Only report whether an update is available
  -f, --force          Reinstall even if the version is current
  -h, --help           Show this help

Supported: OpenWrt, Keenetic (Entware), generic Linux (systemd), Docker.
MikroTik RouterOS: see INSTALL.md (container).
EOF
}

main() {
    AUTO_YES=0; WANT_VERSION=""; METHOD="binary"; UPDATE=0; CHECK_ONLY=0; FORCE=0

    while [ $# -gt 0 ]; do
        case "$1" in
            -y|--yes) AUTO_YES=1 ;;
            -v|--version) [ $# -ge 2 ] || die "$1 needs a value"; WANT_VERSION="$2"; shift ;;
            -m|--method) [ $# -ge 2 ] || die "$1 needs a value"; METHOD="$2"; shift ;;
            -u|--update) UPDATE=1 ;;
            -c|--check-only) CHECK_ONLY=1 ;;
            -f|--force) FORCE=1 ;;
            -h|--help) show_help; exit 0 ;;
            *) log_err "Unknown option: $1"; show_help; exit 1 ;;
        esac
        shift
    done
    case "$METHOD" in binary|docker) ;; *) die "Unknown method: $METHOD" ;; esac

    printf "\n${BLUE}=== HydraVPN for Router ===${NC}\n\n"

    detect_platform
    get_latest_version
    get_installed_version

    if [ "$PLATFORM" = "docker" ]; then
        [ "$CHECK_ONLY" = "1" ] && { log_info "Latest image: ${IMAGE}:${LATEST_VERSION}"; exit 0; }
        ask "Pull ${IMAGE}:${LATEST_VERSION}?" || exit 0
        do_install_docker
        exit 0
    fi

    if [ "$UPDATE" = "1" ] && [ -z "$INSTALLED_VERSION" ]; then
        die "HydraVPN for Router is not installed. Run install.sh without --update."
    fi

    if [ -n "$INSTALLED_VERSION" ] && [ "$FORCE" = "0" ] && ! version_gt "$LATEST_VERSION" "$INSTALLED_VERSION"; then
        log_ok "Already up to date ($INSTALLED_VERSION)"
        exit 0
    fi

    if [ "$CHECK_ONLY" = "1" ]; then
        if [ -n "$INSTALLED_VERSION" ]; then
            log_warn "Update available: $INSTALLED_VERSION -> $LATEST_VERSION"
        else
            log_info "Not installed; latest version is $LATEST_VERSION"
        fi
        exit 0
    fi

    if [ -n "$INSTALLED_VERSION" ]; then
        ask "Update HydraVPN for Router $INSTALLED_VERSION -> $LATEST_VERSION?" || { log_info "Cancelled"; exit 0; }
    else
        ask "Install HydraVPN for Router $LATEST_VERSION?" || { log_info "Cancelled"; exit 0; }
    fi

    do_install_binary

    get_installed_version
    if [ "$INSTALLED_VERSION" = "$LATEST_VERSION" ]; then
        log_ok "HydraVPN for Router $INSTALLED_VERSION is installed"
    else
        log_warn "Installed version '$INSTALLED_VERSION' differs from expected $LATEST_VERSION"
    fi

    printf "\n${GREEN}=== Next steps ===${NC}\n"
    printf "1. Edit the config:   %s/config.yaml (add subscription_urls and sections)\n" "$CONFIG_DIR"
    printf "2. Apply changes:     %s reload\n" "$BIN"
    printf "3. Status:            %s status\n" "$BIN"
    printf "4. Diagnostics:       %s check all\n" "$BIN"
    printf "5. Router self-test:  %s selftest   (nft, sing-box, kernel tproxy)\n" "$BIN"
    printf "\nDocs: https://github.com/%s/%s/blob/main/INSTALL.md\n" "$REPO_OWNER" "$REPO_NAME"
}

main "$@"
