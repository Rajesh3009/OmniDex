# OmniDex

OmniDex is a local hybrid DHT crawler, torrent metadata indexer, and Torznab bridge.

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
