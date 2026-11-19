package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// CrashHandler manages active Go crash output and prior crash log detection.
type CrashHandler struct {
	mu                     sync.Mutex
	dir                    string
	version                string
	activeFile             *os.File
	activePath             string
	PreviousCrashDetected  bool
	LatestCrashFile        string
	CleanExited            bool
}

// SetupCrashHandler checks for prior crash dumps, prunes old ones, and installs Go runtime crash output.
func SetupCrashHandler(dir, version string) (*CrashHandler, error) {
	if dir == "" {
		dir = "."
	}

	handler := &CrashHandler{
		dir:     dir,
		version: version,
	}

	// 1. Scan and detect previous crashes
	crashes, err := scanCrashFiles(dir)
	if err == nil && len(crashes) > 0 {
		handler.PreviousCrashDetected = true
		handler.LatestCrashFile = crashes[0]

		// 2. Prune old crash files (keep at most 5 latest)
		if len(crashes) > 5 {
			for _, old := range crashes[5:] {
				_ = os.Remove(filepath.Join(dir, old))
			}
		}
	}

	// 3. Create active crash dump for the current process session
	now := time.Now().Format("20060102-150405")
	activeFilename := fmt.Sprintf("crash-%s.log", now)
	activePath := filepath.Join(dir, activeFilename)

	f, err := os.OpenFile(activePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return handler, fmt.Errorf("failed to create crash dump file %s: %w", activePath, err)
	}

	handler.activeFile = f
	handler.activePath = activePath

	// Write forensic header
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	header := fmt.Sprintf(
		"================================================================================\n"+
			"OMNIDEX FORENSIC CRASH DUMP\n"+
			"Started:     %s\n"+
			"Version:     %s\n"+
			"OS/Arch:     %s/%s\n"+
			"Go Runtime:  %s\n"+
			"Process PID: %d\n"+
			"Goroutines:  %d\n"+
			"Alloc Heap:  %d MB (Sys: %d MB)\n"+
			"================================================================================\n\n",
		time.Now().Format(time.RFC3339),
		version,
		runtime.GOOS,
		runtime.GOARCH,
		runtime.Version(),
		os.Getpid(),
		runtime.NumGoroutine(),
		mem.Alloc/(1024*1024),
		mem.Sys/(1024*1024),
	)
	_, _ = f.WriteString(header)

	// Direct Go runtime unhandled panic/crash stack dumps directly to active crash file
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})

	return handler, nil
}

// scanCrashFiles returns crash filenames sorted newest first.
func scanCrashFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type crashItem struct {
		name    string
		modTime time.Time
	}

	var crashes []crashItem
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "crash-") && strings.HasSuffix(name, ".log") {
			info, err := entry.Info()
			if err == nil {
				crashes = append(crashes, crashItem{
					name:    name,
					modTime: info.ModTime(),
				})
			}
		}
	}

	// Sort newest first
	sort.Slice(crashes, func(i, j int) bool {
		return crashes[i].modTime.After(crashes[j].modTime)
	})

	names := make([]string, len(crashes))
	for i, c := range crashes {
		names[i] = c.name
	}
	return names, nil
}

// CapturePanic writes detailed panic diagnostics to the active crash file and syncs it to disk.
func (ch *CrashHandler) CapturePanic(r any) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.activeFile == nil {
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	panicDetails := fmt.Sprintf(
		"\n[FATAL CRASH DETECTED]\n"+
			"Crash Time:  %s\n"+
			"Panic Error: %v\n"+
			"Goroutines:  %d\n"+
			"Heap In Use: %d MB\n\n"+
			"STACK TRACE:\n%s\n",
		time.Now().Format(time.RFC3339),
		r,
		runtime.NumGoroutine(),
		mem.HeapInuse/(1024*1024),
		string(debug.Stack()),
	)

	_, _ = ch.activeFile.WriteString(panicDetails)
	_ = ch.activeFile.Sync()
}

// Close terminates crash logging. If cleanExit is true, the unused crash dump is removed from disk.
func (ch *CrashHandler) Close(cleanExit bool) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.CleanExited = cleanExit
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})

	if ch.activeFile != nil {
		path := ch.activePath
		_ = ch.activeFile.Sync()
		_ = ch.activeFile.Close()
		ch.activeFile = nil

		if cleanExit && path != "" {
			_ = os.Remove(path)
		}
	}
}

// GetActiveCrashPath returns the path of the current active crash dump file.
func (ch *CrashHandler) GetActiveCrashPath() string {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.activePath
}
