# Changelog

## Unreleased

- Remote Torznab HTTP requires readable `WG_CONF` and source-binds to the WG iface (loopback/empty exempt)

## [0.1.0] — 2026-08-09

- Initial Torznab/Newznab indexer module (`IndexerService`).
- Soft-empty when `TORZNAB_URL` unset; optional `TORZNAB_API_KEY`.
