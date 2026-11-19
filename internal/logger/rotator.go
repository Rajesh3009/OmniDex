package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RotatingFile is a thread-safe, size-rotating file writer.
type RotatingFile struct {
	mu         sync.Mutex
	filePath   string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

// NewRotatingFile creates a new RotatingFile.
func NewRotatingFile(filePath string, maxBytes int64, maxBackups int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // 10MB default
	}
	if maxBackups <= 0 {
		maxBackups = 3
	}

	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	rf := &RotatingFile{
		filePath:   filePath,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
	}

	if err := rf.openCurrent(); err != nil {
		return nil, err
	}

	return rf, nil
}

func (rf *RotatingFile) openCurrent() error {
	info, err := os.Stat(rf.filePath)
	if err == nil {
		rf.size = info.Size()
		f, err := os.OpenFile(rf.filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		rf.file = f
		return nil
	}

	if os.IsNotExist(err) {
		f, err := os.OpenFile(rf.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		rf.file = f
		rf.size = 0
		return nil
	}

	return err
}

func (rf *RotatingFile) Write(p []byte) (n int, err error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	writeLen := int64(len(p))
	if rf.size+writeLen > rf.maxBytes {
		if err := rf.rotate(); err != nil {
			// If rotation fails, write to current file rather than dropping log
			_ = err
		}
	}

	if rf.file == nil {
		if err := rf.openCurrent(); err != nil {
			return 0, err
		}
	}

	n, err = rf.file.Write(p)
	rf.size += int64(n)
	return n, err
}

func (rf *RotatingFile) rotate() error {
	if rf.file != nil {
		_ = rf.file.Close()
		rf.file = nil
	}

	// Remove oldest backup if it exists: e.g. omnidex.3.log
	oldest := fmt.Sprintf("%s.%d", rf.filePath, rf.maxBackups)
	_ = os.Remove(oldest)

	// Shift backups: omnidex.2.log -> omnidex.3.log, omnidex.1.log -> omnidex.2.log
	for i := rf.maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", rf.filePath, i)
		dst := fmt.Sprintf("%s.%d", rf.filePath, i+1)
		_ = os.Rename(src, dst)
	}

	// Rename current log file: omnidex.log -> omnidex.1.log
	firstBackup := fmt.Sprintf("%s.1", rf.filePath)
	_ = os.Rename(rf.filePath, firstBackup)

	// Create new current log file
	f, err := os.OpenFile(rf.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	rf.file = f
	rf.size = 0
	return nil
}

// Close closes the underlying log file.
func (rf *RotatingFile) Close() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.file != nil {
		err := rf.file.Close()
		rf.file = nil
		return err
	}
	return nil
}
