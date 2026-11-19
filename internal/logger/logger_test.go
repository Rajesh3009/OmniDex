package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local-indexer/internal/config"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Add("INFO", "message 1")
	rb.Add("WARN", "message 2")
	rb.Add("ERROR", "message 3")
	rb.Add("INFO", "message 4")

	entries := rb.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	if entries[0].Message != "message 2" || entries[2].Message != "message 4" {
		t.Errorf("unexpected buffer order: %+v", entries)
	}

	rb.Clear()
	if len(rb.Entries()) != 0 {
		t.Errorf("expected 0 entries after clear")
	}
}

func TestSanitizer(t *testing.T) {
	san := NewSanitizer()
	secret := "secret-api-token-12345"
	san.RegisterSecret(secret)

	input := "Connecting with key secret-api-token-12345 and url https://example.com?apikey=supersecret123456"
	sanitized := san.Sanitize(input)

	if strings.Contains(sanitized, secret) {
		t.Errorf("secret was not redacted: %s", sanitized)
	}
	if strings.Contains(sanitized, "supersecret123456") {
		t.Errorf("query param secret was not redacted: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result: %s", sanitized)
	}
}

func TestRotatingFile(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	// 50 bytes max, 2 backups
	rf, err := NewRotatingFile(logPath, 50, 2)
	if err != nil {
		t.Fatalf("failed to create rotator: %v", err)
	}
	defer rf.Close()

	chunk := []byte("12345678901234567890\n") // 21 bytes
	_, _ = rf.Write(chunk)
	_, _ = rf.Write(chunk)
	_, _ = rf.Write(chunk) // Should trigger rotation

	// Verify primary log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("primary log file does not exist")
	}

	backup1 := logPath + ".1"
	if _, err := os.Stat(backup1); os.IsNotExist(err) {
		t.Errorf("backup log file .1 does not exist")
	}
}

func TestCrashHandlerDetectionAndPruning(t *testing.T) {
	tempDir := t.TempDir()

	// Create 7 dummy crash files
	for i := 1; i <= 7; i++ {
		name := filepath.Join(tempDir, "crash-20261119-10000"+string(rune('0'+i))+".log")
		_ = os.WriteFile(name, []byte("dummy crash"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	ch, err := SetupCrashHandler(tempDir, "1.3.7")
	if err != nil {
		t.Fatalf("failed to setup crash handler: %v", err)
	}

	if !ch.PreviousCrashDetected {
		t.Errorf("expected previous crash to be detected")
	}

	// Should have pruned to 5 old crashes + 1 active crash
	crashes, err := scanCrashFiles(tempDir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// Total files should be 6 (5 pruned historical + 1 newly created active)
	if len(crashes) > 6 {
		t.Errorf("expected at most 6 crash files, found %d", len(crashes))
	}

	// Clean exit should remove active crash file
	ch.Close(true)

	if _, err := os.Stat(ch.GetActiveCrashPath()); !os.IsNotExist(err) {
		t.Errorf("active crash file was not cleaned up on clean exit")
	}
}

func TestLoggerIntegration(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "app.log")

	cfg := config.LoggingConfig{
		Level:        "INFO",
		FilePath:     logPath,
		MaxSizeMB:    1,
		MaxBackups:   2,
		LogToConsole: false,
	}

	l, err := Init(cfg, "1.3.7")
	if err != nil {
		t.Fatalf("failed to init logger: %v", err)
	}
	defer l.Close(true)

	l.RegisterSecret("super-secret-key")

	_, _ = l.Write([]byte("Info test with super-secret-key\n"))
	_, _ = l.Write([]byte("[Warning] Disk space low\n"))

	entries := l.RingBuffer().Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 ring buffer entries, got %d", len(entries))
	}

	if strings.Contains(entries[0].Message, "super-secret-key") {
		t.Errorf("secret not redacted from ring buffer: %s", entries[0].Message)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Errorf("expected redacted token in log file: %s", string(data))
	}
}
