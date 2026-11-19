package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
)

var (
	globalMu     sync.RWMutex
	globalLogger *Logger
)

// Logger coordinates disk rotation, ring buffer storage, sanitization, and crash diagnostics.
type Logger struct {
	mu           sync.RWMutex
	cfg          config.LoggingConfig
	rotator      *RotatingFile
	ringBuffer   *RingBuffer
	sanitizer    *Sanitizer
	crashHandler *CrashHandler
	level        slog.Level
	logToConsole bool
}

// Init creates and activates the application-wide logging system.
func Init(cfg config.LoggingConfig, version string) (*Logger, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	var maxBytes int64 = 10 * 1024 * 1024
	if cfg.MaxSizeMB > 0 {
		maxBytes = int64(cfg.MaxSizeMB) * 1024 * 1024
	}

	backups := 3
	if cfg.MaxBackups > 0 {
		backups = cfg.MaxBackups
	}

	filePath := cfg.FilePath
	if filePath == "" {
		filePath = "omnidex.log"
	}

	rotator, err := NewRotatingFile(filePath, maxBytes, backups)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize log rotator: %w", err)
	}

	ring := NewRingBuffer(500)
	san := NewSanitizer()

	crash, err := SetupCrashHandler(".", version)
	if err != nil {
		// Log warning to stderr, but allow app to start
		_, _ = fmt.Fprintf(os.Stderr, "[Warning] Failed to initialize crash handler: %v\n", err)
	}

	l := &Logger{
		cfg:          cfg,
		rotator:      rotator,
		ringBuffer:   ring,
		sanitizer:    san,
		crashHandler: crash,
		level:        parseSlogLevel(cfg.Level),
		logToConsole: cfg.LogToConsole,
	}

	// Route standard Go library logging through this Logger
	log.SetOutput(l)
	log.SetFlags(0) // Timestamping is handled by Logger

	globalLogger = l
	return l, nil
}

// Global returns the active global Logger instance.
func Global() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalLogger
}

func parseSlogLevel(lvl string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(lvl)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR", "FATAL":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Write processes incoming log lines from log.Print/Println/Printf.
func (l *Logger) Write(p []byte) (n int, err error) {
	n = len(p)
	raw := string(p)

	// Split by newline in case multiple lines arrive together
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		l.logLine(line)
	}

	return n, nil
}

func (l *Logger) logLine(line string) {
	sanitized := l.sanitizer.Sanitize(line)
	lvlStr := detectLogLevel(sanitized)
	lineLevel := parseSlogLevel(lvlStr)

	l.mu.RLock()
	curLevel := l.level
	toConsole := l.logToConsole
	l.mu.RUnlock()

	// Filter out messages below configured level
	if lineLevel < curLevel {
		return
	}

	now := time.Now().Format("2006/01/02 15:04:05")
	formatted := fmt.Sprintf("%s [%s] %s\n", now, lvlStr, sanitized)

	// Add to in-memory ring buffer
	l.ringBuffer.Add(lvlStr, sanitized)

	// Write to rotating file
	if l.rotator != nil {
		_, _ = l.rotator.Write([]byte(formatted))
	}

	// Write to console if enabled
	if toConsole {
		_, _ = io.WriteString(os.Stdout, formatted)
	}
}

func detectLogLevel(msg string) string {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "[error]") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic:") {
		return "ERROR"
	}
	if strings.Contains(lower, "[warning]") || strings.Contains(lower, "[warn]") || strings.Contains(lower, "notice:") {
		return "WARN"
	}
	if strings.Contains(lower, "[debug]") {
		return "DEBUG"
	}
	return "INFO"
}

// SetLevel dynamically adjusts the active logging level.
func (l *Logger) SetLevel(lvl string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = parseSlogLevel(lvl)
}

// GetLevel returns the current active log level name.
func (l *Logger) GetLevel() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	switch l.level {
	case slog.LevelDebug:
		return "DEBUG"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// SetConsoleLogging dynamically enables or disables console output.
func (l *Logger) SetConsoleLogging(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logToConsole = enabled
}

// GetConsoleLogging returns whether console output is enabled.
func (l *Logger) GetConsoleLogging() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.logToConsole
}

// RegisterSecret adds a sensitive token to the sanitizer.
func (l *Logger) RegisterSecret(secret string) {
	if l.sanitizer != nil {
		l.sanitizer.RegisterSecret(secret)
	}
}

// RingBuffer returns the active ring buffer.
func (l *Logger) RingBuffer() *RingBuffer {
	return l.ringBuffer
}

// CrashHandler returns the active crash handler.
func (l *Logger) CrashHandler() *CrashHandler {
	return l.crashHandler
}

// Close gracefully closes the log file and crash handler.
func (l *Logger) Close(cleanExit bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.crashHandler != nil {
		l.crashHandler.Close(cleanExit)
	}
	if l.rotator != nil {
		_ = l.rotator.Close()
	}
}

// RecoverAndDump is called in defer to catch panics in main() and capture diagnostics.
func (l *Logger) RecoverAndDump() {
	if r := recover(); r != nil {
		errMsg := fmt.Sprintf("FATAL APPLICATION CRASH: %v", r)
		if l.crashHandler != nil {
			l.crashHandler.CapturePanic(r)
			l.crashHandler.Close(false) // Keep crash dump file!
		}
		// Write to console and rotator
		now := time.Now().Format("2006/01/02 15:04:05")
		alert := fmt.Sprintf("\n%s [ERROR] %s\nCrash details saved to: %s\n",
			now, errMsg, l.crashHandler.GetActiveCrashPath())
		_, _ = io.WriteString(os.Stderr, alert)
		if l.rotator != nil {
			_, _ = l.rotator.Write([]byte(alert))
			_ = l.rotator.Close()
		}
		os.Exit(1)
	}
}
