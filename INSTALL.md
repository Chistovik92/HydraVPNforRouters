# Установка HydraVPN for Router / Installation Guide

> **Версия / Version**: 1.2.5

---

## Что публикуется в релизе / Release assets

Файлы релиза на [GitHub Releases](https://github.com/Chistovik92/HydraVPNforRouters/releases) названы **по ОС, на которую ставятся**: `openwrt`, `keeneticos`, `routeros`. Рядом лежит `checksums.txt` (SHA-256 всех файлов) и `sizes.md`.
Release files are named after the **OS they are installed on**: `openwrt`, `keeneticos`, `routeros`.

| ОС / OS | Бинарник / Binary | Пакет / Package | Архитектуры / Architectures |
|---|---|---|---|
| **OpenWrt** | `hydravpn-router-<ver>-openwrt-<arch>` | `hydravpn-router-openwrt_<ver>_<arch>.ipk` | `amd64`, `arm64`, `armv7`, `armv6`, `mipsle`, `mips`, `386`; ipk: `x86_64`, `aarch64_generic`, `aarch64_cortex-a53`, `aarch64_cortex-a72`, `arm_cortex-a7_neon-vfpv4`, `arm_cortex-a9`, `arm_cortex-a9_vfpv3-d16`, `arm_cortex-a15_neon-vfpv4`, `mipsel_24kc`, `mipsel_74kc`, `mips_24kc` |
| **KeeneticOS** (Entware) | `hydravpn-router-<ver>-keeneticos-<arch>` | `hydravpn-router-keeneticos_<ver>_<arch>.ipk` | `mipsle` (`mipsel-3.4`), `mips` (`mips-3.4`), `arm64` (`aarch64-3.10`), `armv7` (`armv7sf-3.2`), `armv5` (`armv5sf-3.2`), `amd64` (`x64-3.2`) |
| **MikroTik RouterOS 7** | `hydravpn-router-<ver>-routeros-<arch>` | `hydravpn-router-routeros-<ver>-<arch>.tar` (образ контейнера / container image) | `arm64`, `armv7` (ARM), `amd64` (x86) |

`<ver>` без буквы `v` (например `1.2.5`), тег релиза — `v1.2.5`. Бинарники одной архитектуры идентичны (статические, без зависимостей); ОС в имени говорит установщику и обновлению из веб-интерфейса, какой файл брать. MIPS-сборки используют softfloat.
`<ver>` has no `v` prefix (e.g. `1.2.5`); the tag is `v1.2.5`. Binaries of one architecture are identical static files; the OS in the name tells the installer and the web UI updater which file to take.

Релизы до 1.2.5 содержат только `hydravpn-router-<ver>-linux-<arch>`: `install.sh` и обновление из интерфейса возвращаются к ним для старых версий.

### Сколько места занимает / Disk footprint

Размеры сборки 1.2.5 (без `sing-box`, он ставится отдельно из opkg). Точные цифры релиза — в `sizes.md`.
Sizes of the 1.2.5 build (without `sing-box`, which is installed separately from opkg). Exact figures are in the release's `sizes.md`.

| Вариант установки / Install variant | Архитектура / Arch | Файл для скачивания / Download | На роутере / Installed |
|---|---|---|---|
| OpenWrt — `install.sh` (бинарник) | `aarch64` | 10.5 MiB | 10.5 MiB + конфиг |
| OpenWrt — `install.sh` (бинарник) | `armv7` | 11.0 MiB | 11.0 MiB |
| OpenWrt — `install.sh` (бинарник) | `mipsle` / `mips` | 12.4 MiB | 12.4 MiB |
| OpenWrt — `install.sh` (бинарник) | `x86_64` | 11.3 MiB | 11.3 MiB |
| OpenWrt — `.ipk` | `aarch64_*` | 4.1 MiB | 10.5 MiB |
| OpenWrt — `.ipk` | `arm_cortex-*` | 4.4 MiB | 11.0 MiB |
| OpenWrt — `.ipk` | `mipsel_*` / `mips_24kc` | 4.2 / 4.3 MiB | 12.4 MiB |
| OpenWrt — `.ipk` | `x86_64` | 4.6 MiB | 11.3 MiB |
| KeeneticOS — `install.sh` / `.ipk` | `aarch64-3.10` | 10.5 MiB / 4.1 MiB (ipk) | 10.5 MiB |
| KeeneticOS — `install.sh` / `.ipk` | `mipsel-3.4`, `mips-3.4` | 12.4 MiB / 4.2–4.3 MiB (ipk) | 12.4 MiB |
| KeeneticOS — `install.sh` / `.ipk` | `armv7sf-3.2`, `armv5sf-3.2` | 11.0–11.1 MiB / 4.4 MiB (ipk) | 11.0–11.1 MiB |
| KeeneticOS — `install.sh` / `.ipk` | `x64-3.2` | 11.3 MiB / 4.6 MiB (ipk) | 11.3 MiB |
| RouterOS — контейнер / container | `arm64`, `armv7`, `amd64` | `.tar` — см. `sizes.md` релиза | образ = `sing-box` + `nftables` + бинарник 10.5–11.3 MiB |

Что важно знать / Notes:
- Установленный размер — это почти целиком один бинарник (конфиг и скрипты — единицы КиБ). Бинарник сжат `-s -w`; дополнительно можно собрать с `UPX=1 scripts/build.sh`: файл станет заметно меньше, но выше расход ОЗУ при запуске.
- Зависимости **не входят** в эти цифры: `sing-box` (`sing-box` в OpenWrt, `sing-box-go` в Entware), `nftables`, `kmod-nft-tproxy`, `ip-full`, `ca-bundle` — несколько МиБ сверху. На роутерах с 16 МБ флеша OpenWrt ставьте на USB/extroot; на Keenetic место определяется хранилищем OPKG (`/opt`).
- `.ipk` меньше только при передаче; на роутере он распаковывается в те же 10–13 МиБ.
- Временно при обновлении нужно место под второй бинарник (проверяется перед загрузкой).

---

## Быстрая установка / Quick install

Для **каждой** ОС установка идёт командой со ссылкой на репозиторий. / Every OS installs with a command that points at the repository.

### OpenWrt и KeeneticOS (Entware) / OpenWrt and KeeneticOS (Entware)

```bash
curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
```

Без `curl` (OpenWrt) / Without `curl` (OpenWrt):

```bash
wget -qO- https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh | sh
```

Скрипт / The script:
1. определяет ОС (`openwrt` / `keeneticos`) и архитектуру / detects the OS and the architecture;
2. скачивает файл `hydravpn-router-<ver>-<ОС>-<arch>` и проверяет SHA-256 / downloads that file and verifies SHA-256;
3. ставит зависимости (OpenWrt: `sing-box`, `nftables`, `kmod-nft-tproxy`, `ip-full`; Keenetic: `sing-box-go`) / installs dependencies;
4. создаёт конфиг, только если его ещё нет / creates a config only if none exists;
5. регистрирует сервис (procd / Entware rc.func), создаёт короткую команду `hydra` и запускает сервис / registers the service, creates the short `hydra` command and starts it.

Опции / Options: `--yes` (без вопросов, для cron), `--version 1.2.5`, `--method docker`, `--help`.

| ОС | Бинарник | Конфиг | Сервис |
|---|---|---|---|
| OpenWrt | `/usr/bin/hydravpn-router` | `/etc/hydravpn-router/config.yaml` | `/etc/init.d/hydravpn-router` |
| KeeneticOS (Entware) | `/opt/bin/hydravpn-router` | `/opt/etc/hydravpn-router/config.yaml` | `/opt/etc/init.d/S99hydravpn-router` |

Другой Linux (systemd) тоже работает через `install.sh` (`/usr/local/bin`, `hydravpn-router.service`): это не роутерная ОС, поэтому берётся тот же статический бинарник, что и для OpenWrt.

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

После установки выполните проверку роутера: `hydravpn-router selftest` (`nft --check`, `sing-box check`, поддержка tproxy в ядре, хук NDM).

Ограничения (не проверены на железе, см. [ROADMAP.md](ROADMAP.md)):
- прозрачный перехват (`tproxy`) требует в ядре `nf_tables`/`nft_tproxy` или `xt_TPROXY`; если модуля нет в прошивке вашей модели, режим `singbox` работать не будет, а `zapret`/`byedpi` — по своим требованиям;
- NDM пересобирает правила Netfilter при смене состояния сети; `install.sh` ставит хук `/opt/etc/ndm/netfilter.d/50-hydravpn-router.sh`, который возвращает правила сервиса (при неизменном конфиге перезагружаются только правила firewall);
- на встроенной памяти мало места: используйте `log_level: warn` и не включайте лишние списки.

### Пакеты .ipk / .ipk packages

Для тех, кто ставит пакетным менеджером. Скачайте файл своей ОС и архитектуры (`opkg print-architecture` покажет архитектуру) и установите:

```bash
# OpenWrt
opkg install /tmp/hydravpn-router-openwrt_1.2.5_mipsel_24kc.ipk
# KeeneticOS (Entware)
opkg install /tmp/hydravpn-router-keeneticos_1.2.5_mipsel-3.4.ipk
```

Пакеты зависят от `sing-box` (OpenWrt) / `sing-box-go` (Entware) и подтянут их сами. Конфиг помечен как `conffile` и не перезаписывается при обновлении. OpenWrt 25+ (apk) пока поддерживается только через `install.sh`.

### MikroTik RouterOS 7 (container)

В RouterOS нет POSIX shell, поэтому `install.sh` там не работает. Для RouterOS есть свой установщик `install.rsc` — тоже по ссылке на репозиторий.
RouterOS has no POSIX shell, so `install.sh` does not run there. RouterOS has its own installer, `install.rsc`, also by repository link.

Требования: RouterOS 7.6+, пакет `container`, включённый режим контейнеров (`/system/device-mode/update container=yes` и подтверждение), ARM / ARM64 / x86 (на MIPS контейнеров нет).

```
/tool fetch url="https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.rsc" dst-path=hydravpn-install.rsc
/import hydravpn-install.rsc
```

Скрипт определяет архитектуру, скачивает `hydravpn-router-routeros-<ver>-<arch>.tar` из релиза, создаёт `veth`, bridge, NAT, mount с конфигом и контейнер, запускает его. Параметры задаются до `/import`:

```
:global hydraVersion "1.2.5"            # другая версия (по умолчанию — версия скрипта)
:global hydraDisk "disk1"               # где лежат образ и конфиг (по умолчанию disk1, без диска — flash)
:global hydraRouteLan "192.168.88.0/24" # сразу направить эту сеть через контейнер
```

Без `hydraRouteLan` маршрутизацию LAN в контейнер настраивают вручную: [docs/MIKROTIK.md](docs/MIKROTIK.md).

Образ можно собрать самому / Build the image yourself:

```bash
docker buildx build --platform linux/arm64 --build-arg VERSION=1.2.5 -t hydravpn-router:1.2.5 --output type=docker,dest=hydravpn-router.tar .
```

(`linux/arm/v7` для ARM32, `linux/amd64` для x86.) Если образ опубликован в реестре, вместо `file=` укажите `remote-image=`.

Не проверено на железе: контейнер работает в своей сети (`veth`), LAN-трафик нужно направить в него на стороне RouterOS.

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

Из веб-интерфейса (`api_listen`): на вкладке «Статус» карточка «Обновление» → «Обновить до X». Сервис сам скачает бинарник, проверит SHA-256 и перезапустится. В контейнере так нельзя: обновите образ.
From the web UI (`api_listen`): "Статус" tab → "Обновление" → "Обновить до X". The service downloads the binary, checks SHA-256 and restarts itself (see [docs/API.md](docs/API.md#updating-the-service)). Not in containers: pull the new image.

Из терминала / From the terminal:

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
/container stop [find comment=hydravpn]; /container remove [find comment=hydravpn]
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
