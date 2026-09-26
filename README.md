# HydraVPN for Router

> **Мультиплатформенное решение для обхода DPI на роутерах**  
> **Multi-platform DPI bypass solution for routers**

---

**HydraVPN for Router** — это комплексная, не зависящая от платформы реализация функционала обхода DPI для роутеров, вдохновленная Forkop и HydraVPN. Предоставляет единое решение, работающее на OpenWRT, KeeneticOS и MikroTik RouterOS с полной функциональной совместимостью.

**HydraVPN for Router** is a comprehensive, platform-agnostic implementation of DPI bypass functionality for routers, inspired by Forkop and HydraVPN. It provides a unified solution that runs on OpenWRT, KeeneticOS, and MikroTik RouterOS with full feature parity.

## Возможности / Features

### Основные возможности / Core Capabilities
- **Мультипротокольное прокси-ядро** — на базе sing-box с поддержкой VMess, VLESS, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard и других
- **Multi-protocol proxy core** — built on sing-box supporting VMess, VLESS, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard, and more
- **Обход DPI** — несколько методов:
- **DPI Bypass** — multiple bypass methods:
  - zapret/zapret2 (NFQWS/NFQWS2) — продвинутая десинхронизация DPI
  - zapret/zapret2 (NFQWS/NFQWS2) — advanced DPI desynchronization
  - ByeDPI (ciadpi) — пассивный обход DPI
  - ByeDPI (ciadpi) — passive DPI bypass
- **Гибкая маршрутизация** — правила по доменам, IP, GeoIP, GeoSite, процессам, портам, сетям
- **Flexible Routing** — domain, IP, GeoIP, GeoSite, process, port, and network-based routing rules
- **Управление подписками** — автообновление из VLESS/VMess/Trojan/SS/Hysteria2/Clash URL
- **Subscription Management** — auto-update from VLESS/VMess/Trojan/SS/Hysteria2/Clash URLs
- **DNS-менеджмент** — фейловер, FakeIP, кастомные апстримы, DoH/DoT
- **DNS Management** — failover, FakeIP, custom upstream servers, DoH/DoT support
- **Clash API совместимость** — работает с Clash Dashboard, YACD и другими клиентами
- **Clash API Compatible** — works with Clash Dashboard, YACD, and other Clash clients
- **Веб-интерфейс** — нативные UI для каждой платформы (LuCI для OpenWRT, нативные для KeeneticOS/MikroTik)
- **Web UI** — platform-native interfaces (LuCI for OpenWRT, native for KeeneticOS/MikroTik)

### Поддержка платформ / Platform Support

| Платформа / Platform | Формат пакета / Package Format | Веб-UI / Web UI | Init система / Init System | Статус / Status |
|----------|---------------|--------|-------------|--------|
| OpenWRT 21.02+ | IPK/APK | LuCI | procd | ✅ Полная / Full |
| KeeneticOS 3.7+ | KNP/Entware | Native | ndm/Entware | ✅ Полная / Full |
| RouterOS 7+ | NPK/Docker | Native | systemd/container | ✅ Полная / Full |

## Архитектура / Architecture

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

## Установка / Installation

### OpenWRT

```bash
# Установка через opkg / Install via opkg
opkg update
opkg install hydravpn-router

# Или установить IPK вручную / Or install IPK manually
opkg install hydravpn-router_1.0.0_all.ipk

# Включить и запустить / Enable and start
/etc/init.d/hydravpn-router enable
/etc/init.d/hydravpn-router start
```

### KeeneticOS

```bash
# Через Entware / Via Entware
opkg update
opkg install hydravpn-router

# Или установить KNP пакет через веб-интерфейс / Or install KNP package via web interface
# System → Components → Add component → hydravpn-router.knp
```

### MikroTik RouterOS

```bash
# Через NPK пакет / Via NPK package
# Скопируйте hydravpn-router.npk в Files роутера / Copy hydravpn-router.npk to router Files
# System → Packages → Install

# Или через Docker контейнер / Or via Docker container
/container add name=hydravpn-router image=hydravpn-router:latest \
    interface=veth1 mounts=hydravpn-router-config:/etc/hydravpn-router \
    dns=77.88.8.8,77.88.8.1 logging=yes
```

## Конфигурация / Configuration

HydraVPN for Router использует единый YAML конфигурационный файл (`/etc/hydravpn-router/config.yaml`) на всех платформах:

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

## Использование CLI / CLI Usage

```bash
# Запуск сервиса / Start service
hydravpn-router start -c /etc/hydravpn-router/config.yaml

# Остановка сервиса / Stop service
hydravpn-router stop

# Перезагрузка конфигурации / Reload configuration
hydravpn-router reload -c /etc/hydravpn-router/config.yaml

# Показать статус / Show status
hydravpn-router status

# Показать конфигурацию / Show configuration
hydravpn-router config

# Показать версию / Show version
hydravpn-router version

# Запуск диагностики / Run diagnostics
hydravpn-router check all
hydravpn-router check proxy
hydravpn-router check dns
hydravpn-router check singbox
hydravpn-router check inbounds
hydravpn-router check fakeip
hydravpn-router check nft
```

## Веб-интерфейс / Web UI

### OpenWRT (LuCI)
Доступ через `http://router.ip/cgi-bin/luci/admin/services/hydravpn-router`  
Access via `http://router.ip/cgi-bin/luci/admin/services/hydravpn-router`

### KeeneticOS
Доступ через `http://router.ip/hydravpn-router` или `http://router.ip:8080`  
Access via `http://router.ip/hydravpn-router` or `http://router.ip:8080`

