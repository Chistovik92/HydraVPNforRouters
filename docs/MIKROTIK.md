# MikroTik RouterOS: контейнер и направление трафика в него

> **Статус: не проверено на реальном роутере.** Ниже — схема и команды, которые следуют из устройства RouterOS 7 и из того, как сервис перехватывает трафик. Перед использованием выполните проверку из раздела «Проверка».

## Установка одной командой

```
/tool fetch url="https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.rsc" dst-path=hydravpn-install.rsc
/import hydravpn-install.rsc
```

Скрипт `scripts/install.rsc` определяет архитектуру, скачивает `hydravpn-router-routeros-<ver>-<arm64|armv7|amd64>.tar`, создаёт `veth`, bridge, NAT, mount и контейнер и запускает его. Перед `/import` можно задать `:global hydraRouteLan "192.168.88.0/24"` — тогда будут добавлены таблица маршрутизации и правило mangle из раздела ниже. Команды ниже — то, что скрипт делает; их можно выполнить вручную.

## Требования

- RouterOS **7.6+** с пакетом `container`, режим устройства с включёнными контейнерами (`/system/device-mode/update container=yes` + подтверждение кнопкой/перезагрузкой).
- Архитектура **ARM, ARM64 или x86**. Контейнеры не работают на MIPS-моделях.
- Внешний диск/USB или достаточный объём внутренней памяти под образ.

## Как это устроено

Контейнер живёт в своей сети (`veth`), а не в сети роутера. Сервис внутри контейнера перехватывает только тот трафик, который пришёл на его интерфейс. Поэтому LAN-трафик нужно **направить в контейнер средствами RouterOS** (маркировка + маршрут через контейнер), а внутри контейнера перехватывать всё, что пришло с `eth0`:

```
клиент LAN → RouterOS (mangle: метка маршрута) → veth → контейнер (tproxy → sing-box) → veth → RouterOS (NAT) → интернет
```

## Настройка RouterOS

Адреса подберите под свою сеть (пример: LAN `192.168.88.0/24`, контейнер `172.17.0.2`).

```
# сеть контейнера
/interface veth add name=veth-hydra address=172.17.0.2/24 gateway=172.17.0.1
/interface bridge add name=br-containers
/ip address add address=172.17.0.1/24 interface=br-containers
/interface bridge port add bridge=br-containers interface=veth-hydra

# NAT для трафика контейнера
/ip firewall nat add chain=srcnat src-address=172.17.0.0/24 action=masquerade comment="hydravpn container"

# отдельная таблица маршрутизации через контейнер
/routing table add name=via-hydra fib
/ip route add dst-address=0.0.0.0/0 gateway=172.17.0.2 routing-table=via-hydra

# чьи клиенты идут через контейнер
/ip firewall address-list add list=hydra-clients address=192.168.88.0/24
/ip firewall mangle add chain=prerouting src-address-list=hydra-clients \
    dst-address-type=!local in-interface-list=LAN \
    action=mark-routing new-routing-mark=via-hydra passthrough=no comment="hydravpn: clients via container"
```

Контейнер:

```
/container mounts add name=hydravpn-config src=disk1/hydravpn-config dst=/etc/hydravpn-router
/container add file=disk1/hydravpn-router.tar interface=veth-hydra mounts=hydravpn-config \
    root-dir=disk1/hydravpn logging=yes start-on-boot=yes
/container start [find tag~"hydravpn"]
```

Конфиг сервиса в контейнере (`disk1/hydravpn-config/config.yaml`): перехватывается трафик, пришедший на интерфейс контейнера.

```yaml
settings:
  source_network_interfaces: ["eth0"]
  api_listen: "172.17.0.2:8088"      # управление из LAN; порт можно открыть dst-nat'ом
```

## Проверка (после запуска контейнера)

В консоли контейнера (`/container shell`) выполните `hydravpn-router selftest`. Обязательно должны быть `PASS` для `sing-box config`, `nft` и `kernel tproxy`. Если `kernel tproxy` — `FAIL`: ядро RouterOS не содержит `nft_tproxy`/`xt_TPROXY`, секции с `provider: singbox` работать не будут (этот вариант тогда не подходит для вашей модели). Сообщите результат в issue: он попадёт в таблицу проверенных моделей.

## Ограничения

- Автоматической настройки RouterOS нет: команды выше выполняются вручную.
- Трафик самого роутера (не клиентов) через контейнер не идёт.
- Контейнер не видит реальные интерфейсы роутера; мониторинг WAN (`enable_badwan_interface_monitoring`) внутри контейнера не применим.
