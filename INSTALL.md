# Установка HydraVPN for Router / Installation Guide

> **Версия / Version**: 1.0.7

---

## Что публикуется в релизе / Release assets

Каждый релиз на [GitHub Releases](https://github.com/Chistovik92/HydraVPNforRouters/releases) содержит бинарники и `checksums.txt`:

| Файл / File | Устройства / Devices |
|---|---|
| `hydravpn-router-<ver>-linux-amd64` | x86_64 |
| `hydravpn-router-<ver>-linux-arm64` | ARM64 (aarch64): MT7981/MT7986, Raspberry Pi 4, Keenetic на aarch64 |
| `hydravpn-router-<ver>-linux-armv7` | ARMv7 (IPQ40xx, bcm53xx и т.п.) |
| `hydravpn-router-<ver>-linux-armv6` | ARMv6/ARMv5 |
| `hydravpn-router-<ver>-linux-mipsle` | MIPS little-endian (MT7621, MT7628, большинство Keenetic) |
| `hydravpn-router-<ver>-linux-mips` | MIPS big-endian (ath79/QCA) |
| `hydravpn-router-<ver>-linux-mips64`, `-mips64le`, `-386` | прочие / other |

`<ver>` без буквы `v` (например `1.0.7`), тег релиза — `v1.0.7`.
`<ver>` has no `v` prefix (e.g. `1.0.7`); the release tag is `v1.0.7`.

MIPS-сборки используют softfloat и работают на роутерах без FPU.
MIPS builds use softfloat and run on routers without an FPU.

---

## Быстрая установка / Quick install

### OpenWRT, KeeneticOS (Entware), Linux

```bash
curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
```

Без `curl` (OpenWRT) / Without `curl` (OpenWRT):

```bash
wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
```

Скрипт / The script:
1. определяет платформу и архитектуру / detects the platform and architecture;
2. скачивает бинарник нужной версии и проверяет SHA-256 / downloads the binary and verifies SHA-256;
3. ставит зависимости (OpenWRT: `sing-box`, `nftables`, `kmod-nft-tproxy`, `ip-full`) / installs dependencies;
4. создаёт конфиг, только если его ещё нет / creates a config only if none exists;
5. регистрирует сервис (procd / Entware rc.func / systemd) и запускает его / registers and starts the service.

Опции / Options: `--yes` (без вопросов, для cron), `--version 1.0.7`, `--method docker`, `--help`.

| Платформа | Бинарник | Конфиг | Сервис |
|---|---|---|---|
| OpenWRT | `/usr/bin/hydravpn-router` | `/etc/hydravpn-router/config.yaml` | `/etc/init.d/hydravpn-router` |
| Keenetic (Entware) | `/opt/bin/hydravpn-router` | `/opt/etc/hydravpn-router/config.yaml` | `/opt/etc/init.d/S99hydravpn-router` |
| Linux | `/usr/local/bin/hydravpn-router` | `/etc/hydravpn-router/config.yaml` | `hydravpn-router.service` |

### KeeneticOS: Entware на флешке или во встроенной памяти

Сам KeeneticOS закрыт: ставить можно только в хранилище OPKG (`/opt`). Хранилище — это USB-накопитель **или** встроенная память (на моделях, где она поддерживается). Для программы разницы нет: путь всегда `/opt`, скрипт определяет платформу по `/opt/bin/opkg`.
KeeneticOS is closed; software goes to the OPKG storage (`/opt`) on a USB drive **or** in the built-in memory (on models that support it). The program does not care: the path is always `/opt`.

1. В веб-интерфейсе роутера: «Общие настройки» → «Изменение набора компонентов» → включить «Пакеты OPKG» (и «Модули ядра для подсистемы Netfilter», если такой компонент есть на вашей модели).
2. «Приложения» / «USB-накопители»: выбрать хранилище OPKG — флешку (ext4) или встроенную память — и установить Entware по инструкции Keenetic.
3. Подключиться по SSH (порт 222 для Entware, `root`) и выполнить установку:
   ```bash
   opkg update && opkg install ca-certificates curl
   curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
   ```
   Скрипт поставит `sing-box-go` из Entware, бинарник в `/opt/bin`, конфиг `/opt/etc/hydravpn-router/config.yaml` (LAN — `br0`) и сервис `/opt/etc/init.d/S99hydravpn-router`.

Ограничения (не проверены на железе, см. [ROADMAP.md](ROADMAP.md)):
- прозрачный перехват (`tproxy`) требует в ядре `nf_tables`/`nft_tproxy` или `xt_TPROXY`; если модуля нет в прошивке вашей модели, режим `singbox` работать не будет, а `zapret`/`byedpi` — по своим требованиям;
- NDM пересобирает правила Netfilter при смене состояния сети; для устойчивости нужен хук в `/opt/etc/ndm/netfilter.d/` (запланирован);
- на встроенной памяти мало места: используйте `log_level: warn` и не включайте лишние списки.

### OpenWRT: пакет .ipk / .ipk package

Пакет собирается командой `make build-openwrt` (Docker) под архитектуры OpenWRT (`x86_64`, `aarch64_generic`, `aarch64_cortex-a53`, `arm_cortex-a7_neon-vfpv4`, `arm_cortex-a9`, `mipsel_24kc`, `mips_24kc`):

```bash
opkg install /tmp/hydravpn-router_1.0.7_mipsel_24kc.ipk
```

Архитектуру роутера покажет `opkg print-architecture`. OpenWRT 25+ (apk) пока поддерживается только через `install.sh`.

### MikroTik RouterOS 7 (container)

В RouterOS нет POSIX shell, поэтому `install.sh` там не работает. Используйте контейнер.
RouterOS has no POSIX shell, so `install.sh` does not run there. Use the container.

1. Соберите образ под архитектуру роутера / Build the image for the router architecture:
   ```bash
   docker buildx build --platform linux/arm64 -t hydravpn-router:1.0.7 --output type=docker,dest=hydravpn-router.tar .
   ```
   (`linux/arm/v7` для ARM32 / for ARM32)
2. Загрузите `hydravpn-router.tar` на роутер (`disk1/`) и выполните / Upload it and run:
   ```
   /interface veth add name=veth-hydravpn address=172.17.0.2/24 gateway=172.17.0.1
   /container mounts add name=hydravpn-config src=disk1/hydravpn-config dst=/etc/hydravpn-router
   /container add file=disk1/hydravpn-router.tar interface=veth-hydravpn mounts=hydravpn-config root-dir=disk1/hydravpn logging=yes
   /container start [find tag~"hydravpn"]
   ```

Ограничения MikroTik (не проверены на железе): контейнеры доступны на RouterOS 7.6+ с пакетом `container` и только на ARM/ARM64/x86 (не на MIPS-моделях). Контейнер работает в своей сети (`veth`), поэтому LAN-трафик нужно направить в него на стороне RouterOS (маркировка в `/ip firewall mangle` и маршрут через `172.17.0.2`), а в конфиге задать `source_network_interfaces: ["eth0"]` (интерфейс контейнера). Автоматической настройки RouterOS пока нет.

Если образ опубликован в реестре (`make docker-build && docker push ...`), вместо `file=` укажите `remote-image=`.

---

## Настройка / Configuration

```bash
vi /etc/hydravpn-router/config.yaml      # Keenetic: /opt/etc/hydravpn-router/config.yaml
```

Минимум / Minimum:
- `subscription_urls[].url` — URL подписки / subscription URL (top-level list);
- `settings.source_network_interfaces` — LAN-интерфейсы (`br-lan` на OpenWRT, `br0` на Keenetic);
- `sections[].provider` — `singbox` (по умолчанию), `zapret`, `zapret2` или `byedpi`.

Проверить и применить / Validate and apply:

```bash
hydravpn-router config                 # печатает конфиг или ошибку разбора / prints the config or a parse error
hydravpn-router reload                 # SIGHUP работающему сервису / SIGHUP to the running service
hydravpn-router check all
```

Полный пример: [configs/config.yaml](configs/config.yaml).

---

## Обновление / Update

```bash
curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/update.sh | sh
```

`update.sh --check-only` — только проверить; `--yes` — без вопросов (cron); `--force` — переустановить текущую версию. Конфиг не перезаписывается.

---

## Удаление / Uninstall

```bash
# OpenWRT
/etc/init.d/hydravpn-router stop; /etc/init.d/hydravpn-router disable
rm -f /etc/init.d/hydravpn-router /usr/bin/hydravpn-router; rm -rf /etc/hydravpn-router
# (если ставили .ipk / if installed from .ipk: opkg remove hydravpn-router)

# KeeneticOS Entware
/opt/etc/init.d/S99hydravpn-router stop
rm -f /opt/etc/init.d/S99hydravpn-router /opt/bin/hydravpn-router; rm -rf /opt/etc/hydravpn-router

# Linux
systemctl disable --now hydravpn-router
rm -f /etc/systemd/system/hydravpn-router.service /usr/local/bin/hydravpn-router; rm -rf /etc/hydravpn-router

# MikroTik
/container stop [find tag~"hydravpn"]; /container remove [find tag~"hydravpn"]
```

При остановке сервис сам удаляет свою таблицу nftables и правило `ip rule`.
On stop the service removes its nftables table and `ip rule` entry itself.

---

## Диагностика / Diagnostics

```bash
hydravpn-router check all              # все проверки / all checks
hydravpn-router status                 # состояние сервиса / service status
hydravpn-router firewall               # правила / firewall state
logread -e hydravpn-router             # OpenWRT / Keenetic
journalctl -u hydravpn-router          # Linux
```

---

## Поддержка / Support

- **GitHub Issues**: https://github.com/Chistovik92/HydraVPNforRouters/issues
- **Releases**: https://github.com/Chistovik92/HydraVPNforRouters/releases
- **Telegram**: @SecretHero
