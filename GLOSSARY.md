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
