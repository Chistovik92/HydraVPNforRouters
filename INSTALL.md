# Установка HydraVPN for Router / Installation Guide

> **Версия / Version**: 1.0.1  
> **Дата / Date**: 2026-09-27

---

## Репозитории и источники пакетов / Repositories & Package Sources

Все пакеты распространяются через **GitHub Releases** и контейнерные реестры:

| Платформа / Platform | Тип пакета / Package Type | Репозиторий / Repository | URL |
|----------|---------------|--------|-----|
| **OpenWRT** | IPK/APK | GitHub Releases (opkg repo) | `https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/openwrt/<arch>/` |
| **KeeneticOS** | KNP | GitHub Releases | `https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router_1.0.1.knp` |
| **KeeneticOS** | Entware (opkg) | Entware репозиторий (после добавления в feed) | `https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/keenetic/` |
| **MikroTik** | NPK | GitHub Releases | `https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router-1.0.1.npk` |
| **MikroTik** | Docker | GHCR (GitHub Container Registry) | `ghcr.io/chistovik92/hydravpn-router:1.0.1` |
| **Все / All** | Бинарники / Binaries | GitHub Releases | `https://github.com/Chistovik92/HydraVPNforRouters/releases/tag/v1.0.1` |

**Поддерживаемые архитектуры OpenWRT / OpenWRT Architectures:**
- `x86_64` — x86_64 (Intel/AMD 64-bit)
- `aarch64_cortex-a53` — ARM64 (Raspberry Pi 4, NanoPi R4S, etc.)
- `arm_cortex-a7_neon-vfpv4` — ARMv7 (NanoPi R2S, Xiaomi Mi Router 4A, etc.)
- `mips_24kc` — MIPS (MT7621, MT7620, etc.)
- `mipsel_24kc` — MIPSLE (MT7621, MT7620 little-endian)

---

## Быстрая установка через терминал / Quick Terminal Install

### OpenWRT (одной командой / single command)
```bash
echo "src/gz hydravpn_router https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/openwrt/$(. /etc/openwrt_release; echo ${DISTRIB_ARCH}_${DISTRIB_TARGET//\//_})" >> /etc/opkg/customfeeds.conf && opkg update && opkg install hydravpn-router && /etc/init.d/hydravpn-router enable && /etc/init.d/hydravpn-router start
```

### KeeneticOS Entware (одной командой / single command)

**Сначала добавьте репозиторий (один раз) / Add repository first (once):**
```bash
echo "src/gz hydravpn_keenetic https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/keenetic/" >> /opt/etc/opkg.conf && opkg update
```

**Потом установите / Then install:**
```bash
opkg install hydravpn-router && vi /opt/etc/hydravpn-router/config.yaml && /opt/etc/init.d/S99hydravpn-router enable && /opt/etc/init.d/S99hydravpn-router start
```

### MikroTik Docker (одной командой / single command)
```bash
/container config set registry-url=https://registry-1.docker.io tmpdir=disk1/pull && /interface veth add name=veth-hydravpn address=172.17.0.2/24 gateway=172.17.0.1 && /file mkdir name=disk1/hydravpn-config && /file mkdir name=disk1/hydravpn-cache && /container add name=hydravpn-router image=ghcr.io/chistovik92/hydravpn-router:1.0.1 interface=veth-hydravpn mounts=disk1/hydravpn-config:/etc/hydravpn-router,disk1/hydravpn-cache:/tmp/hydravpn-router dns=77.88.8.8,77.88.8.1 logging=yes envlist="TZ=Europe/Moscow" && /container start hydravpn-router
```

---

## Подробная установка / Detailed Installation

### OpenWRT

#### Через официальный репозиторий (Рекомендуемый) / Official Repository (Recommended)

**URL репозитория / Repository URL:**
```
https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/openwrt/<arch>/
```
где `<arch>` — ваша архитектура (см. таблицу ниже).

