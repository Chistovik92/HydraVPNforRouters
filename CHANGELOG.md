# Changelog

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
