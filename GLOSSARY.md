# OmniDex

OmniDex is a local hybrid DHT crawler, torrent metadata indexer, and Torznab bridge.

## Architecture & Core

**Core Engine**:
The root domain coordinator module (`internal/core.Engine`) that encapsulates all crawler, ingester, scraper, storage, and maintenance subsystems behind a cohesive operations interface.
_Avoid_: Core service, manager, orchestrator service, backend

## Logging & Diagnostics

**Application Log**:
The continuous operational event stream written simultaneously to standard output and rotating disk storage.
_Avoid_: Event stream, debug printout, console output

**Crash Dump**:
A self-contained forensic log generated immediately upon an unhandled panic or fatal runtime termination, containing error diagnostics, goroutine stack traces, and system resource metrics.
_Avoid_: Core dump, error log, crash report

**Crash Alert**:
A startup notification surfaced in the console and web dashboard warning the operator that an unhandled crash dump was detected from a previous run.
_Avoid_: Crash banner, warning prompt

**Log Ring Buffer**:
A fixed-capacity in-memory FIFO queue that stores recent log events for low-latency retrieval by the web dashboard.
_Avoid_: Log cache, memory buffer, log queue

**Log Sanitizer**:
A security filter that automatically masks confidential tokens and API secrets from log records prior to persistence or display.
_Avoid_: Redactor, credential scrubber

## Process Supervision & Management CLI

**Systemd Service Manager**:
The daemon supervisor adapter module (`internal/cli.SystemdServiceManager`) that registers, configures, and controls user-level systemd units with automatic crash recovery (`Restart=always`) and session persistence across logouts.
_Avoid_: Init script, process daemonizer, background supervisor

**Self-Updater**:
The automated binary release manager module (`internal/cli.Updater`) that queries GitHub releases, downloads matching OS/Arch assets, executes in-place binary swaps, and triggers healthchecked rollbacks if startup fails.
_Avoid_: Auto-updater, binary puller, release downloader

**Directory Migrator**:
The data relocation module (`internal/cli.Migrator`) that safely stops active services, coordinates SQLite WAL checkpoints, moves database and configuration files, and updates systemd unit paths.
_Avoid_: File mover, folder copier, path updater

## User Interface & Presentation

**Search & Discovery Dashboard**:
The primary catalog interface for querying, filtering, and inspecting indexed torrent releases.
_Avoid_: Home screen, torrent search engine, web GUI

**Dense Table View**:
A high-information tabular results layout optimized for desktop displays, seeder comparisons, and compact data inspection.
_Avoid_: Spreadsheet view, grid list

**Visual Card View**:
A rich, media-focused results grid featuring prominent resolution, codec, audio badges, and release tags.
_Avoid_: Poster view, thumbnail grid

**Release Group**:
An expandable UI container that aggregates multiple releases or quality encodes of the same media title into a single master entry.
_Avoid_: Duplicate cluster, release bundle, torrent folder

**Stats Ticker**:
A minimized single-line operational summary bar displayed when the full top metrics grid is collapsed to maximize vertical catalog space.
_Avoid_: Mini stats, status bar, compact header

**Focus Ring**:
A high-contrast visual highlight indicating the actively selected torrent card or row during keyboard-driven catalog navigation.
_Avoid_: Selection box, cursor indicator, active highlight

**Shortcut Modal**:
An interactive in-app cheat sheet overlay displaying all keyboard navigation bindings and hotkey triggers.
_Avoid_: Help popup, hotkey menu, keybind dialog

**Filter Drawer**:
A responsive slide-over panel that consolidates secondary filter controls on mobile viewports to preserve vertical catalog space.
_Avoid_: Filter popup, mobile menu, bottom sheet

**File Inspector**:
An enriched file-tree component inside the Torrent Details modal featuring extension-aware icons, in-modal file search, and primary media file identification.
_Avoid_: File list, contents viewer, torrent file browser