```bash
# 1. Добавьте репозиторий / Add repository
echo "src/gz hydravpn_router https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/openwrt/$(. /etc/openwrt_release; echo ${DISTRIB_ARCH}_${DISTRIB_TARGET//\//_})" >> /etc/opkg/customfeeds.conf

# 2. Обновите список пакетов / Update package list
opkg update

# 3. Установите пакет / Install package
opkg install hydravpn-router

# 4. Включите автозапуск / Enable autostart
/etc/init.d/hydravpn-router enable

# 5. Запустите сервис / Start service
/etc/init.d/hydravpn-router start
```

**Поддерживаемые архитектуры / Supported Architectures:**
- `x86_64` — x86_64 (Intel/AMD 64-bit)
- `aarch64_cortex-a53` — ARM64 (Raspberry Pi 4, NanoPi R4S, etc.)
- `arm_cortex-a7_neon-vfpv4` — ARMv7 (NanoPi R2S, Xiaomi Mi Router 4A, etc.)
- `mips_24kc` — MIPS (MT7621, MT7620, etc.)
- `mipsel_24kc` — MIPSLE (MT7621, MT7620 little-endian)

#### Ручная установка IPK / Manual IPK Installation

**Прямые ссылки на IPK / Direct IPK URLs:**
```
https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router_1.0.1_<arch>.ipk
```

```bash
# Определите архитектуру / Determine architecture
. /etc/openwrt_release
ARCH="${DISTRIB_ARCH}_${DISTRIB_TARGET//\//_}"

# Скачайте и установите / Download and install
cd /tmp
wget "https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router_1.0.1_${ARCH}.ipk"
opkg install "hydravpn-router_1.0.1_${ARCH}.ipk"
```

#### Настройка после установки / Post-Install Configuration

```bash
# Отредактируйте конфиг / Edit config
vi /etc/hydravpn-router/config.yaml

# Обязательные параметры / Required parameters:
# - sections[0].subscription_urls[0].url — URL вашей подписки
# - settings.source_network_interfaces — интерфейсы LAN (обычно br-lan)

# Проверьте и перезапустите / Verify and restart
hydravpn-router config && /etc/init.d/hydravpn-router restart && hydravpn-router check all
```

### KeeneticOS

#### Через Entware (Рекомендуемый) / Via Entware (Recommended)

**URL репозитория Entware / Entware Repository URL:**
```
https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/keenetic/
```

```bash
# 1. Установите Entware через веб-UI: Приложения → Entware → Установить

# 2. Подключитесь по SSH
ssh root@keenetic.local

# 3. Добавьте репозиторий HydraVPN (один раз) / Add HydraVPN repo (once)
echo "src/gz hydravpn_keenetic https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/packages/keenetic/" >> /opt/etc/opkg.conf

# 4. Обновите и установите / Update and install
opkg update && opkg install hydravpn-router

# 5. Настройте / Configure
vi /opt/etc/hydravpn-router/config.yaml

# 6. Запустите и добавьте в автозагрузку / Start and enable
/opt/etc/init.d/S99hydravpn-router enable && /opt/etc/init.d/S99hydravpn-router start
```

#### KNP пакет (Нативный) / KNP Package (Native)

**Скачать KNP / Download KNP:**
```
https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router_1.0.1.knp
```

```bash
# Скачайте и установите через веб-интерфейс:
# http://keenetic.local → Система → Компоненты → Добавить компонент
# Выберите: hydravpn-router_1.0.1.knp

# Настройка: http://keenetic.local/hydravpn-router
```

### MikroTik RouterOS

#### Docker контейнер (Рекомендуемый для RouterOS 7+) / Docker Container (Recommended)

**Docker образ / Docker Image:**
```
ghcr.io/chistovik92/hydravpn-router:1.0.1
```
(альтернативно: `docker.io/chistovik92/hydravpn-router:1.0.1`)

