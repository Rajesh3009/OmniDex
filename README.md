# OmniDex — Hybrid DHT Indexer & Torznab Bridge

[![Version](https://img.shields.io/badge/version-1.0.1-blue.svg)](file:///d:/Projects/Web/torrent_indexer/main.go)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](file:///d:/Projects/Web/torrent_indexer/LICENSE)

**OmniDex** is a lightweight, high-performance personal torrent indexer daemon and Web UI running on Go and embedded SQLite (FTS5).

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

### Prerequisites
- [Go](https://go.dev/dl/) 1.22 or higher
- Git (optional, to clone the repo)

> [!NOTE]
> **No C compiler or Node.js required:** OmniDex bundles all web frontend assets directly into the Go executable via `//go:embed` and utilizes a pure-Go SQLite driver (`modernc.org/sqlite`). Always set `CGO_ENABLED=0` to ensure clean, dependency-free compilation.

---

### 🔨 Building from Source

#### Option A: Using the Automated Multi-Platform Build Scripts (Recommended)

Pre-configured build scripts are included to automatically compile for **Windows**, **Linux**, and **macOS** (both AMD64 and ARM64) into the `dist/` directory with embedded assets and stripped symbols:

- **Windows (PowerShell):**
  ```powershell
  # Build all platforms (Windows, Linux, macOS AMD64 & ARM64)
  .\build.ps1

  # Or build a specific target: 'windows', 'linux', 'mac', or 'current'
  .\build.ps1 -Target linux
  .\build.ps1 -Target current
  ```

- **Linux / macOS (Bash):**
  ```bash
  chmod +x build.sh

  # Build all platforms
  ./build.sh

  # Or build a specific target:
  ./build.sh linux
  ./build.sh mac
  ./build.sh windows
  ```

- **Windows (Command Prompt):**
  ```cmd
  build.bat all
  REM Or: build.bat windows | build.bat linux | build.bat mac
  ```

All compiled binaries will be output cleanly into the `dist/` directory.

---

#### Option B: Manual Compilation

1. **Clone and download dependencies:**
   ```bash
   git clone https://github.com/Rajesh3009/OmniDex.git
   cd OmniDex
   go mod download
   ```

2. **Compile manually:**
   - **Windows (PowerShell):**
     ```powershell
     $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o omnidex.exe .
     ```
   - **Windows (CMD):**
     ```cmd
     set CGO_ENABLED=0 && go build -ldflags="-s -w" -o omnidex.exe .
     ```
   - **Linux / macOS:**
     ```bash
     CGO_ENABLED=0 go build -ldflags="-s -w" -o omnidex .
     ```

3. **Manual Cross-Compilation:**
   ```bash
   # Linux (x86_64)
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o omnidex-linux-amd64 .

   # Linux ARM64 (Raspberry Pi, AWS Graviton)
   GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o omnidex-linux-arm64 .

   # macOS Apple Silicon (M1/M2/M3/M4)
   GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o omnidex-darwin-arm64 .

   # macOS Intel (x86_64)
   GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o omnidex-darwin-amd64 .
   ```

---

### 1. Run the Executable
- **Windows:**
  ```powershell
  .\omnidex.exe
  ```
- **Linux / macOS:**
  ```bash
  chmod +x omnidex
  ./omnidex
  ```

### 2. Access Web Dashboard
Open your browser at:
[http://localhost:8686](http://localhost:8686)

### 3. Setup in Prowlarr / Radarr / Sonarr
- **Indexer Type:** Torznab (Generic)
- **URL:** `http://localhost:8686/api/v1/torznab`
- **API Key:** `local-indexer-secret-key` (or your custom key configured in Settings)
- **Categories:** 2000 (Movies), 5000 (TV), 3000 (Audio), 4000 (PC), 7000 (Books), 8000 (Other)

---

## 🛠️ Configuration (`config.json`)

The application automatically creates `config.json` with sensible defaults:
```json
{
  "web_port": 8686,
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
