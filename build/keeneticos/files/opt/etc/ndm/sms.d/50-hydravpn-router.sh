#!/bin/sh
# Installed by HydraVPN for Router. KeeneticOS runs sms.d hooks for every new
# SMS with $interface_id and $message_id; the service reads the text with
# ndmc and forwards it to Telegram (sms_forward in config.yaml).
[ -n "$interface_id" ] && [ -n "$message_id" ] || exit 0
case "$interface_id$message_id" in
    *[!A-Za-z0-9._-]*) exit 0 ;;
esac
dir=/opt/var/run/hydravpn-router/sms
mkdir -p "$dir" || exit 0
printf '%s %s\n' "$interface_id" "$message_id" > "$dir/$interface_id-$message_id"
exit 0
