# HydraVPN for Router

> **Multi-platform DPI bypass solution for routers**

HydraVPN for Router is a comprehensive, platform-agnostic implementation of DPI bypass functionality for routers, inspired by Forkop and HydraVPN. It provides a unified solution that runs on OpenWRT, KeeneticOS, and MikroTik RouterOS with full feature parity.

## Features

### Core Capabilities
- **Multi-protocol proxy core** - Built on sing-box supporting VMess, VLESS, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard, and more
- **DPI Bypass** - Multiple bypass methods:
  - zapret/zapret2 (NFQWS/NFQWS2) - Advanced DPI desynchronization
  - ByeDPI (ciadpi) - Passive DPI bypass
- **Flexible Routing** - Domain, IP, GeoIP, GeoSite, process, port, and network-based routing rules
- **Subscription Management** - Auto-update from VLESS/VMess/Trojan/SS/Hysteria2/Clash URLs
- **DNS Management** - Failover, FakeIP, custom upstream servers, DoH/DoT support
- **Clash API Compatible** - Works with Clash Dashboard, YACD, and other Clash clients
- **Web UI** - Platform-native interfaces (LuCI for OpenWRT, native for KeeneticOS/MikroTik)

### Platform Support

| Platform | Package Format | Web UI | Init System | Status |
|----------|---------------|--------|-------------|--------|
| OpenWRT 21.02+ | IPK/APK | LuCI | procd | ✅ Full |
| KeeneticOS 3.7+ | KNP/Entware | Native | ndm/Entware | ✅ Full |
| RouterOS 7+ | NPK/Docker | Native | systemd/container | ✅ Full |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      HydraVPN for Router Core                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐   │
│  │ Config   │ │ Engine   │ │ DNS Mgr  │ │ Firewall Mgr │   │
│  │ Manager  │ │          │ │          │ │              │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐   │
│  │ Sub Mgr  │ │ Diagnost │ │  Clash   │ │  Platform    │   │
│  │          │ │  ics     │ │   API    │ │  Adapters    │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘   │
└─────────────────────────────────────────────────────────────┘
         │                    │                    │
    ┌────▼────┐         ┌────▼────┐         ┌────▼────┐
    │ OpenWRT │         │Keenetic │         │ MikroTik│
    │  (IPK)  │         │   (KNP) │         │ (NPK/Dkr)│
    └─────────┘         └─────────┘         └─────────┘
```

## Installation

### OpenWRT

```bash
# Install via opkg
opkg update
opkg install hydravpn-router

# Or install IPK manually
opkg install podkop-plus_1.0.0_all.ipk

# Enable and start
/etc/init.d/hydravpn-router enable
/etc/init.d/hydravpn-router start
```

### KeeneticOS

```bash
# Via Entware
opkg update
opkg install hydravpn-router

# Or install KNP package via web interface
# System → Components → Add component → hydravpn-router.knp
```

### MikroTik RouterOS

```bash
# Via NPK package
# Copy hydravpn-router.npk to router Files
# System → Packages → Install

# Or via Docker container
/container add name=hydravpn-router image=hydravpn-router:latest \
    interface=veth1 mounts=hydravpn-router-config:/etc/hydravpn-router \
    dns=77.88.8.8,77.88.8.1 logging=yes
```

## Configuration

HydraVPN for Router uses a unified YAML configuration file (`/etc/hydravpn-router/config.yaml`) across all platforms:

```yaml
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
    enabled: true
    action: "connection"
    selector_proxy_links: []
    community_lists:
      - "russia_inside"
    rule_set:
      - "https://example.com/rules.srs"

interfaces:
  - section: "my-subscription"
    name: "tun0"
    domain_resolver_enabled: true

subscription_urls:
  - section: "my-subscription"
    url: "https://example.com/subscription"
    subscription_update_enabled: true
    subscription_update_interval: "1h"
    download_via_proxy_enabled: false
    show_dashboard_metadata: true

urltests:
  - section: "my-subscription"
    name: "Fastest"
    check_interval: "3m"
    tolerance: 50
    testing_url: "https://www.gstatic.com/generate_204"
    idle_timeout: "30m"
    interrupt_exist_connections: true
    pin_dashboard: true

servers:
  - name: "vless-reality"
    label: "VLESS Reality"
    enabled: true
    protocol: "vless"
    listen: "0.0.0.0"
    listen_port: 443
    public_host: "vpn.example.com"
    routing_mode: "rules"
    security: "reality"
    server_uuid: "00000000-0000-0000-0000-000000000000"
    reality_private_key: "PRIVATE_KEY"
    reality_public_key: "PUBLIC_KEY"
    reality_short_id: "5a"
    transport: "tcp"

rules:
  - section: "my-subscription"
    enabled: true
    outbound: "proxy-out"
    domain_suffix:
      - "google.com"
      - "youtube.com"
    geoip:
      - "US"
      - "EU"

rule_sets:
  - name: "russia_inside"
    url: "https://github.com/itdoginfo/allow-domains/releases/latest/download/russia_inside.srs"
    interval: "24h"

