#!/bin/sh
# HydraVPN for Router - Universal Auto-Install Script
# Detects platform, fetches latest version, and installs automatically
# Works on: OpenWRT, KeeneticOS (Entware), MikroTik (RouterOS), Linux, Docker
#
# Usage: 
#   curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh

set -e

# Configuration
REPO_OWNER="Chistovik92"
REPO_NAME="HydraVPNforRouters"
GITHUB_API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"
RAW_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main"
RELEASE_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { printf "${BLUE}[INFO]${NC} %s\n" "$*"; }
log_ok() { printf "${GREEN}[OK]${NC} %s\n" "$*"; }
log_warn() { printf "${YELLOW}[WARN]${NC} %s\n" "$*"; }
log_err() { printf "${RED}[ERR]${NC} %s\n" "$*"; }

# Detect platform
detect_platform() {
    if [ -f /etc/openwrt_release ]; then
        . /etc/openwrt_release
        PLATFORM="openwrt"
        ARCH="${DISTRIB_ARCH}_${DISTRIB_TARGET//\//_}"
        PKG_MGR="opkg"
        CONFIG_DIR="/etc/hydravpn-router"
        SERVICE_CMD="/etc/init.d/hydravpn-router"
    elif [ -f /opt/etc/opkg.conf ] && (grep -q "keenetic" /opt/etc/opkg.conf 2>/dev/null || grep -q "entware" /opt/etc/opkg.conf 2>/dev/null); then
        PLATFORM="keenetic-entware"
        ARCH="mipsel_24kc"
        PKG_MGR="opkg"
        CONFIG_DIR="/opt/etc/hydravpn-router"
        SERVICE_CMD="/opt/etc/init.d/S99hydravpn-router"
    elif [ -d /flash ] && command -v /system >/dev/null 2>&1; then
        PLATFORM="mikrotik"
        PKG_MGR=""
        CONFIG_DIR="/flash/hydravpn-router"
        SERVICE_CMD=""
    elif command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1; then
        PLATFORM="docker"
        PKG_MGR="docker"
        CONFIG_DIR="/etc/hydravpn-router"
        SERVICE_CMD=""
    else
        PLATFORM="linux"
        ARCH=$(uname -m)
        case "$ARCH" in
            x86_64) ARCH="amd64" ;;
            aarch64) ARCH="arm64" ;;
            armv7l) ARCH="armv7" ;;
            mips) ARCH="mips" ;;
            mipsel) ARCH="mipsle" ;;
            *) log_err "Unsupported architecture: $ARCH"; exit 1 ;;
        esac
        PKG_MGR="binary"
        CONFIG_DIR="/etc/hydravpn-router"
        SERVICE_CMD="systemctl"
    fi
    log_info "Platform: $PLATFORM (arch: ${ARCH:-auto})"
}

# Get latest version
get_latest_version() {
    log_info "Fetching latest version from GitHub..."
    LATEST_VERSION=$(curl -fsSL "${GITHUB_API}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')
    
    if [ -z "$LATEST_VERSION" ]; then
        LATEST_VERSION=$(curl -fsSL "${GITHUB_API}/tags" 2>/dev/null | grep '"name"' | head -1 | sed -E 's/.*"name": "([^"]+)".*/\1/')
    fi
    
    if [ -z "$LATEST_VERSION" ]; then
        log_err "Failed to fetch latest version"
        return 1
    fi
    
    log_ok "Latest version: $LATEST_VERSION"
}

# Check if already installed
check_installed() {
    case "$PLATFORM" in
        openwrt|keenetic-entware|linux)
            if command -v hydravpn-router >/dev/null 2>&1; then
                INSTALLED_VERSION=$(hydravpn-router version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
                log_info "Already installed: v$INSTALLED_VERSION"
                return 0
            fi
            ;;
        mikrotik)
            if /system package print | grep -q hydravpn-router; then
                INSTALLED_VERSION=$(/system package print where name=hydravpn-router | grep version | sed 's/.*version=\([^ ]*\).*/\1/')
                log_info "Already installed (NPK): v$INSTALLED_VERSION"
                return 0
            fi
            if /container print where name=hydravpn-router 2>/dev/null | grep -q image; then
                INSTALLED_VERSION=$(/container print where name=hydravpn-router | grep image | sed 's/.*image=\([^ ]*\).*/\1/' | sed 's/.*://')
                log_info "Already installed (Docker): v$INSTALLED_VERSION"
                return 0
            fi
            ;;
        docker)
            if docker ps -a --format '{{.Image}}' | grep -q hydravpn-router; then
                INSTALLED_VERSION=$(docker inspect ghcr.io/chistovik92/hydravpn-router:latest 2>/dev/null | grep '"Tag"' | sed 's/.*"latest"//' | sed 's/.*://' | tr -d '", ')
                log_info "Already installed (Docker): v$INSTALLED_VERSION"
                return 0
            fi
            ;;
    esac
    return 1
}

