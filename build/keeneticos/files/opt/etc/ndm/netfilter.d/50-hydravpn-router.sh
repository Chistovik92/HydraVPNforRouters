#!/bin/sh
# Installed by HydraVPN for Router. NDM passes $type (iptables|ip6tables) and $table.
[ "$type" = "ip6tables" ] && exit 0
[ "$table" = "mangle" ] || exit 0
[ -x /opt/bin/hydravpn-router ] || exit 0

STAMP=/tmp/hydravpn-router.ndm
now=$(date +%s)
last=$(cat "$STAMP" 2>/dev/null || echo 0)
[ $((now - last)) -lt 3 ] && exit 0
echo "$now" > "$STAMP"

/opt/bin/hydravpn-router reload -c /opt/etc/hydravpn-router/config.yaml --runtime-dir /opt/var/run/hydravpn-router >/dev/null 2>&1 &
exit 0
