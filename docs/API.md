# Management API

JSON over HTTP(S). Enable it with `settings.api_listen` (for example `192.168.1.1:8088`). The same server serves the web UI at `/`.

## Access

- **Token.** Every `/api/v1/*` request needs `Authorization: Bearer <token>`. The token is `settings.api_token`, or, if empty, a random one generated on first start and stored in `<runtime-dir>/api-token` (mode 0600). Print it on the router with `hydravpn-router api-token`.
- **Addresses.** Only loopback and private networks may connect. Change it with `settings.api_allow` (list of IPs/CIDRs).
- **Brute force.** After 10 wrong tokens within a minute an address gets `429` for a minute.
- **TLS.** Set `api_tls_cert` and `api_tls_key` for HTTPS. Without TLS on a non-loopback address the service logs a warning: the token travels in clear text. See [REMOTE_ACCESS.md](REMOTE_ACCESS.md).
- **Audit.** Every request that changes state and every denied request is written to the journal (`[api] 192.168.1.20 POST /api/v1/subscriptions -> 201`). Tokens and subscription URLs never appear in it.
- Secrets in responses (`/config`, `/subscriptions`, `/servers`) are masked.

Errors: `{"error": "text"}` with a 4xx/5xx status.

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/version` | version and commit |
| GET | `/api/v1/status` | service, providers, DNS, firewall, subscriptions, lists, component versions |
| GET | `/api/v1/config` | configuration with masked secrets |
| POST | `/api/v1/reload` | re-read the config file and apply it |
| GET | `/api/v1/logs?n=200&level=info` | last journal entries |
| GET | `/api/v1/logs/stream` | server-sent events with new entries (`?token=` is accepted here, EventSource cannot send headers) |
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

Changes are written to the config file atomically; the previous file is kept as `config.yaml.bak`. Comments of a hand-written file are lost on the first change made through the API.

## Example

```bash
TOKEN=$(hydravpn-router api-token)
curl -H "Authorization: Bearer $TOKEN" http://192.168.1.1:8088/api/v1/status
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"section":"main","url":"https://panel.example/sub/xyz"}' \
     http://192.168.1.1:8088/api/v1/subscriptions
```