# Ask for confirmation
ask_confirm() {
    if [ "$AUTO_YES" = "1" ]; then
        return 0
    fi
    
    if [ -n "$INSTALLED_VERSION" ]; then
        printf "\n${YELLOW}Установлена версия: ${INSTALLED_VERSION}${NC}\n"
        printf "Доступная версия: ${LATEST_VERSION}\n"
        printf "Переустановить/обновить? [y/N]: "
    else
        printf "\n${YELLOW}Установить HydraVPN for Router ${LATEST_VERSION}?${NC}\n"
        printf "Продолжить? [y/N]: "
    fi
    read -r REPLY
    case "$REPLY" in
        [Yy]*) return 0 ;;
        *) log_info "Установка отменена"; exit 0 ;;
    esac
}

# Install OpenWRT
install_openwrt() {
    log_info "Installing on OpenWRT..."
    
    # Add repository
    REPO_URL="${RELEASE_BASE}/${LATEST_VERSION}/packages/openwrt/${ARCH}/"
    if ! grep -q "hydravpn_router" /etc/opkg/customfeeds.conf 2>/dev/null; then
        echo "src/gz hydravpn_router ${REPO_URL}" >> /etc/opkg/customfeeds.conf
        log_ok "Repository added: $REPO_URL"
    fi
    
    opkg update
    opkg install hydravpn-router
    
    # Create default config if not exists
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        curl -fsSL "${RAW_BASE}/configs/config.yaml" -o "$CONFIG_DIR/config.yaml" 2>/dev/null || \
        cat > "$CONFIG_DIR/config.yaml" << 'EOF'
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

sections:
  - name: "my-subscription"
    label: "My VPN"
    enabled: false
    action: "connection"
    selector_proxy_links: []
    community_lists: []
    rule_set: []

interfaces:
  - section: "my-subscription"
    name: "tun0"
    domain_resolver_enabled: true

subscription_urls: []

urltests: []

servers: []

rules: []

rule_sets: []

community_lists: []
EOF
        log_ok "Default config created at $CONFIG_DIR/config.yaml"
    fi
    
    $SERVICE_CMD enable
    $SERVICE_CMD start
    log_ok "OpenWRT installation completed"
}

# Install KeeneticOS Entware
install_keenetic_entware() {
    log_info "Installing on KeeneticOS (Entware)..."
    
    # Add repository
    REPO_URL="${RELEASE_BASE}/${LATEST_VERSION}/packages/keenetic/"
    if ! grep -q "hydravpn_keenetic" /opt/etc/opkg.conf 2>/dev/null; then
        echo "src/gz hydravpn_keenetic ${REPO_URL}" >> /opt/etc/opkg.conf
        log_ok "Repository added: $REPO_URL"
    fi
    
    opkg update
    opkg install hydravpn-router
    
    # Create default config
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        curl -fsSL "${RAW_BASE}/configs/config.yaml" -o "$CONFIG_DIR/config.yaml" 2>/dev/null || \
        cat > "$CONFIG_DIR/config.yaml" << 'EOF'
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
    - "br0"
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
  config_path: "/opt/etc/hydravpn-router/sing-box/config.json"
  cache_path: "/opt/tmp/hydravpn-router/cache.db"
  log_level: "warn"
  exclude_ntp: false
  shutdown_correctly: false

sections:
  - name: "my-subscription"
    label: "My VPN"
    enabled: false
    action: "connection"
    selector_proxy_links: []
    community_lists: []
    rule_set: []

interfaces:
  - section: "my-subscription"
    name: "tun0"
    domain_resolver_enabled: true

subscription_urls: []

urltests: []

servers: []

rules: []

rule_sets: []

community_lists: []
EOF
        log_ok "Default config created"
    fi
    
    # Create init script if not exists
    if [ ! -f "$SERVICE_CMD" ]; then
        cat > "$SERVICE_CMD" << 'EOF'
#!/bin/sh
ENABLED=yes
PROCS=hydravpn-router
ARGS="start -c /opt/etc/hydravpn-router/config.yaml"
PREARGS=""
DESC="HydraVPN for Router"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

. /opt/etc/init.d/rc.func
EOF
        chmod +x "$SERVICE_CMD"
    fi
    
    $SERVICE_CMD enable
    $SERVICE_CMD start
    log_ok "KeeneticOS Entware installation completed"
}

