package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultServiceName = "omnidex.service"
	unitTemplate       = `[Unit]
Description=OmniDex - Hybrid DHT Indexer & Torznab Bridge
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`
)

// SystemdServiceManager manages systemd user unit generation, registration, and lifecycle.
type SystemdServiceManager struct {
	runner CommandRunner
	homeDir string
}

// NewSystemdServiceManager creates a service manager instance.
func NewSystemdServiceManager(runner CommandRunner) *SystemdServiceManager {
	home, _ := os.UserHomeDir()
	return &SystemdServiceManager{
		runner:  runner,
		homeDir: home,
	}
}

// UnitPath returns the standard user systemd unit file path (~/.config/systemd/user/omnidex.service).
func (m *SystemdServiceManager) UnitPath() string {
	return filepath.Join(m.homeDir, ".config", "systemd", "user", defaultServiceName)
}

// Install generates and registers the systemd unit with auto-restart and linger enabled.
func (m *SystemdServiceManager) Install(ctx context.Context, workingDir string, execPath string) error {
	if runtime.GOOS != "linux" && os.Getenv("OMNIDEX_MOCK_SYSTEMD") == "" {
		return fmt.Errorf("systemd service installation is only supported on Linux (detected OS: %s)", runtime.GOOS)
	}

	if execPath == "" {
		ep, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine executable path: %w", err)
		}
		execPath, _ = filepath.EvalSymlinks(ep)
	}
	execPath, err := filepath.Abs(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute executable path: %w", err)
	}

	if workingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			workingDir = filepath.Dir(execPath)
		} else {
			workingDir = wd
		}
	}
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("failed to resolve working directory: %w", err)
	}

	unitDir := filepath.Dir(m.UnitPath())
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create systemd user directory (%s): %w", unitDir, err)
	}

	unitContent := fmt.Sprintf(unitTemplate, workingDir, execPath)
	if err := os.WriteFile(m.UnitPath(), []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd unit file: %w", err)
	}

	// 1. Reload systemd daemon
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload failed: %w", err)
	}

	// 2. Enable service for auto-start
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "enable", defaultServiceName); err != nil {
		return fmt.Errorf("systemctl --user enable failed: %w", err)
	}

	// 3. Restart/Start service immediately
	if _, err := m.runner.Run(ctx, "systemctl", "--user", "restart", defaultServiceName); err != nil {
		return fmt.Errorf("systemctl --user restart failed: %w", err)
	}

	// 4. Enable user lingering for 24/7 background operation without active login
	currUser, uErr := user.Current()
	username := ""
	if uErr == nil && currUser != nil {
		username = currUser.Username
	}
	if username != "" {
		_, _ = m.runner.Run(ctx, "loginctl", "enable-linger", username)
	}

	return nil
}

// Uninstall stops, disables, and deletes the systemd unit.
func (m *SystemdServiceManager) Uninstall(ctx context.Context) error {
	_, _ = m.runner.Run(ctx, "systemctl", "--user", "stop", defaultServiceName)
	_, _ = m.runner.Run(ctx, "systemctl", "--user", "disable", defaultServiceName)

	unitFile := m.UnitPath()
	if err := os.Remove(unitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unit file (%s): %w", unitFile, err)
	}

	_, _ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	return nil
}

// Action triggers a systemctl lifecycle operation (start, stop, restart, status).
func (m *SystemdServiceManager) Action(ctx context.Context, action string) (string, error) {
	return m.runner.Run(ctx, "systemctl", "--user", action, defaultServiceName)
}

// IsActive checks if the systemd user service is currently running.
func (m *SystemdServiceManager) IsActive(ctx context.Context) bool {
	out, err := m.runner.Run(ctx, "systemctl", "--user", "is-active", defaultServiceName)
	return err == nil && strings.TrimSpace(out) == "active"
}

