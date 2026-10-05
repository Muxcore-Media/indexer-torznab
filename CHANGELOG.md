# Changelog

## [0.1.8] - 2026-10-05

### Changed
- Built on core v0.6.14 / sdk/go/module v0.6.4: unregisters on shutdown and re-registers after core restarts (ADR-0022).

## [0.1.7] - 2026-10-05


### Changed

- Reported version comes from muxcore.json (ADR-0021); built on core v0.6.12 / sdk/go/module v0.6.3 (mesh enrollment, ADR-0017).

## [0.1.5] - 2026-10-05

### Changed
- CI runs on GitHub-hosted runners from the umbrella template; retired-origin workflows removed.
- Dependencies resolve from published GitHub tags (no filesystem `replace`); requires core v0.6.0.

## [0.1.6] — 2026-09-09

### Added
- Prowlarr-mode `GetIndexer` / `CreateIndexer` / `UpdateIndexer` / `DeleteIndexer` for Torznab/Newznab feeds.
- Direct Torznab/Jackett returns `FailedPrecondition` (single-feed `TORZNAB_URL`, not a household registry).

## [0.1.5] — 2026-08-31

### Added
- RFC1918, link-local, `.local`, and docker-style hostnames exempt from `WG_CONF` VPN gate.
- Prowlarr `ListIndexers` via `/api/v1/indexer`; `SearchRequest.indexer_ids` forwarded as `indexerIds`.
- `GetCapabilities` driven from Torznab `t=caps` XML; unconfigured module does not advertise search.
- `Health` probes upstream (`t=caps` or Prowlarr `/api/v1/system/status`).
- Music/book/audiobook search types and `year` query param.
- HTTP 429 and Newznab `<error>` mapped to gRPC `ResourceExhausted`.
- `make lint`, golangci-lint CI, and Dockerfile (`EXPOSE 9486`).

### Fixed
- Strip `apikey` (and equivalent) from result download/info URLs before cache or automation history.
- Prowlarr release mapping: child indexer name/id, IMDB/TMDB/TVDB, protocol, category; forward search offset.
- Docs/metadata drift (`PROWLARR_*`, `TORZNAB_TIMEOUT`, contract v1, no bogus `HTTPAddr`).

### Security
- Remote Torznab HTTP requires readable `WG_CONF` and source-binds to the WG iface (public hostnames only).

## [0.1.4] — 2026-08-20

### Added
- Mesh StorageService cache: search hits with a btih are written to `torrent/{infohash}/index.json` when `MUXCORE_GRPC_ADDR` is set (S3/local backends share the same key space as the downloader piece store).

## [0.1.3] — 2026-08-18

### Fixed
- Prefer a magnet in the item `link` (or enclosure) over an HTTP proxy download URL when `magneturl` is absent, so search hits still expose an infohash.

## [0.1.2] — 2026-08-18

### Fixed
- Prefer Torznab `magneturl` over enclosure so search hits expose infohashes instead of opaque proxy download URLs.

## [0.1.1] — 2026-08-18

### Fixed
- Default Prowlarr/Torznab HTTP timeout 25s → 90s (override with `TORZNAB_TIMEOUT`) so a fan-out across many public indexers is not canceled while awaiting headers.
- Honor `PROWLARR_URL` + `PROWLARR_API_KEY` and search Prowlarr's `/api/v1/search` JSON API (vault systemd does not set `TORZNAB_URL`).

## [0.1.0] — 2026-08-09

- Initial Torznab/Newznab indexer module (`IndexerService`).
- Soft-empty when `TORZNAB_URL` unset; optional `TORZNAB_API_KEY`.