# Install KeeneticOS KNP (manual download)
install_keenetic_knp() {
    log_info "Installing KeeneticOS KNP package..."
    
    KNP_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router_${LATEST_VERSION}.knp"
    log_info "Download KNP from: $KNP_URL"
    log_warn "Please install manually via Web UI:"
    log_warn "  1. Download: wget '$KNP_URL' -O /tmp/hydravpn-router.knp"
    log_warn "  2. Web UI: http://keenetic.local -> System -> Components -> Add Component"
    log_warn "  3. Select file: /tmp/hydravpn-router.knp"
    log_warn "  4. Configure: http://keenetic.local/hydravpn-router"
}

# Install MikroTik Docker
install_mikrotik_docker() {
    log_info "Installing on MikroTik (Docker)..."
    
    # Check if container feature is enabled
    if ! /system package print | grep -q "container"; then
        log_err "Container package not installed. Enable in: System -> Packages -> Container"
        return 1
    fi
    
    # Configure registry
    /container config set registry-url=https://registry-1.docker.io tmpdir=disk1/pull
    
    # Create veth interface
    if ! /interface veth print | grep -q "veth-hydravpn"; then
        /interface veth add name=veth-hydravpn address=172.17.0.2/24 gateway=172.17.0.1
        log_ok "Created veth-hydravpn interface"
    fi
    
    # Create mount directories
    /file mkdir name=disk1/hydravpn-config 2>/dev/null || true
    /file mkdir name=disk1/hydravpn-cache 2>/dev/null || true
    
    # Add container
    /container add name=hydravpn-router \
        image=ghcr.io/chistovik92/hydravpn-router:${LATEST_VERSION} \
        interface=veth-hydravpn \
        mounts=disk1/hydravpn-config:/etc/hydravpn-router,disk1/hydravpn-cache:/tmp/hydravpn-router \
        dns=77.88.8.8,77.88.8.1 \
        logging=yes \
        envlist="TZ=Europe/Moscow"
    
    /container start hydravpn-router
    
    # Add NAT for Web UI
    /ip firewall nat add chain=dstnat action=dst-nat to-addresses=172.17.0.2 to-ports=8080 protocol=tcp dst-port=8080 comment="HydraVPN Web UI" 2>/dev/null || true
    
    log_ok "MikroTik Docker installation completed"
    log_info "Web UI: http://<router-ip>:8080"
}

# Install MikroTik NPK
install_mikrotik_npk() {
    log_info "Installing MikroTik NPK package..."
    
    NPK_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router-${LATEST_VERSION}.npk"
    
    # Download NPK
    /tool fetch url="$NPK_URL" dst-path="/hydravpn-router-${LATEST_VERSION}.npk" mode=https
    
    # Install
    /system package install file-name=hydravpn-router-${LATEST_VERSION}.npk
    
    log_warn "Reboot required! Run: /system reboot"
}

# Install Generic Linux Binary
install_linux_binary() {
    log_info "Installing Linux binary..."
    
    BINARY_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router-${LATEST_VERSION}-linux-${ARCH}"
    
    # Download binary
    log_info "Downloading: $BINARY_URL"
    curl -fsSL "$BINARY_URL" -o /usr/local/bin/hydravpn-router
    chmod +x /usr/local/bin/hydravpn-router
    
    # Create config directory
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        curl -fsSL "${RAW_BASE}/configs/config.yaml" -o "$CONFIG_DIR/config.yaml" 2>/dev/null || \
        cat > "$CONFIG_DIR/config.yaml" << 'EOF'
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
    - "eth0"
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

sections:
  - name: "my-subscription"
    label: "My VPN"
    enabled: false
    action: "connection"
    selector_proxy_links: []
    community_lists: []
    rule_set: []

interfaces:
  - section: "my-subscription"
    name: "tun0"
    domain_resolver_enabled: true

subscription_urls: []

urltests: []

servers: []

rules: []

rule_sets: []

community_lists: []
EOF
        log_ok "Default config created"
    fi
    
    # Create systemd service
    if command -v systemctl >/dev/null 2>&1; then
        cat > /etc/systemd/system/hydravpn-router.service << EOF
[Unit]
Description=HydraVPN for Router
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hydravpn-router start -c /etc/hydravpn-router/config.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable hydravpn-router
        systemctl start hydravpn-router
        log_ok "Systemd service created and started"
    fi
    
    log_ok "Linux binary installation completed"
}

