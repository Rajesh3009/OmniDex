# Personal Torrent Indexer & Hybrid Prowlarr Bridge — Spec & Starter

## 1. Project Overview & Architecture

A lightweight torrent indexing daemon and Web UI tailored for a personal machine (laptop, ~12 GB RAM). It combines:
1. **Passive DHT Crawling (KRPC + BEP 9 / BEP 10):** Listens for candidate `infohash`es from incoming peer announces, resolving top-level metadata dictionaries directly from the wire.
2. **Bi-Directional Prowlarr Integration:**
   - **Outbound Client:** Queries Prowlarr's REST API (`GET /api/v1/search`) to aggregate external indexers.
   - **Inbound Torznab Server:** Exposes a standard Torznab-compatible RSS/XML endpoint (`/api/v1/torznab`) supporting `t=caps` and `t=search` so Prowlarr, Radarr, and Sonarr can query this local indexer as a generic Torznab provider.
3. **Storage & Search:** Embedded SQLite database with an `FTS5` virtual table for instant full-text search across titles.
4. **Embedded Web UI:** A single-page dashboard serving search, live stats, DHT metrics, and manual query actions.

---

## 2. Dependencies & Build Setup

### `go.mod`
```go
module local-indexer

go 1.22

require (
	[github.com/anacrolix/dht/v2](https://github.com/anacrolix/dht/v2) v2.21.1
	[github.com/anacrolix/torrent](https://github.com/anacrolix/torrent) v1.56.1
	[github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) v1.14.22
)