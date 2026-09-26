#!/bin/sh
# HydraVPN for Router - Universal Auto-Update Script
# Checks GitHub for latest version and updates automatically
# Works on: OpenWRT, KeeneticOS (Entware), MikroTik (RouterOS), Linux
#
# Usage: 
#   curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/update.sh | sh
#   wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/update.sh | sh

set -e

# Configuration
REPO_OWNER="Chistovik92"
REPO_NAME="HydraVPNforRouters"
GITHUB_API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}"
RAW_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main"
RELEASE_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
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
        BINARY_NAME="hydravpn-router"
    elif [ -f /opt/etc/opkg.conf ] && grep -q "keenetic" /opt/etc/opkg.conf 2>/dev/null; then
        PLATFORM="keenetic-entware"
        ARCH="mipsel_24kc"  # Most Keenetic devices are MIPSLE
        PKG_MGR="opkg"
        CONFIG_DIR="/opt/etc/hydravpn-router"
        SERVICE_CMD="/opt/etc/init.d/S99hydravpn-router"
        BINARY_NAME="hydravpn-router"
    elif [ -d /flash ] && command -v /system >/dev/null 2>&1; then
        PLATFORM="mikrotik"
        PKG_MGR="npk"
        CONFIG_DIR="/flash/hydravpn-router"
        SERVICE_CMD=""
        BINARY_NAME=""
    elif command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1; then
        PLATFORM="docker"
        PKG_MGR="docker"
        CONFIG_DIR="/etc/hydravpn-router"
        SERVICE_CMD=""
        BINARY_NAME=""
    else
        # Generic Linux
        PLATFORM="linux"
        ARCH=$(uname -m)
        case "$ARCH" in
            x86_64) ARCH="amd64" ;;
            aarch64) ARCH="arm64" ;;
            armv7l) ARCH="armv7" ;;
            mips) ARCH="mips" ;;
            mipsel) ARCH="mipsle" ;;
        esac
        PKG_MGR="binary"
        CONFIG_DIR="/etc/hydravpn-router"
        SERVICE_CMD="systemctl"
        BINARY_NAME="hydravpn-router"
    fi
    log_info "Detected platform: $PLATFORM (arch: ${ARCH:-auto})"
}

# Get latest release version from GitHub API
get_latest_version() {
    log_info "Checking for latest version on GitHub..."
    LATEST_VERSION=$(curl -fsSL "${GITHUB_API}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')
    
    if [ -z "$LATEST_VERSION" ]; then
        # Fallback: try to get from tags
        LATEST_VERSION=$(curl -fsSL "${GITHUB_API}/tags" 2>/dev/null | grep '"name"' | head -1 | sed -E 's/.*"name": "([^"]+)".*/\1/')
    fi
    
    if [ -z "$LATEST_VERSION" ]; then
        log_err "Failed to fetch latest version from GitHub"
        return 1
    fi
    
    log_ok "Latest version: $LATEST_VERSION"
    return 0
}

# Get current installed version
get_current_version() {
    CURRENT_VERSION=""
    
    case "$PLATFORM" in
        openwrt|keenetic-entware|linux)
            if command -v hydravpn-router >/dev/null 2>&1; then
                CURRENT_VERSION=$(hydravpn-router version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
            fi
            ;;
        mikrotik)
            # Check NPK version
            if /system package print | grep -q hydravpn-router; then
                CURRENT_VERSION=$(/system package print where name=hydravpn-router | grep version | sed 's/.*version=\([^ ]*\).*/\1/')
            fi
            # Check Docker version
            if /container print where name=hydravpn-router 2>/dev/null | grep -q image; then
                CURRENT_VERSION=$(/container print where name=hydravpn-router | grep image | sed 's/.*image=\([^ ]*\).*/\1/' | sed 's/.*://')
            fi
            ;;
        docker)
            if command -v docker >/dev/null 2>&1; then
                CURRENT_VERSION=$(docker inspect ghcr.io/chistovik92/hydravpn-router:latest 2>/dev/null | grep '"Tag"' | sed 's/.*"latest"//' | sed 's/.*://' | tr -d '", ')
            fi
            ;;
    esac
    
    if [ -z "$CURRENT_VERSION" ]; then
        CURRENT_VERSION="not installed"
    fi
    log_info "Current version: $CURRENT_VERSION"
}

# Compare versions (simple string comparison for semantic versions)
version_gt() {
    # Returns 0 if $1 > $2
    [ "$1" = "$2" ] && return 1
    [ "$(printf '%s\n%s' "$2" "$1" | sort -V | head -1)" = "$2" ]
}