# Install Docker (generic)
install_docker() {
    log_info "Installing Docker container..."
    
    docker pull ghcr.io/chistovik92/hydravpn-router:${LATEST_VERSION}
    docker pull ghcr.io/chistovik92/hydravpn-router:latest
    
    log_ok "Docker images pulled"
    log_info "Run container with:"
    log_info "  docker run -d --name hydravpn-router \\"
    log_info "    --cap-add=NET_ADMIN --cap-add=SYS_RESOURCE \\"
    log_info "    -v /etc/hydravpn-router:/etc/hydravpn-router \\"
    log_info "    -v /tmp/hydravpn-router:/tmp/hydravpn-router \\"
    log_info "    --network host \\"
    log_info "    ghcr.io/chistovik92/hydravpn-router:${LATEST_VERSION}"
}

# Main install function
do_install() {
    case "$PLATFORM" in
        openwrt)
            install_openwrt
            ;;
        keenetic-entware)
            install_keenetic_entware
            ;;
        keenetic-knp)
            install_keenetic_knp
            ;;
        mikrotik)
            # Ask which method
            if [ "$AUTO_YES" = "1" ] || [ "$INSTALL_METHOD" = "docker" ]; then
                install_mikrotik_docker
            elif [ "$INSTALL_METHOD" = "npk" ]; then
                install_mikrotik_npk
            else
                log_info "Choose installation method for MikroTik:"
                log_info "  1) Docker (recommended for RouterOS 7+)"
                log_info "  2) NPK (native package)"
                printf "Choice [1/2]: "
                read -r CHOICE
                case "$CHOICE" in
                    1) install_mikrotik_docker ;;
                    2) install_mikrotik_npk ;;
                    *) log_err "Invalid choice"; exit 1 ;;
                esac
            fi
            ;;
        docker)
            install_docker
            ;;
        linux)
            install_linux_binary
            ;;
        *)
            log_err "Unsupported platform: $PLATFORM"
            exit 1
            ;;
    esac
}

# Show help
show_help() {
    cat << EOF
HydraVPN for Router - Universal Auto-Install Script

Usage: $0 [OPTIONS]

Options:
  -y, --yes          Auto-confirm install (non-interactive)
  -m, --method       Install method for MikroTik: docker|npk
  -h, --help         Show this help

Examples:
  # Interactive install
  $0
  
  # Non-interactive (for automation)
  $0 --yes
  
  # MikroTik with specific method
  $0 --method docker
  $0 --method npk

Quick install (one-liner):
  curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
  wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh

Supported platforms:
  - OpenWRT (opkg repository, auto-detects architecture)
  - KeeneticOS (Entware opkg + KNP manual)
  - MikroTik RouterOS (Docker + NPK)
  - Generic Linux (binary + systemd)
  - Docker/Podman

The script automatically:
  1. Detects your platform
  2. Fetches latest version from GitHub
  3. Installs appropriate package
  4. Creates default configuration
  5. Starts the service

EOF
}

# Main
main() {
    AUTO_YES=0
    INSTALL_METHOD=""
    
    while [ $# -gt 0 ]; do
        case "$1" in
            -y|--yes) AUTO_YES=1 ;;
            -m|--method) INSTALL_METHOD="$2"; shift ;;
            -h|--help) show_help; exit 0 ;;
            *) log_err "Unknown option: $1"; show_help; exit 1 ;;
        esac
        shift
    done
    
    printf "\n${BLUE}=== HydraVPN for Router Auto-Install ===${NC}\n"
    printf "Repository: ${REPO_OWNER}/${REPO_NAME}\n\n"
    
    detect_platform
    get_latest_version || exit 1
    check_installed
    ask_confirm
    do_install
    
    # Verify
    if check_installed; then
        log_ok "Installation verified: v$INSTALLED_VERSION"
    else
        log_warn "Installation completed but verification failed"
    fi
    
    printf "\n${GREEN}=== Next Steps ===${NC}\n"
    printf "1. Edit config: ${CONFIG_DIR}/config.yaml\n"
    printf "2. Add subscription URL in sections[0].subscription_urls[0].url\n"
    printf "3. Restart: ${SERVICE_CMD:-hydravpn-router} restart\n"
    printf "4. Check status: hydravpn-router status\n"
    printf "5. Run diagnostics: hydravpn-router check all\n"
    printf "\nWeb UI:\n"
    case "$PLATFORM" in
        openwrt) printf "  http://router.ip/cgi-bin/luci/admin/services/hydravpn-router\n" ;;
        keenetic-entware) printf "  http://keenetic.local/hydravpn-router\n" ;;
        mikrotik) printf "  http://<container-ip>:8080 (Docker) or WinBox\n" ;;
        *) printf "  http://localhost:8080\n" ;;
    esac
    printf "\nDocs: https://github.com/Chistovik92/HydraVPNforRouters/blob/main/INSTALL.md\n"
}

main "$@"