# Changelog

## [0.1.2] — 2026-08-18

### Fixed
- Prefer Torznab `magneturl` over enclosure so search hits expose infohashes instead of opaque proxy download URLs.

## [0.1.1] — 2026-08-18

### Fixed
- Default Prowlarr/Torznab HTTP timeout 25s → 90s (override with `TORZNAB_TIMEOUT`) so a fan-out across many public indexers is not canceled while awaiting headers.
- Honor `PROWLARR_URL` + `PROWLARR_API_KEY` and search Prowlarr's `/api/v1/search` JSON API (vault systemd does not set `TORZNAB_URL`).

## Unreleased

- Remote Torznab HTTP requires readable `WG_CONF` and source-binds to the WG iface (loopback/empty exempt)

## [0.1.0] — 2026-08-09

- Initial Torznab/Newznab indexer module (`IndexerService`).
- Soft-empty when `TORZNAB_URL` unset; optional `TORZNAB_API_KEY`.