community_lists:
  - name: "russia_inside"
    type: "domain"
    url: "https://github.com/itdoginfo/allow-domains/releases/latest/download/russia_inside.lst"
    interval: "24h"
```

## CLI Usage

```bash
# Start service
hydravpn-router start -c /etc/hydravpn-router/config.yaml

# Stop service
hydravpn-router stop

# Reload configuration
hydravpn-router reload -c /etc/hydravpn-router/config.yaml

# Show status
hydravpn-router status

# Show configuration
hydravpn-router config

# Show version
hydravpn-router version

# Run diagnostics
hydravpn-router check all
hydravpn-router check proxy
hydravpn-router check dns
hydravpn-router check singbox
```

## Web UI

### OpenWRT (LuCI)
Access via `http://router.ip/cgi-bin/luci/admin/services/hydravpn-router`

### KeeneticOS
Access via `http://router.ip/hydravpn-router` or `http://router.ip:8080`

### MikroTik
Access via `http://router.ip:8080` (container) or WinBox/WebFig

## Building from Source

### Prerequisites
- Go 1.23+
- Docker (for cross-platform builds)
- OpenWRT SDK (for IPK/APK)
- KeeneticOS SDK (for KNP)
- MikroTik RouterOS build environment (for NPK)

### Build Commands

```bash
# Build all platforms
./scripts/build.sh 1.0.0 ./dist

# Build specific platform
GOOS=linux GOARCH=amd64 go build -o hydravpn-router ./cmd/hydravpn-router

# Build OpenWRT packages
docker build -f build/openwrt/Dockerfile -t hydravpn-router-openwrt .
docker run --rm -v $(pwd)/dist:/output hydravpn-router-openwrt

# Build KeeneticOS package
cd internal/platform/keenetic && go run . generate-knp 1.0.0 ./dist

# Build MikroTik package
cd internal/platform/mikrotik && go run . generate-npk 1.0.0 ./dist
```

## API

### Clash API (Port 9090)
- `GET /configs` - Get configuration
- `GET /proxies` - List proxies and groups
- `PUT /proxies/{name}` - Select proxy in group
- `GET /rules` - List routing rules
- `GET /connections` - Active connections
- `GET /traffics` - Traffic statistics
- `GET /memory` - Memory usage
- `GET /version` - Version info
- `GET /logs` - WebSocket log stream

### RPC API (LuCI)
- `get_status` - Service status
- `get_config` - Current configuration
- `set_config` - Update configuration
- `reload` - Reload service
- `get_subscriptions` - Subscription status
- `update_subscription` - Force update
- `run_diagnostics` - Run health checks

## Development

### Project Structure
```
hydravpn-router/
├── cmd/hydravpn-router/           # Main entry point
├── internal/
│   ├── config/                # Configuration types
│   ├── core/                  # Core engine
│   ├── dns/                   # DNS management
│   ├── firewall/              # Firewall abstraction
│   ├── providers/             # Provider implementations
│   │   ├── singbox/           # sing-box provider
│   │   ├── zapret/            # zapret/zapret2 provider
│   │   └── byedpi/            # ByeDPI provider
│   ├── subscription/          # Subscription management
│   ├── diagnostics/           # Health checks
│   ├── api/                   # Clash API server
│   └── platform/              # Platform adapters
│       ├── openwrt/           # OpenWRT integration
│       ├── keenetic/          # KeeneticOS integration
│       └── mikrotik/          # MikroTik integration
├── pkg/
│   ├── version/               # Version info
│   └── utils/                 # Utilities
├── web/                       # Web UI sources
│   ├── luci/                  # LuCI interface
│   ├── keenetic/              # KeeneticOS UI
│   └── mikrotik/              # MikroTik UI
├── build/                     # Build scripts
├── configs/                   # Default configs
├── docs/                      # Documentation
└── scripts/                   # Utility scripts
```

### Adding a New Provider

1. Create provider in `internal/providers/newprovider/`
2. Implement `Provider` interface:
   ```go
   type Provider interface {
       Start(ctx context.Context) error
       Stop() error
       Reload(cfg *Config) error
       GetStatus() map[string]interface{}
   }
   ```
3. Register in engine initialization

### Adding a New Platform

1. Create platform in `internal/platform/newplatform/`
2. Implement `Platform` interface:
   ```go
   type Platform interface {
       Initialize(ctx context.Context) error
       Start(ctx context.Context) error
       Stop() error
       Reload(cfg *config.Config) error
       GetSystemInfo() map[string]interface{}
       GeneratePackage(version, outputDir string) error
   }
   ```

## License

GPL-3.0-or-later

## Credits

- **Forkop** - Original OpenWRT DPI bypass implementation
- **HydraVPN** - Mobile VPN client reference
- **sing-box** - Universal proxy platform
- **zapret/zapret2** - DPI bypass tools
- **ByeDPI/ciadpi** - Passive DPI bypass
- **OpenWRT/KeeneticOS/MikroTik** - Target platforms

## Support

- GitHub Issues: https://github.com/Chistovik92/hydravpn-router/issues
- Documentation: https://github.com/Chistovik92/hydravpn-router/wiki
- Telegram: @podkop_plus