# Ask user for confirmation
ask_update() {
    if [ "$AUTO_YES" = "1" ]; then
        return 0
    fi
    
    printf "\n${YELLOW}Доступно обновление: ${CURRENT_VERSION} -> ${LATEST_VERSION}${NC}\n"
    printf "Обновить сейчас? [y/N]: "
    read -r REPLY
    case "$REPLY" in
        [Yy]*) return 0 ;;
        *) log_info "Обновление отменено пользователем"; return 1 ;;
    esac
}

# Update OpenWRT
update_openwrt() {
    log_info "Updating OpenWRT package..."
    
    # Add repo if not exists
    if ! grep -q "hydravpn_router" /etc/opkg/customfeeds.conf 2>/dev/null; then
        echo "src/gz hydravpn_router ${RELEASE_BASE}/${LATEST_VERSION}/packages/openwrt/${ARCH}/" >> /etc/opkg/customfeeds.conf
        log_ok "Repository added"
    fi
    
    opkg update
    opkg upgrade hydravpn-router
    
    if [ $? -eq 0 ]; then
        $SERVICE_CMD restart
        log_ok "OpenWRT update completed"
    else
        log_err "OpenWRT update failed"
        return 1
    fi
}

# Update KeeneticOS Entware
update_keenetic_entware() {
    log_info "Updating KeeneticOS Entware package..."
    
    # Add repo if not exists
    if ! grep -q "hydravpn_keenetic" /opt/etc/opkg.conf 2>/dev/null; then
        echo "src/gz hydravpn_keenetic ${RELEASE_BASE}/${LATEST_VERSION}/packages/keenetic/" >> /opt/etc/opkg.conf
        log_ok "Repository added"
    fi
    
    opkg update
    opkg upgrade hydravpn-router
    
    if [ $? -eq 0 ]; then
        $SERVICE_CMD restart
        log_ok "KeeneticOS Entware update completed"
    else
        log_err "KeeneticOS Entware update failed"
        return 1
    fi
}

# Update KeeneticOS KNP
update_keenetic_knp() {
    log_info "Updating KeeneticOS KNP package..."
    
    KNP_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router_${LATEST_VERSION}.knp"
    log_info "Downloading KNP from: $KNP_URL"
    
    # Download to /tmp and install via ndm
    cd /tmp
    wget -q "$KNP_URL" -O hydravpn-router.knp
    
    # Install via CLI (requires ndm)
    if command -v ndm >/dev/null 2>&1; then
        ndm -c "components install hydravpn-router.knp"
    else
        log_warn "Cannot auto-install KNP via CLI. Please install manually via Web UI:"
        log_warn "  System -> Components -> Add Component -> Select: /tmp/hydravpn-router.knp"
        return 1
    fi
    
    log_ok "KeeneticOS KNP update completed"
}

# Update MikroTik Docker
update_mikrotik_docker() {
    log_info "Updating MikroTik Docker container..."
    
    /container stop hydravpn-router 2>/dev/null || true
    /container remove hydravpn-router 2>/dev/null || true
    
    /container add name=hydravpn-router \
        image=ghcr.io/chistovik92/hydravpn-router:${LATEST_VERSION} \
        interface=veth-hydravpn \
        mounts=disk1/hydravpn-config:/etc/hydravpn-router,disk1/hydravpn-cache:/tmp/hydravpn-router \
        dns=77.88.8.8,77.88.8.1 \
        logging=yes \
        envlist="TZ=Europe/Moscow"
    
    /container start hydravpn-router
    log_ok "MikroTik Docker update completed"
}

# Update MikroTik NPK
update_mikrotik_npk() {
    log_info "Updating MikroTik NPK package..."
    
    NPK_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router-${LATEST_VERSION}.npk"
    log_info "Downloading NPK from: $NPK_URL"
    
    # Download via fetch
    /tool fetch url="$NPK_URL" dst-path="/hydravpn-router-${LATEST_VERSION}.npk" mode=https
    
    /system package install file-name=hydravpn-router-${LATEST_VERSION}.npk
    log_warn "Reboot required for NPK update"
    log_info "Run: /system reboot"
}