```bash
# Полная установка через терминал / Full terminal installation
/container config set registry-url=https://registry-1.docker.io tmpdir=disk1/pull
/interface veth add name=veth-hydravpn address=172.17.0.2/24 gateway=172.17.0.1
/file mkdir name=disk1/hydravpn-config
/file mkdir name=disk1/hydravpn-cache
/container add name=hydravpn-router image=ghcr.io/chistovik92/hydravpn-router:1.0.1 interface=veth-hydravpn mounts=disk1/hydravpn-config:/etc/hydravpn-router,disk1/hydravpn-cache:/tmp/hydravpn-router dns=77.88.8.8,77.88.8.1 logging=yes envlist="TZ=Europe/Moscow"
/container start hydravpn-router

# Настройте NAT для веб-UI / NAT for Web UI
/ip firewall nat add chain=dstnat action=dst-nat to-addresses=172.17.0.2 to-ports=8080 protocol=tcp dst-port=8080 comment="HydraVPN Web UI"
```

#### NPK пакет (Нативный) / NPK Package (Native)

**Скачать NPK / Download NPK:**
```
https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router-1.0.1.npk
```

```bash
# 1. Загрузите NPK на роутер / Upload NPK to router
# scp hydravpn-router-1.0.1.npk admin@router:/hydravpn-router-1.0.1.npk

# 2. Установите через терминал / Install via terminal
/system package install file-name=hydravpn-router-1.0.1.npk
/system reboot
```

---

## Обновление / Update

```bash
# OpenWRT (через репозиторий / via repository)
opkg update && opkg upgrade hydravpn-router && /etc/init.d/hydravpn-router restart

# KeeneticOS Entware (через репозиторий / via repository)
opkg update && opkg upgrade hydravpn-router && /opt/etc/init.d/S99hydravpn-router restart

# KeeneticOS KNP (переустановка / reinstall)
# Скачайте новый KNP: https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router_1.0.1.knp
# Система → Компоненты → Обновить / System → Components → Update

# MikroTik Docker (новый образ / new image)
/container stop hydravpn-router && /container remove hydravpn-router && /container add name=hydravpn-router image=ghcr.io/chistovik92/hydravpn-router:1.0.1 ... && /container start hydravpn-router

# MikroTik NPK (новый NPK / new NPK)
# Скачайте: https://github.com/Chistovik92/HydraVPNforRouters/releases/download/v1.0.1/hydravpn-router-1.0.1.npk
/system package install file-name=hydravpn-router-1.0.1.npk && /system reboot
```

---

## Удаление / Uninstall

```bash
# OpenWRT
/etc/init.d/hydravpn-router stop && /etc/init.d/hydravpn-router disable && opkg remove hydravpn-router && rm -rf /etc/hydravpn-router /var/run/hydravpn-router

# KeeneticOS Entware
/opt/etc/init.d/S99hydravpn-router stop && /opt/etc/init.d/S99hydravpn-router disable && opkg remove hydravpn-router && rm -rf /opt/etc/hydravpn-router /opt/var/run/hydravpn-router

# MikroTik Docker
/container stop hydravpn-router && /container remove hydravpn-router && /file remove disk1/hydravpn-config && /file remove disk1/hydravpn-cache

# MikroTik NPK
/system package uninstall hydravpn-router && /system reboot
```

---

## Диагностика / Diagnostics

```bash
# Полная проверка / Full check
hydravpn-router check all

# Статус и конфиг / Status and config
hydravpn-router status && hydravpn-router config

# Логи / Logs
logread -f -e hydravpn-router          # OpenWRT/KeeneticOS
/container logs hydravpn-router        # MikroTik Docker
/log print where topics~hydravpn       # MikroTik NPK
```

---

## Поддержка / Support

- **GitHub Issues**: https://github.com/Chistovik92/HydraVPNforRouters/issues
- **Releases**: https://github.com/Chistovik92/HydraVPNforRouters/releases
- **Telegram**: @SecretHero