# Что взято из других проектов / What was taken from other projects

Версия 1.2.5 переносит в HydraVPN for Router идеи четырёх проектов. Код этих проектов **не копировался**: всё написано заново на Go под архитектуру сервиса (sing-box + nftables), поэтому лицензии оригиналов (MIT, GPL-3.0) на этот код не распространяются. Ниже — что именно сделано и чего нет.

Version 1.2.5 brings the ideas of four projects into HydraVPN for Router. Their code was **not copied**: everything is a fresh Go implementation for this service.

## MagiTrickle — типы правил доменов / domain rule types

[MagiTrickle/MagiTrickle](https://github.com/MagiTrickle/MagiTrickle) (GPL-3.0): маршрутизация по доменам с четырьмя типами правил.

| Тип MagiTrickle | Запись в списке / `sections[].domains` | Что делает |
|---|---|---|
| Namespace | `namespace:example.com`, `example.com`, `*.example.com` | домен и все поддомены |
| Domain | `full:example.com`, `exact:example.com` | только этот домен |
| Wildcard | `wildcard:cdn*.example.?om` (или `*`/`?` в обычной записи) | `*` — любые символы, `?` — один символ |
| RegExp | `regexp:^ads?\d+\.example\.com$` | регулярное выражение (RE2) |
| — | `keyword:tracker` | любой домен со словом |

- Те же записи работают в `.lst`-списках (`community_lists`, `rule_set`), в `sections[].domains` (inline) и в `rules[]` (`domain_regex`, `domain_wildcard`).
- Отличие: MagiTrickle использует парсер `regexp2` (lookahead и т.п.), sing-box — RE2. Выражения, которые RE2 не принимает, отбрасываются при разборе списка.
- Перехват DNS-ответов и кэш «IP → домен» MagiTrickle здесь не повторён: за это отвечают FakeIP и sniffing в sing-box (`fakeip_enabled`, перенаправление DNS клиентов), а также правила по доменам прямо в sing-box.
- Вкладка **«Правила»** веб-интерфейса редактирует `domains` выбранной секции.

## DomainMapper — домены → маршруты / domains to routes

[Ground-Zerro/DomainMapper](https://github.com/Ground-Zerro/DomainMapper) (MIT): превращает домены сервисов в списки IP-адресов.

`hydravpn-router domainmap` (и вкладка **«Маппер»**, `POST /api/v1/domainmap`):

- опрашиваются **все** выбранные DNS-серверы, ответы объединяются; дубли и заглушки (`0.0.0.0`, loopback, сами DNS-серверы, частные адреса) отбрасываются;
- группировка `host` (/32), `24`, `16`, `24+32` (/24 при двух адресах в одной подсети, иначе /32); вложенные подсети схлопываются;
- `--no-cloudflare` убирает диапазоны Cloudflare (до группировки);
- форматы: `plain`, `keenetic-cli`, `keenetic-bat`, `mikrotik`, `wireguard`, `openvpn`, `nft`, `ipset`, `json`; `--split N` делит вывод на части;
- источники: файл или URL списка (`--list`, повторяется), `--domain`, секция конфига (`--section`: её `domains` и списки);
- `--exec "команда"` — запуск после записи файлов (в DomainMapper — пользовательский скрипт по завершении).

```bash
hydravpn-router domainmap --section main --dns 1.1.1.1 --dns 8.8.8.8 \
    -a 24+32 --no-cloudflare -f keenetic-cli --interface Wireguard0 -o routes.txt
```

Нет: готового каталога сервисов (`platformdb`/`dnsdb`) — используйте свои списки или `community_lists`; запуск по расписанию (используйте cron/`--exec`).

## HydraRoute — маршрутизация по доменам и CIDR на Keenetic / Keenetic split routing

[Ground-Zerro/HydraRoute](https://github.com/Ground-Zerro/HydraRoute) (MIT): Keenetic с Entware, «домены и CIDR — через туннель, остальное напрямую».

- Это основная схема проекта: секции с `domains`, списками и подсетями идут в sing-box, остальное — напрямую.
- Для Keenetic без перехвата трафика сервисом: `domainmap --apply-keenetic Wireguard0` превращает домены в **статические маршруты** через интерфейс роутера (`ndmc`: `ip route … auto`, затем `system configuration save`). `--dry-run` только печатает команды.
- Установка по ссылке на репозиторий, пакет для Entware (`hydravpn-router-keeneticos_…ipk`), хук NDM для восстановления правил — как у остальных вариантов.

Нет: собственного набора политик и конфигурации `dnsmasq`/`ipset` из HydraRoute.

## NeoFit — Keenetic, ndmc, генератор конфигов / Keenetic, ndmc, config generator

[pegakmop/neofit](https://github.com/pegakmop/neofit): панель для Xray и sing-box на Keenetic.

- **Генератор конфигов:** `hydravpn-router genconfig 'vless://…' --core xray|sing-box` (и вкладка **«Конфиг»**, `POST /api/v1/genconfig`): VLESS (Reality, TLS, WS, gRPC), VMess, Trojan, Shadowsocks → готовый конфиг с локальными SOCKS5/HTTP-входами. Hysteria2 и другие — только для sing-box.
- **ndmc:** `hydravpn-router keenetic interfaces` (список интерфейсов), `keenetic proxy` (создаёт интерфейс Proxy, направленный на локальный прокси сервиса; без `--apply` только печатает команды). Вкладка **«Keenetic»**.
- **Короткая команда:** `install.sh` создаёт `hydra` — ссылку на `hydravpn-router` (аналог `nf`).
- Веб-интерфейс работает на `api_listen` (в NeoFit — порт 92). Установка и управление пакетами opkg — через `.ipk` релиза.

Нет: установки и обновления самих Xray/Sing-Box из панели (sing-box ставится из opkg, Xray как провайдер не подключён), собственного opkg-репозитория.

## Проверка на железе / Hardware status

Команды `ndmc` (`ip route`, `interface … proxy …`, `show interface`) написаны по описанию CLI Keenetic и **не проверялись на роутере**. Поэтому `domainmap --apply-keenetic`, `keenetic proxy` и кнопки веб-интерфейса сначала показывают команды; применять их стоит после просмотра. Сообщите результат в issue.
