# Changelog

## 1.2.0

### Проверка роутера / Router verification
- `hydravpn-router selftest`: runs on the router and checks sing-box (version, `sing-box check` of the generated config), the nftables ruleset (`nft --check`), kernel TPROXY support (loaded, built-in, `/proc/config.gz`, module files), `ip rule`, IPv4 forwarding, provider binaries and the KeeneticOS setup. `--print-nft` prints the ruleset for CI; `-f json` for machines.
- KeeneticOS (Entware only): `install.sh` installs `/opt/etc/ndm/netfilter.d/50-hydravpn-router.sh`, which restores the rules after the firmware rebuilds its firewall. `reload` with an unchanged config now only re-applies the firewall rules instead of reloading sing-box.
- docs/MIKROTIK.md: RouterOS container scheme with the mangle/routing/NAT commands.
- Not yet verified on real hardware: run `hydravpn-router selftest` on your model; results decide the table of supported models.

## 1.1.0

### Оптимизация / Optimization (no behavior change)
- Building the sing-box config for a large subscription (5000 nodes with identical names, a list of 100 000 domains) took 1.8 s and 440 MB; it now takes 21 ms and 17 MB (quadratic tag numbering removed, reduced fallback configs built only when needed, large configs rendered compact). This mattered on routers with 128 MB RAM.
- `-buildvcs=false` in all builds; `UPX=1 scripts/build.sh` optionally compresses Linux binaries.
- Unused dependency (`gorilla/websocket`) dropped; `govulncheck` reports no vulnerabilities. Two indirect modules (`x/net`, `x/sys`) stay on their previous versions because their newer releases require Go 1.26.

### Обновление компонентов / Component versions (no code change)
- Container image pins sing-box **v1.14.2** (was `latest`); docs/components.tsv and the tested version (1.14) match. Xray is not run by the service: its version is tracked for the link formats only (see docs/COMPONENTS.md).

## 1.0.9

### Внешние компоненты / External components
- docs/COMPONENTS.md: step-by-step review of sing-box (1.13/1.14 changes), zapret/zapret2, ByeDPI and Xray formats: what changed, what affects the project, how updates are made.
- `docs/components.tsv` + `scripts/check-upstream.sh` compare the tested versions with the latest releases; weekly workflow `Upstream` opens an issue when a component is newer.
- The service warns in the journal when the installed sing-box is newer than the tested one (`untested_newer` in `status`).

## 1.0.8

### Приложение HydraVPN и удалённое управление / App integration
- Output of sing-box, nfqws and ciadpi is now captured line by line into the journal with the detected level (it was discarded before); the journal is readable through the API and the live stream.
- Full management through the API: sections (add, replace, delete), subscriptions, servers, `restart`, node selection; every change is audited.
- `hydravpn-router pair --host <addr>` prints a `hydravpn-router://` link (address, port, token, TLS pinning fingerprint) that the app uses to add the router.
- docs/REMOTE_ACCESS.md: comparison of ways to reach the router from the LAN and from the internet (VPN, port forward, SSH, reverse channel, Telegram) with a recommendation and the security requirements.

## 1.0.7

### Управление / Management
- Built-in management API and web UI (`api_listen`): status, journal (also as a live stream), subscriptions and servers (add, remove, refresh; saved to the config and applied at once), node selection and latency tests, diagnostics. Token authentication, address filter (private networks by default), brute-force limit, optional TLS, audit of every change. See docs/API.md. `hydravpn-router api-token` prints the token.
- Removed the unconnected code (`internal/api`, `internal/platform/*`) and the LuCI prototype that talked to a non-existent RPC.
- sing-box exposes a local-only Clash API (127.0.0.1:9090) used for node status and latency.

### Узлы и серверы / Nodes and servers
- Country detection from the node name (flag emoji or a code like `[DE]`); `include_countries` / `exclude_countries` of `urltests` work.
- `tailscale` servers become sing-box endpoints (needs a sing-box build with Tailscale support). MTProto is reported as unsupported by sing-box.
- If sing-box rejects the generated config, the service tries a config without inbound servers/endpoints first and only then a minimal one, so one unsupported feature does not switch off all sections.

## 1.0.6

### Сети и списки / Networking and lists
- Plain `.lst` lists (`community_lists[].url`, `rule_sets`) are downloaded, cached next to the config and turned into inline sing-box rules; only `.srs`/`.json` were supported before. Lists update by `interval`.
- IPv6: `enable_ipv6: true` intercepts IPv6 LAN traffic (nft `tproxy ip6`, `ip -6` policy routing, dual-stack tproxy inbound).
- FakeIP: with `fakeip_enabled` (and not `dont_touch_dhcp`) dnsmasq on OpenWrt is pointed at the sing-box DNS inbound and restored on stop.
- WAN monitoring (`enable_badwan_interface_monitoring`, `badwan_monitored_interfaces`, `badwan_reload_delay`): when a WAN comes back or changes address the firewall and sing-box are reloaded.
- Component check (`component_update_check_*`): sing-box version is compared with the latest release and the supported minimum; result in `status` (`components`). Nothing is installed automatically.

### Журнал и безопасность / Journal and safety
- `app_log_level`, `log_file`, `log_max_size_mb`, `log_keep`: leveled journal with size rotation.
- `hydravpn-router config` masks keys, tokens and subscription URLs (`--show-secrets` prints them).
- Config saves are atomic and keep a `.bak`.
- Tests for config, core, zapret, ByeDPI, lists, journal, WAN monitor.

### CI и релизы / CI and releases
- GitHub Actions: CI (vet for linux/windows, race tests, govulncheck, nft syntax check) and a release workflow that builds binaries, `.ipk` packages and the container image from a `vX.Y.Z` tag and refuses a tag that differs from `pkg/version`.
- `scripts/bump-version.sh`, `scripts/check-version.sh`; LF line endings enforced (`.gitattributes`, `.editorconfig`).

## 1.0.5

- `install.sh`: KeeneticOS without Entware is detected and the script stops with instructions (Keenetic is supported through Entware only, on a USB drive or in the built-in memory).
- INSTALL.md: Keenetic (Entware) and MikroTik container notes and known limitations.
- New ROADMAP.md.

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
