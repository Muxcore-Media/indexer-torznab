# indexer-torznab

MuxCore indexer that fans out through a **Torznab** (or Newznab) HTTP API — typically [Prowlarr](https://github.com/Prowlarr/Prowlarr) or Jackett — so the mesh is not tied to a single Apibay mirror.

## Configure

| Env | Purpose |
|-----|---------|
| `TORZNAB_URL` | Base Torznab API URL (e.g. `http://127.0.0.1:9696/1/api` or `http://prowlarr:9696/1`) |
| `TORZNAB_API_KEY` | Upstream API key (`apikey` query param) |
| `PROWLARR_URL` | Prowlarr base URL (used when `TORZNAB_URL` is empty; searches `/api/v1/search`) |
| `PROWLARR_API_KEY` | Prowlarr API key (`X-Api-Key` header) |
| `TORZNAB_TIMEOUT` | Upstream HTTP timeout (default `90s`) |
| `TORZNAB_GRPC_ADDR` | Module gRPC listen (default `:9486`) |
| `TORZNAB_NAME` | Display name for direct Torznab mode (default `Torznab`) |
| `WG_CONF` | Required for **public** Torznab/Prowlarr URLs; HTTP is source-bound to the WG iface (same policy as piratebay / downloader). Loopback, RFC1918, `.local`, and docker-style hostnames (no dot) are exempt. |

When `TORZNAB_URL` and `PROWLARR_URL` are both empty the module still registers on the mesh (`indexer` capability) and returns empty search results (same soft pattern as `indexer-piratebay`).

## Multi-indexer ops

1. Run Prowlarr (or Jackett) with your trackers/indexers configured.
2. Copy a Torznab feed URL + API key into `mvp/.env` (`TORZNAB_URL`, `TORZNAB_API_KEY`) **or** set `PROWLARR_URL` + `PROWLARR_API_KEY`.
3. Start the MVP host — `indexer-torznab` joins capability `indexer` alongside `indexer-piratebay` when that is also configured.
4. `media-automation` fans out `Search` to every discovered `indexer` peer.

Prefer Torznab for production; keep Apibay as an optional live-smoke peer.

## Build

```bash
go test ./...
go build -o indexer-torznab ./cmd/module
make lint
```
