# Changelog

## 1.0.4

### Рабочий режим / Real-world routing
- The sing-box config now contains what the settings describe: proxy nodes from subscriptions, `selector` + `urltest` per section, routing rules from `sections`, `rules`, `community_lists`, `rule_set` and `fully_routed_ips`, `block`/`bypass` actions, ByeDPI sections routed to the local SOCKS port, inbound `servers` (VLESS/Reality or raw `inbound_json`). In 1.0.3 subscriptions were downloaded but never used, so no traffic was proxied.
- sing-box is reloaded automatically when subscription nodes change; the config is validated with `sing-box check` before it is applied, and a minimal config without sections is used if it is rejected.
- Subscriptions: nodes are cached next to the config (survives reboot, works offline), full sing-box JSON and legacy `ss://` are parsed, gRPC/ALPN/insecure options, `prefix_nodes`, `auto_hwid` (`x-hwid`), `Subscription-Userinfo` quota in `subs` status. Subscription URLs (tokens) are redacted in logs and status output.
- Config validation: unique section names, known actions, subscriptions must reference an existing section and an http(s) URL.

### Исправления / Fixes
- Version drift: the `v1.0.3` tag pointed at a commit whose source still said 1.0.1, so builds from the tag reported 1.0.1. A test now fails if the version in the code, web UI, INSTALL.md, CHANGELOG or scripts differs.
- Firewall: `tproxy` after `meta l4proto { tcp, udp }` is rejected by nft; tcp and udp now have separate rules.
- Reload applies subscriptions before providers, so sing-box is rebuilt from the new sections.
- Removed unused code and renamed internal helpers to `ConfigFromSettings`.

### Планы / Roadmap
- See [ROADMAP.md](ROADMAP.md). 2.0.0: connect to an external Telegram bot (Radar) and receive subscriptions from it.

## 1.0.3

### Установка и обновление / Install & update
- `install.sh` works on a clean system (no longer exits under `set -e`), asks via `/dev/tty` with `curl | sh`, downloads release assets by their real names (`hydravpn-router-<ver>-linux-<arch>`) and verifies SHA-256.
- `update.sh` uses `install.sh --update`; versions are compared numerically, so updates are detected correctly.
- MIPS endianness is detected on the router (MT7621 gets `mipsle`).
- A single version source: `pkg/version/version.go` (Makefile, build.sh, Dockerfiles read it).

### Сборка / Build
- `scripts/build.sh` builds `cmd/hydravpn-router` with the correct module path; MIPS builds use softfloat.
- `build/openwrt/Dockerfile` builds a per-architecture `.ipk` without the OpenWrt SDK.
- New root `Dockerfile` for Docker hosts and MikroTik containers.

### Сервис / Service
- `start` handles SIGTERM/SIGINT (cleans up firewall rules) and SIGHUP (reloads config); `stop`, `reload`, `status`, `providers`, `dns`, `firewall`, `subs`, `check` work against the running service.
- Firewall: atomic nftables ruleset with tproxy to sing-box and policy routing via table 105 (the non-existent `tun0` route and the broken DNS redirect are gone); rules are removed on stop and not duplicated on reload.
- sing-box config uses the 1.12+ format (validated with sing-box 1.14).
- Fixed deadlocks in DNS and subscription managers on stop, the infinite recursion of `check global`, subscriptions not being fetched after start, base64 subscription decoding, node names, TLS verification for subscriptions.
- Providers are enabled per section (`provider: singbox|zapret|zapret2|byedpi`), use real nfqws/nfqws2/ciadpi options and are restarted if they crash.
- Dependencies updated (fixes GO-2026-6278 in gorilla/websocket).
