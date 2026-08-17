# indexer-torznab

MuxCore indexer that fans out through a **Torznab** (or Newznab) HTTP API — typically [Prowlarr](https://github.com/Prowlarr/Prowlarr) or Jackett — so the mesh is not tied to a single Apibay mirror.

## Configure

| Env | Purpose |
|-----|---------|
| `TORZNAB_URL` | Base Torznab API URL (e.g. `http://127.0.0.1:9696/1/api` or `http://prowlarr:9696/1`) |
| `TORZNAB_API_KEY` | Upstream API key (`apikey` query param) |
| `TORZNAB_GRPC_ADDR` | Module gRPC listen (default `:9486`) |
| `TORZNAB_NAME` | Display name in results (default `Torznab`) |
| `WG_CONF` | Required for **remote** Torznab URLs; HTTP is source-bound to the WG iface (same policy as piratebay / downloader). Loopback/empty URL OK without VPN. |

When `TORZNAB_URL` is empty the module still registers on the mesh (`indexer` capability) and returns empty search results (same soft pattern as `indexer-piratebay`).

## Multi-indexer ops

1. Run Prowlarr (or Jackett) with your trackers/indexers configured.
2. Copy a Torznab feed URL + API key into `mvp/.env` (`TORZNAB_URL`, `TORZNAB_API_KEY`).
3. Start the MVP host — `indexer-torznab` joins capability `indexer` alongside `indexer-piratebay` when that is also configured.
4. `media-automation` fans out `Search` to every discovered `indexer` peer.

Prefer Torznab for production; keep Apibay as an optional live-smoke peer.

## Build

```bash
go test ./...
go build -o indexer-torznab ./cmd/module
```