### MikroTik
Доступ через `http://router.ip:8080` (контейнер) или WinBox/WebFig  
Access via `http://router.ip:8080` (container) or WinBox/WebFig

## Сборка из исходников / Building from Source

### Требования / Prerequisites
- Go 1.23+
- Docker (для кросс-платформенных сборок / for cross-platform builds)
- OpenWRT SDK (для IPK/APK / for IPK/APK)
- KeeneticOS SDK (для KNP / for KNP)
- Сборочное окружение MikroTik RouterOS (для NPK / for NPK)

### Команды сборки / Build Commands

```bash
# Сборка всех платформ / Build all platforms
./scripts/build.sh 1.0.0 ./dist

# Сборка конкретной платформы / Build specific platform
GOOS=linux GOARCH=amd64 go build -o hydravpn-router ./cmd/hydravpn-router

# Сборка OpenWRT пакетов / Build OpenWRT packages
docker build -f build/openwrt/Dockerfile -t hydravpn-router-openwrt .
docker run --rm -v $(pwd)/dist:/output hydravpn-router-openwrt

# Сборка KeeneticOS пакета / Build KeeneticOS package
cd internal/platform/keenetic && go run . generate-knp 1.0.0 ./dist

# Сборка MikroTik пакета / Build MikroTik package
cd internal/platform/mikrotik && go run . generate-npk 1.0.0 ./dist
```

## API

### Clash API (порт 9090 / Port 9090)
- `GET /configs` — получить конфигурацию / get configuration
- `GET /proxies` — список прокси и групп / list proxies and groups
- `PUT /proxies/{name}` — выбрать прокси в группе / select proxy in group
- `GET /rules` — список правил маршрутизации / list routing rules
- `GET /connections` — активные соединения / active connections
- `GET /traffics` — статистика трафика / traffic statistics
- `GET /memory` — использование памяти / memory usage
- `GET /version` — информация о версии / version info
- `GET /logs` — WebSocket поток логов / WebSocket log stream

### RPC API (LuCI)
- `get_status` — статус сервиса / service status
- `get_config` — текущая конфигурация / current configuration
- `set_config` — обновить конфигурацию / update configuration
- `reload` — перезагрузить сервис / reload service
- `get_subscriptions` — статус подписок / subscription status
- `update_subscription` — принудительное обновление / force update
- `run_diagnostics` — запуск проверок здоровья / run health checks

## Разработка / Development

### Структура проекта / Project Structure
```
hydravpn-router/
├── cmd/hydravpn-router/           # Главная точка входа / Main entry point
├── internal/
│   ├── config/                # Типы конфигурации / Configuration types
│   ├── core/                  # Основной движок / Core engine
│   ├── dns/                   # DNS управление / DNS management
│   ├── firewall/              # Абстракция файрвола / Firewall abstraction
│   ├── providers/             # Реализации провайдеров / Provider implementations
│   │   ├── singbox/           # sing-box провайдер / sing-box provider
│   │   ├── zapret/            # zapret/zapret2 провайдер / zapret/zapret2 provider
│   │   └── byedpi/            # ByeDPI провайдер / ByeDPI provider
│   ├── subscription/          # Управление подписками / Subscription management
│   ├── diagnostics/           # Проверки здоровья / Health checks
│   ├── api/                   # Clash API сервер / Clash API server
│   └── platform/              # Адаптеры платформ / Platform adapters
│       ├── openwrt/           # OpenWRT интеграция / OpenWRT integration
│       ├── keenetic/          # KeeneticOS интеграция / KeeneticOS integration
│       └── mikrotik/          # MikroTik интеграция / MikroTik integration
├── pkg/
│   ├── version/               # Информация о версии / Version info
│   └── utils/                 # Утилиты / Utilities
├── web/                       # Исходники Web UI / Web UI sources
│   ├── luci/                  # LuCI интерфейс / LuCI interface
│   ├── keenetic/              # KeeneticOS UI
│   └── mikrotik/              # MikroTik UI
├── build/                     # Скрипты сборки / Build scripts
├── configs/                   # Конфиги по умолчанию / Default configs
├── docs/                      # Документация / Documentation
└── scripts/                   # Утилитарные скрипты / Utility scripts
```

### Добавление нового провайдера / Adding a New Provider

1. Создайте провайдер в `internal/providers/newprovider/`
2. Реализуйте интерфейс `Provider`:
   ```go
   type Provider interface {
       Start(ctx context.Context) error
       Stop() error
       Reload(cfg *Config) error
       GetStatus() map[string]interface{}
   }
   ```
3. Зарегистрируйте при инициализации движка

### Добавление новой платформы / Adding a New Platform

1. Создайте платформу в `internal/platform/newplatform/`
2. Реализуйте интерфейс `Platform`:
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

## Лицензия / License

GPL-3.0-or-later

## Благодарности / Credits

- **Forkop** — оригинальная OpenWRT реализация обхода DPI / Original OpenWRT DPI bypass implementation
- **HydraVPN** — мобильный VPN клиент-референс / Mobile VPN client reference
- **sing-box** — универсальная прокси-платформа / Universal proxy platform
- **zapret/zapret2** — инструменты обхода DPI / DPI bypass tools
- **ByeDPI/ciadpi** — пассивный обход DPI / Passive DPI bypass
- **OpenWRT/KeeneticOS/MikroTik** — целевые платформы / Target platforms

## Поддержка / Support

- GitHub Issues: https://github.com/Chistovik92/HydraVPNforRouters/issues
- Документация / Documentation: https://github.com/Chistovik92/HydraVPNforRouters/wiki
- Telegram: @podkop_plus