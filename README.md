# Local Torrent Indexer & Hybrid Prowlarr Bridge

[![Version](https://img.shields.io/badge/version-1.0.0-blue.svg)](file:///d:/Projects/Web/torrent_indexer/main.go)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](file:///d:/Projects/Web/torrent_indexer/LICENSE)

A lightweight, high-performance personal torrent indexer daemon and Web UI running on Go and embedded SQLite (FTS5).

## ✨ Features

1. **Passive DHT Crawling (KRPC + BEP 9 / BEP 10)**
   - Listens on UDP port 6881 for DHT peer announces.
   - Bootstraps with public BitTorrent router nodes (`router.bittorrent.com`, `dht.transmissionbt.com`).
   - Resolves metadata dictionaries directly from wire peers without downloading torrent payloads.
   - Automatically parses file names, total size, file count, and infers categories (Video, Audio, Software, Books, etc.).

2. **Torznab Server (Inbound)**
   - Exposes standard Torznab XML endpoints (`/api/v1/torznab`) supporting `t=caps`, `t=search`, `t=tvsearch`, and `t=movie`.
   - Allows **Prowlarr**, **Radarr**, and **Sonarr** to use your local indexed torrent collection as a standard Torznab provider.
   - Provides instant FTS5 BM25 ranked search results.

3. **Prowlarr Integration (Outbound)**
   - Configurable Prowlarr API connection to search and sync external indexers directly through your dashboard.

4. **Embedded SQLite Database with FTS5**
   - High-concurrency WAL mode with FTS5 full-text search across titles and infohashes.
   - B-tree indexing for instant sorting by date and size.

5. **Modern Web UI Dashboard**
   - Glassmorphic dark UI dashboard with real-time stats (indexed count, KRPC announces, wire resolving status).
   - Instant search with category filters and pagination.
   - File tree inspector and one-click Magnet link launcher.
   - Built-in settings modal to configure API keys, Prowlarr URL, and worker threads.

---

## 🚀 Getting Started

### 1. Run the Executable
```powershell
.\torrent-indexer.exe
```

### 2. Access Web Dashboard
Open your browser at:
[http://localhost:8080](http://localhost:8080)

### 3. Setup in Prowlarr / Radarr / Sonarr
- **Indexer Type:** Torznab (Generic)
- **URL:** `http://localhost:8080/api/v1/torznab`
- **API Key:** `local-indexer-secret-key` (or your custom key configured in Settings)
- **Categories:** 2000 (Movies), 5000 (TV), 3000 (Audio), 4000 (PC), 7000 (Books), 8000 (Other)

---

## 🛠️ Configuration (`config.json`)

The application automatically creates `config.json` with sensible defaults:
```json
{
  "web_port": 8080,
  "dht_listen_addr": ":6881",
  "dht_workers": 15,
  "max_torrent_peers": 20,
  "db_path": "indexer.db",
  "torznab_api_key": "local-indexer-secret-key",
  "prowlarr_url": "http://localhost:9696",
  "prowlarr_api_key": ""
}
```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE) - see the [LICENSE](LICENSE) file for details.
