# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.3] - 2026-11-05

### Added
- **Native Torrent Scrapers Suite**: Implemented 8 robust native Go scrapers in `internal/scraper`:
  - AudiobookBay
  - EZTV
  - Nyaa
  - OxTorrent
  - Sukebei
  - The Pirate Bay
  - TorrentDownloads
  - YTS
- Centralized `Registry` for discovering, configuring, and executing scrapers concurrently.
- Resilient HTML, XML, and JSON parsing helpers for normalizing torrent metadata, magnet URIs, sizes, and seed/leech stats.
- Extensive test coverage including offline unit tests with mock response fixtures and multi-stage adversarial challenge test suites.

### Fixed & Optimized
- **DHT Crawler Hardening & SQLite Performance**:
  - Fast-path visited filters and index-optimized info_hash queries avoiding table scans.
  - O(1) circular ring buffer replacing reallocating cache slices.
  - Decoupled bandwidth mutex from blocking database I/O operations.
  - Atomic metrics reporting eliminating data races under heavy crawler load.
  - Semaphore-bounded concurrent query discovery goroutines.
  - Graceful phased shutdown using `sync.WaitGroup` preventing teardown panics.
- **Web UI & Settings Improvements**:
  - Fixed grid layout and container overflow on settings page across viewports.
  - Enhanced feed cards displaying sync interval, item count, and full URL tooltips.
  - Streamlined card action buttons with modern color glow highlights on hover and active states.

## [1.3.1] - 2026-11-02

### Added
- Intelligent RSS torrent size extraction from descriptions and custom XML tags.
- DHT peer metadata resolution for torrents ingested with zero size.

## [1.2.3] - 2026-10-27

### Added
- In-app toast notification system and confirmation dialog modals.
- Live static asset reloading during development.
- RSS and feed ingestion improvements.

## [1.2.0] - 2026-10-25

### Added
- Cross-platform GitHub Actions build and automated release workflow for Windows, Linux, and macOS.

## [1.0.0] - 2026-10-20

### Added
- Initial release of OmniDex: Hybrid DHT indexer and Torznab bridge daemon with embedded SQLite FTS5 search and web interface.
