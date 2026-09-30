# Management API

JSON over HTTP(S). Enable it with `settings.api_listen` (for example `192.168.1.1:8088`). The same server serves the web UI at `/`.

## Access

- **Token.** Every `/api/v1/*` request needs `Authorization: Bearer <token>`. The token is `settings.api_token`, or, if empty, a random one generated on first start and stored in `<runtime-dir>/api-token` (mode 0600). Print it on the router with `hydravpn-router api-token`.
- **Addresses.** Only loopback and private networks may connect. Change it with `settings.api_allow` (list of IPs/CIDRs).
- **Wrong tokens** are slowed down, not blocked: each recent failure from an address adds a second of delay (up to five) and at most four such requests wait at once (more get `429`). A request with the **right token is never delayed or refused**, so another device in the LAN cannot lock the app out. A configured `api_token` must be at least 16 characters.
- **TLS.** Set `api_tls_cert` and `api_tls_key` for HTTPS. Without TLS on a non-loopback address the service logs a warning: the token travels in clear text. See [REMOTE_ACCESS.md](REMOTE_ACCESS.md).
- **Audit.** Every request that changes state and every denied request is written to the journal (`[api] 192.168.1.20 POST /api/v1/subscriptions -> 201`). Tokens and subscription URLs never appear in it.
- Secrets in responses (`/config`, `/subscriptions`, `/servers`) are masked.

Errors: `{"error": "text"}` with a 4xx/5xx status.

Changes that need the service to restart parts of itself (reload, subscription or section changes) answer within 25 seconds. If applying takes longer the change is already saved and the answer is `202 {"status":"applying"}`; the work continues in the background. `GET /api/v1/status` never waits for it: while the service is busy it returns the last snapshot with `"busy": true`.

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/version` | version and commit |
| GET | `/api/v1/status` | service, providers, DNS, firewall, subscriptions, lists, component versions |
| GET | `/api/v1/config` | configuration with masked secrets |
| POST | `/api/v1/reload` | re-read the config file and apply it |
| POST | `/api/v1/restart` | stop and start all components (`202`) |
| GET/POST | `/api/v1/sections` | list / add sections |
| PUT/DELETE | `/api/v1/sections/{name}` | replace / delete a section (a section used by a subscription cannot be deleted) |
| GET | `/api/v1/logs?n=200&level=info` | last journal entries |
| GET | `/api/v1/logs/stream` | server-sent events with new entries (the only path that accepts `?token=`, because EventSource cannot send headers) |
| GET | `/api/v1/subscriptions` | subscriptions (`index` identifies one) |
| POST | `/api/v1/subscriptions` | add `{"section":"main","url":"https://…"}`; saved to the config, applied at once |
| POST | `/api/v1/subscriptions/{index}/refresh` | fetch now |
| DELETE | `/api/v1/subscriptions/{index}` | remove |
| GET | `/api/v1/servers` | inbound servers |
| POST | `/api/v1/servers` | add a server (same fields as `servers:` in the config) |
| DELETE | `/api/v1/servers/{name}` | remove |
| GET | `/api/v1/nodes` | selector / url-test groups and proxies with the last latency and country |
| POST | `/api/v1/nodes/select` | `{"group":"main","node":"Node 1"}` |
| POST | `/api/v1/nodes/test` | `{"node":"Node 1"}` → `{"delay_ms":123}` |
| GET | `/api/v1/check/{name}` | diagnostics: `global`, `dns`, `singbox`, `nft`, `proxy`, … |
| GET | `/api/v1/radar` | link to a Radar bot account: `{"linked":true,"server":"…","username":"…"}` (the token is never returned) |
| POST | `/api/v1/radar/link` | `{"server":"radar.example.org","code":"12345678","section":"main"}`: exchange the code, then sync (`201`) |
| POST | `/api/v1/radar/sync` | `{"section":"main"}` (optional): fetch the subscriptions and add the new ones |
| DELETE | `/api/v1/radar` | disconnect the device in the bot and forget the token |

Changes are written to the config file atomically, and only the changed list item is edited: comments, key order and two-space indentation of a hand-written file stay. Blank lines between blocks are dropped by the YAML library and a commented-out example below a list may move behind the new items. The previous file is kept as `config.yaml.bak`; a change that makes the config invalid is refused and nothing is written.

## Example

```bash
TOKEN=$(hydravpn-router api-token)
curl -H "Authorization: Bearer $TOKEN" http://192.168.1.1:8088/api/v1/status
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"section":"main","url":"https://panel.example/sub/xyz"}' \
     http://192.168.1.1:8088/api/v1/subscriptions
```

## Radar bot account

The router can sign in to a person's account in the Radar bot and take the subscriptions issued to them (bot API for apps, bot 5.9.1; the contract is `docs/API_APPS.md` in github.com/Chistovik92/radar).

1. In the bot: "VPN" → "Connect an app". The bot shows a one-time code (8 digits, 5 minutes).
2. `POST /api/v1/radar/link` with the bot address and the code (or `hydravpn-router radar link`). The router exchanges the code for a device token and stores it in `radar.json` in the config directory (mode 0600; not in the runtime dir, which is tmpfs on OpenWrt and is emptied at reboot). The token is never returned by the API and never logged.
3. Every working **subscription link** of the person is added to the section (`main`, else the first; `section` overrides) with automatic user agent and HWID and a 24 h refresh. Single keys (Outline) and config files (wg-easy) are skipped. A link already in the config, in any section, is not added twice.
4. A linked router re-reads the bot every 12 hours. Subscriptions that disappeared from the bot are **not** removed from the config.
5. If the device is disconnected in the bot, the next sync answers `409` and the link is dropped: a new code is needed.

Answers: `201`/`200` with `{"status":…,"result":{"added":1,"present":0,"skipped":1,"section":"main"}}`; `409` not linked or disconnected in the bot; `422` the bot does not accept the code (a bot `401` is not passed through: here it would read as a wrong router token); `429` too many attempts; `502` the bot is unreachable. The bot address must be `https`; plain `http` is accepted only for localhost and private networks, and redirects are not followed, so the token cannot be carried to another address.

```bash
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json'      -d '{"server":"radar.example.org","code":"12345678"}'      http://192.168.1.1:8088/api/v1/radar/link
```