# Update generic Linux binary
update_linux_binary() {
    log_info "Updating Linux binary..."
    
    BINARY_URL="${RELEASE_BASE}/${LATEST_VERSION}/hydravpn-router-${LATEST_VERSION}-linux-${ARCH}"
    
    # Backup current binary
    if [ -f /usr/local/bin/hydravpn-router ]; then
        cp /usr/local/bin/hydravpn-router /usr/local/bin/hydravpn-router.backup
    fi
    
    # Download new binary
    log_info "Downloading from: $BINARY_URL"
    curl -fsSL "$BINARY_URL" -o /usr/local/bin/hydravpn-router.new
    chmod +x /usr/local/bin/hydravpn-router.new
    mv /usr/local/bin/hydravpn-router.new /usr/local/bin/hydravpn-router
    
    # Restart service if systemd
    if command -v systemctl >/dev/null 2>&1; then
        systemctl restart hydravpn-router 2>/dev/null || true
    fi
    
    log_ok "Linux binary update completed"
}

# Update Docker (generic)
update_docker() {
    log_info "Updating Docker container..."
    
    docker pull ghcr.io/chistovik92/hydravpn-router:${LATEST_VERSION}
    docker pull ghcr.io/chistovik92/hydravpn-router:latest
    
    # Restart container if running
    if docker ps --format '{{.Names}}' | grep -q hydravpn-router; then
        docker stop hydravpn-router
        docker rm hydravpn-router
        # User needs to re-run their docker run command
        log_warn "Container stopped. Re-run your docker run command to restart."
    fi
    
    log_ok "Docker image updated"
}

# Main update function
do_update() {
    case "$PLATFORM" in
        openwrt)
            update_openwrt
            ;;
        keenetic-entware)
            update_keenetic_entware
            ;;
        keenetic-knp)
            update_keenetic_knp
            ;;
        mikrotik)
            # Check which method is used
            if /container print where name=hydravpn-router 2>/dev/null | grep -q image; then
                update_mikrotik_docker
            else
                update_mikrotik_npk
            fi
            ;;
        docker)
            update_docker
            ;;
        linux)
            update_linux_binary
            ;;
        *)
            log_err "Unsupported platform: $PLATFORM"
            return 1
            ;;
    esac
}

# Show help
show_help() {
    cat << EOF
HydraVPN for Router - Universal Auto-Update Script

Usage: $0 [OPTIONS]

Options:
  -y, --yes          Auto-confirm update (non-interactive)
  -c, --check-only   Only check for updates, don't install
  -f, --force        Force update even if versions match
  -h, --help         Show this help

Examples:
  # Interactive update
  $0
  
  # Non-interactive (for cron)
  $0 --yes
  
  # Check only
  $0 --check-only
  
  # Force reinstall
  $0 --force

Installation (run once):
  curl -fsSL ${RAW_BASE}/scripts/update.sh | sh
  wget -qO- ${RAW_BASE}/scripts/update.sh | sh

Supported platforms:
  - OpenWRT (opkg repository)
  - KeeneticOS (Entware opkg + KNP)
  - MikroTik RouterOS (Docker + NPK)
  - Generic Linux (binary)
  - Docker/Podman

EOF
}

# Main
main() {
    AUTO_YES=0
    CHECK_ONLY=0
    FORCE=0
    
    while [ $# -gt 0 ]; do
        case "$1" in
            -y|--yes) AUTO_YES=1 ;;
            -c|--check-only) CHECK_ONLY=1 ;;
            -f|--force) FORCE=1 ;;
            -h|--help) show_help; exit 0 ;;
            *) log_err "Unknown option: $1"; show_help; exit 1 ;;
        esac
        shift
    done
    
    printf "\n${BLUE}=== HydraVPN for Router Auto-Update ===${NC}\n"
    printf "Repository: ${REPO_OWNER}/${REPO_NAME}\n\n"
    
    detect_platform
    get_latest_version || exit 1
    get_current_version
    
    # Check if update needed
    if [ "$CURRENT_VERSION" = "$LATEST_VERSION" ] && [ "$FORCE" = "0" ]; then
        log_ok "Already at latest version ($LATEST_VERSION)"
        exit 0
    fi
    
    if [ "$CHECK_ONLY" = "1" ]; then
        if [ "$CURRENT_VERSION" = "$LATEST_VERSION" ]; then
            log_ok "Up to date"
        else
            log_warn "Update available: $CURRENT_VERSION -> $LATEST_VERSION"
        fi
        exit 0
    fi
    
    ask_update || exit 0
    do_update
    
    # Verify update
    get_current_version
    if [ "$CURRENT_VERSION" = "$LATEST_VERSION" ]; then
        log_ok "Successfully updated to $LATEST_VERSION"
    else
        log_warn "Version mismatch after update. Current: $CURRENT_VERSION, Expected: $LATEST_VERSION"
    fi
}

main "$@"