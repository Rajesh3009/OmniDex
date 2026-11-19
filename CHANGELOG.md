# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.6] - 2026-11-19

### Added
- **Unified Rotating Application Logging & Forensic Crash Capture**:
  - Thread-safe, size-rotating file logger (`omnidex.log`) with automated historical backups (`omnidex.1.log`, `omnidex.2.log`, etc.) in current working directory.
  - Runtime crash hooking via `runtime/debug.SetCrashOutput` and top-level deferred panic recovery to capture full goroutine stack traces and system diagnostics to `crash-YYYYMMDD-HHMMSS.log`.
  - Automatic crash log pruning (retains latest 5 crash logs) and startup crash detection surfacing warning banners in console and Web Dashboard.
  - Automated credential sanitizer to scrub sensitive API keys and tokens from persisted logs and streams.
  - In-memory 500-entry ring buffer with REST APIs (`/api/v1/logs`, `/api/v1/logs/download`, `/api/v1/logs/crash-download`, `/api/v1/logs/clear`, `/api/v1/logs/level`).
  - Interactive "Logs" modal in Web UI dashboard with live streaming, level filtering (`ALL`, `DEBUG`, `INFO`, `WARN`, `ERROR`), text search, auto-scroll, and one-click download buttons.
  - Dedicated "Logging & Crash Diagnostics" configuration section in Settings page with dynamic in-memory level switching.

## [1.3.5] - 2026-11-17

### Security & Hardening
- **Comprehensive Vulnerability Remediation**:
  - Neutralized Stored & Reflected XSS in magnet launcher and download endpoint.
  - Eliminated attribute-context DOM XSS by converting inline event handlers to safe DOM event listeners and dataset attributes.
  - Implemented SSRF protection and outbound URL validator blocking cloud metadata and link-local addresses.
  - Added credential masking for Prowlarr and external feed API keys in configuration endpoints with tamper/overwrite protection.
  - Enforced Same-Origin CSRF protection middleware for all mutating REST APIs.
  - Upgraded Torznab credential verification to constant-time comparison (`crypto/subtle`).
  - Added safe interface binding (`web_bind_addr` default to `127.0.0.1`) and defensive HTTP security headers (CSP, nosniff, DENY).

## [1.3.4] - 2026-11-10

### Added
- **Automated High-Seed / Trending Torrent Harvester**:
  - Implemented `TrendingHarvester` engine in `internal/ingester` with scheduled background loop and manual UI trigger.
  - Added `TrendingScraper` interface and `Trending()` support for native scrapers (The Pirate Bay Top 100, YTS top movies, EZTV airing shows, Nyaa/Sukebei popular RSS, TorrentDownloads).
  - Minimum seed qualification threshold (configurable, default: >= 5 seeds) and anti-spam heuristics filter.
  - Dedicated "Trending Harvester" management tab in Web UI Settings with live status metrics, interval/seed threshold controls, and provider toggles.
  - REST API endpoints: `POST /api/v1/trending/harvest` and `GET /api/v1/trending/status`.

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

### Security
- **SSRF Prevention**: Added `ValidateOutboundURL` blocking cloud metadata endpoints (AWS, GCP, Azure), link-local addresses, and non-HTTP(S) schemes in feed ingestion and remote tracker updates.
- **Timing-Safe Torznab Authentication**: Switched API key verification to `subtle.ConstantTimeCompare` to mitigate timing attacks.
- **CSRF & Defense-in-Depth Headers**: Added security middleware enforcing `Origin`/`Referer` validation on mutable state requests, alongside `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy`.
- **Default Bind Address & Key Generation**: Changed default web bind address to `127.0.0.1` and randomized initial Torznab API keys for new installations.
- **XSS & CSP Hardening**: Replaced inline event handlers in rendered torrent rows with DOM event listeners; enforced strict `magnet:?` validation on URI redirects and magnet buttons.

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
