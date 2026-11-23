package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// MockCommandRunner records executed commands and returns programmed responses.
type MockCommandRunner struct {
	mu       sync.Mutex
	Commands [][]string
	Outputs  map[string]string
	Errors   map[string]error
}

func NewMockCommandRunner() *MockCommandRunner {
	return &MockCommandRunner{
		Outputs: make(map[string]string),
		Errors:  make(map[string]error),
	}
}

func (m *MockCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := append([]string{name}, args...)
	m.Commands = append(m.Commands, cmd)

	key := strings.Join(cmd, " ")
	for k, out := range m.Outputs {
		if strings.Contains(key, k) {
			if err, ok := m.Errors[k]; ok {
				return out, err
			}
			return out, nil
		}
	}

	if err, ok := m.Errors[key]; ok {
		return "", err
	}
	return "ok", nil
}

func TestCLI_SubcommandDispatch(t *testing.T) {
	runner := NewMockCommandRunner()
	var out, errOut bytes.Buffer

	c := &CLI{
		Version: "1.3.9",
		Runner:  runner,
		Out:     &out,
		ErrOut:  &errOut,
	}

	// 1. Version command
	code := c.Execute(context.Background(), []string{"version"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(out.String(), "v1.3.9") {
		t.Errorf("expected version output, got: %s", out.String())
	}

	// 2. Help command
	out.Reset()
	code = c.Execute(context.Background(), []string{"help"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected usage output, got: %s", out.String())
	}

	// 3. Unknown command
	out.Reset()
	errOut.Reset()
	code = c.Execute(context.Background(), []string{"unknown-cmd"})
	if code != 1 {
		t.Fatalf("expected code 1 for unknown command, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Unknown command") {
		t.Errorf("expected error for unknown command, got: %s", errOut.String())
	}
}

func TestServiceManager_InstallAndUninstall(t *testing.T) {
	tempHome := t.TempDir()
	runner := NewMockCommandRunner()

	mgr := &SystemdServiceManager{
		runner:  runner,
		homeDir: tempHome,
	}

	// Mock environment so Install runs even on non-linux OS during test
	t.Setenv("OMNIDEX_MOCK_SYSTEMD", "1")

	workDir := filepath.Join(tempHome, "data")
	_ = os.MkdirAll(workDir, 0755)
	execPath := filepath.Join(tempHome, "omnidex")
	_ = os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0755)

	err := mgr.Install(context.Background(), workDir, execPath)
	if err != nil {
		t.Fatalf("failed to install service: %v", err)
	}

	unitPath := mgr.UnitPath()
	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("unit file not created: %v", err)
	}

	if !strings.Contains(string(content), "WorkingDirectory="+workDir) {
		t.Errorf("expected working directory in unit, got: %s", string(content))
	}
	if !strings.Contains(string(content), "ExecStart="+execPath) {
		t.Errorf("expected ExecStart in unit, got: %s", string(content))
	}
	if !strings.Contains(string(content), "Restart=always") {
		t.Errorf("expected Restart=always in unit, got: %s", string(content))
	}

	// Verify systemctl commands called
	runner.mu.Lock()
	cmds := runner.Commands
	runner.mu.Unlock()

	foundDaemonReload := false
	foundEnable := false
	foundRestart := false
	for _, cmd := range cmds {
		full := strings.Join(cmd, " ")
		if strings.Contains(full, "systemctl --user daemon-reload") {
			foundDaemonReload = true
		}
		if strings.Contains(full, "systemctl --user enable") {
			foundEnable = true
		}
		if strings.Contains(full, "systemctl --user restart") {
			foundRestart = true
		}
	}

	if !foundDaemonReload || !foundEnable || !foundRestart {
		t.Errorf("systemctl setup commands missing: %+v", cmds)
	}

	// Test uninstall
	err = mgr.Uninstall(context.Background())
	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}

	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Errorf("unit file was not removed upon uninstall")
	}
}

func TestMigrator_Relocation(t *testing.T) {
	tempHome := t.TempDir()
	srcDir := filepath.Join(tempHome, "source")
	tgtDir := filepath.Join(tempHome, "target")

	_ = os.MkdirAll(srcDir, 0755)

	// Create dummy OmniDex state files
	_ = os.WriteFile(filepath.Join(srcDir, "config.json"), []byte(`{"port":8080}`), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "indexer.db"), []byte("sqlite database"), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "indexer.db-wal"), []byte("sqlite wal"), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "omnidex.log"), []byte("app log"), 0644)

	runner := NewMockCommandRunner()
	runner.Outputs["is-active"] = "active" // Simulate active service

	// Create dummy unit file
	unitDir := filepath.Join(tempHome, ".config", "systemd", "user")
	_ = os.MkdirAll(unitDir, 0755)
	unitPath := filepath.Join(unitDir, "omnidex.service")
	_ = os.WriteFile(unitPath, []byte("WorkingDirectory="+srcDir+"\nExecStart=/opt/omnidex\n"), 0644)

	var out, errOut bytes.Buffer
	m := &Migrator{
		runner: runner,
		out:    &out,
		errOut: &errOut,
	}

	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	err := m.MigrateRelocation(context.Background(), MigrationOptions{
		SourceDir: srcDir,
		TargetDir: tgtDir,
		CopyOnly:  false, // Move
	})

	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Check that target files exist
	expectedFiles := []string{"config.json", "indexer.db", "indexer.db-wal", "omnidex.log"}
	for _, f := range expectedFiles {
		tgtPath := filepath.Join(tgtDir, f)
		if _, err := os.Stat(tgtPath); os.IsNotExist(err) {
			t.Errorf("expected migrated file %s in target, not found", f)
		}
		// Source file should be deleted on move
		srcPath := filepath.Join(srcDir, f)
		if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
			t.Errorf("expected source file %s to be removed after move", f)
		}
	}

	// Verify unit file WorkingDirectory updated
	newUnit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("failed to read updated unit file: %v", err)
	}
	if !strings.Contains(string(newUnit), "WorkingDirectory="+tgtDir) {
		t.Errorf("unit file not updated with target directory: %s", string(newUnit))
	}
}

func TestExpectedAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, expected string
	}{
		{"linux", "amd64", "omnidex-linux-amd64"},
		{"linux", "arm64", "omnidex-linux-arm64"},
		{"windows", "amd64", "omnidex-windows-amd64.exe"},
		{"darwin", "arm64", "omnidex-darwin-arm64"},
	}

	for _, c := range cases {
		actual := ExpectedAssetName(c.goos, c.goarch)
		if actual != c.expected {
			t.Errorf("ExpectedAssetName(%s, %s) = %s; want %s", c.goos, c.goarch, actual, c.expected)
		}
	}
}
