# Telegram-бот и SMS / Telegram bot and SMS

С 1.3.0 роутером можно управлять из Telegram, а SMS с модема роутера (встроенного или USB, с SIM-картой) приходят в тот же чат — как в [sms2gram](https://github.com/spatiumstas/sms2gram), но внутри службы.

Бот выполняет команды через те же обработчики, что и [Management API](API.md) (внутри процесса, без сети), поэтому умеет то же, что приложение [HydraVPN](https://github.com/Chistovik92/HydraVPN) и веб-интерфейс, с теми же проверками. HTTP-API для бота включать не нужно. Соединения только исходящие: роутер сам опрашивает Telegram, наружу ничего не открывается.

## Настройка

1. Создайте бота у [@BotFather](https://t.me/BotFather) и скопируйте токен.
2. Укажите его (веб-интерфейс, приложение Hydra, `PATCH /api/v1/settings` или `config.yaml`):

   ```yaml
   settings:
     telegram_bot_token: "123456:ABC..."
     telegram_chat_ids: []        # пока пусто
     telegram_via_proxy: true     # если api.telegram.org заблокирован: через локальный прокси
     sms_forward: true            # пересылать SMS с модема
     sms_delete_forwarded: false  # удалять доставленные SMS с модема
   ```

3. Напишите боту что угодно: он ответит ID чата. Добавьте его в `telegram_chat_ids` (можно несколько: личный чат, группа). Команды выполняются только из этих чатов; токен и чаты меняются без перезапуска службы.

`telegram_via_proxy` отправляет запросы к Telegram через локальный mixed-прокси sing-box (`127.0.0.1:4534`), то есть по правилам разделов: добавьте `api.telegram.org` (или список Telegram) в раздел с VPN.

Токен бота в ответах API маскируется, в журнал не пишется.

## Команды

| Команда | Что делает |
|---|---|
| `/status` | состояние службы, версия, ошибка, доступное обновление, узлы на ядрах |
| `/nodes`, `/select <группа> <узел>`, `/test <узел>` | группы и выбранные узлы, смена узла, задержка |
| `/subs`, `/addsub <раздел> <ссылка>`, `/refresh <N>`, `/delsub <N>` | подписки (ссылку выдаёт бот «Радар» или приложение Hydra) |
| `/update`, `/update check`, `/update install [версия]` | обновление службы (и откат на версию) |
| `/cores`, `/versions <sing-box\|xray>`, `/core <sing-box\|xray> <версия>` | ядра; выбор версии sing-box и Xray, в том числе бет |
| `/reload`, `/restart` | перечитать конфиг, перезапустить службу |
| `/logs [N]` | последние предупреждения и ошибки |
| `/check <global\|dns\|singbox\|nft\|proxy>` | диагностика |
| `/set <ключ> <значение>` | изменить настройку (значение — JSON или текст); `api_*` и пути к файлам менять нельзя |
| `/sms`, `/sendsms <номер> <текст>` | последние SMS, отправка SMS с модема |

## SMS

| Роутер | Как читаются SMS | Что нужно |
|---|---|---|
| KeeneticOS (встроенный или USB-модем NDIS/QMI) | прошивка вызывает `/opt/etc/ndm/sms.d/50-hydravpn-router.sh` (ставится пакетом и `install.sh`), служба читает текст через `ndmc -c "sms <iface> list id <id>"`, удаляет и отправляет через RCI (`127.0.0.1:79/rci`) | ничего |
| OpenWrt и Linux с ModemManager | `mmcli -m any --messaging-list-sms` раз в 10 с | `modemmanager` |
| USB-модемы через smstools3 | файлы `/opt/var/spool/sms/incoming` или `/var/spool/sms/incoming` | `smstools3` |

SMS, которое не удалось доставить в Telegram (нет сети, неверный токен), остаётся на модеме. Последние 20 SMS видны в `/sms` и в `GET /api/v1/sms`; отправка — `POST /api/v1/sms` `{"to":"+7…","text":"…"}`.

**Не проверено на железе:** формат вывода `ndmc` и RCI-команды удаления/отправки взяты из sms2gram; ModemManager — из документации mmcli. Сообщайте о расхождениях.