// RunService handles `omnidex service <action>`.
func (c *CLI) RunService(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.ErrOut, "Error: service subcommand requires an action (install, status, start, stop, restart, logs, uninstall)")
		fmt.Fprintln(c.ErrOut, "Run 'omnidex service --help' for details.")
		return 1
	}

	mgr := NewSystemdServiceManager(c.Runner)

	action := strings.ToLower(strings.TrimSpace(args[0]))
	switch action {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		fs.SetOutput(c.ErrOut)
		dir := fs.String("dir", "", "Working directory for database and config (defaults to current directory)")
		execPath := fs.String("exec", "", "Path to omnidex binary (defaults to current running executable)")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}

		fmt.Fprintln(c.Out, "Installing OmniDex systemd user service with auto-restart and linger...")
		if err := mgr.Install(ctx, *dir, *execPath); err != nil {
			fmt.Fprintf(c.ErrOut, "Error installing service: %v\n", err)
			return 1
		}

		fmt.Fprintln(c.Out, "✅ Service installed and started successfully!")
		fmt.Fprintf(c.Out, "Unit file: %s\n", mgr.UnitPath())
		fmt.Fprintln(c.Out, "Automatic crash restart: enabled (Restart=always, RestartSec=3)")
		fmt.Fprintln(c.Out, "24/7 background session: enabled (loginctl enable-linger)")

		status, _ := mgr.Action(ctx, "status")
		if status != "" {
			fmt.Fprintf(c.Out, "\n%s\n", status)
		}
		return 0

	case "uninstall":
		fmt.Fprintln(c.Out, "Stopping and removing OmniDex systemd user service...")
		if err := mgr.Uninstall(ctx); err != nil {
			fmt.Fprintf(c.ErrOut, "Error uninstalling service: %v\n", err)
			return 1
		}
		fmt.Fprintln(c.Out, "✅ Service successfully uninstalled.")
		return 0

	case "start", "stop", "restart":
		fmt.Fprintf(c.Out, "%sing OmniDex background service...\n", strings.Title(action))
		out, err := mgr.Action(ctx, action)
		if err != nil {
			fmt.Fprintf(c.ErrOut, "Failed to %s service: %v\n", action, err)
			return 1
		}
		if out != "" {
			fmt.Fprintln(c.Out, out)
		}
		fmt.Fprintf(c.Out, "✅ Service %sed.\n", action)
		return 0

	case "status":
		out, err := mgr.Action(ctx, "status")
		if out != "" {
			fmt.Fprintln(c.Out, out)
		}
		if err != nil {
			return 1
		}
		return 0

	case "logs":
		fs := flag.NewFlagSet("service logs", flag.ContinueOnError)
		fs.SetOutput(c.ErrOut)
		follow := fs.Bool("f", false, "Follow log output continuously")
		lines := fs.Int("n", 50, "Number of journal lines to display")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}

		cmdArgs := []string{"--user", "-u", defaultServiceName, "-n", fmt.Sprintf("%d", *lines)}
		if *follow {
			cmdArgs = append(cmdArgs, "-f")
		}
		out, err := c.Runner.Run(ctx, "journalctl", cmdArgs...)
		if out != "" {
			fmt.Fprintln(c.Out, out)
		}
		if err != nil {
			fmt.Fprintf(c.ErrOut, "Error reading logs: %v\n", err)
			return 1
		}
		return 0

	case "--help", "-h", "help":
		fmt.Fprint(c.Out, `Usage: omnidex service <command> [options]

Commands:
  install [--dir <path>]   Install and start systemd user service with auto-restart and linger
  status                   Show live systemd service status
  start                    Start the background service
  stop                     Stop the background service
  restart                  Restart the background service
  logs [-f] [-n <lines>]   View service logs from journalctl
  uninstall                Stop, disable, and remove the systemd user service
`)
		return 0

	default:
		fmt.Fprintf(c.ErrOut, "Unknown service action: '%s'. Available: install, status, start, stop, restart, logs, uninstall\n", action)
		return 1
	}
}
