package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CommandRunner defines the execution seam for invoking operating system utilities (systemctl, loginctl, etc.).
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// OSCommandRunner is the standard production adapter that executes commands via os/exec.
type OSCommandRunner struct{}

func (r *OSCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if len(trimmed) > 0 {
			return trimmed, fmt.Errorf("%s: %w", trimmed, err)
		}
		return "", err
	}
	return trimmed, nil
}

// CLI holds the configuration and runtime dependencies for the OmniDex CLI subcommands.
type CLI struct {
	Version string
	Runner  CommandRunner
	Out     io.Writer
	ErrOut  io.Writer
}

// NewCLI creates a default CLI instance.
func NewCLI(version string) *CLI {
	return &CLI{
		Version: version,
		Runner:  &OSCommandRunner{},
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
	}
}

// HandleArgs checks if os.Args matches a known CLI subcommand.
// If it does, it executes the command and returns true.
// If args indicate starting the daemon server (empty, "run", "serve"), it returns false.
func HandleArgs(args []string, version string) bool {
	if len(args) == 0 {
		return false
	}

	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	switch cmd {
	case "run", "serve", "server":
		return false
	case "service", "update", "migrate", "version", "--version", "-v", "help", "--help", "-h":
		c := NewCLI(version)
		exitCode := c.Execute(context.Background(), args)
		if exitCode != 0 {
			os.Exit(exitCode)
		}
		return true
	default:
		// Unknown subcommand or flag: if it begins with -, check if it's help/version, otherwise return false
		if strings.HasPrefix(cmd, "-") {
			return false
		}
		c := NewCLI(version)
		fmt.Fprintf(c.ErrOut, "Unknown command: %s\n\n", cmd)
		c.PrintUsage()
		os.Exit(1)
		return true
	}
}

// Execute parses and runs the requested subcommand.
func (c *CLI) Execute(ctx context.Context, args []string) int {
	if len(args) == 0 {
		c.PrintUsage()
		return 0
	}

	subcmd := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcmd {
	case "version", "--version", "-v":
		return c.RunVersion(ctx, args[1:])
	case "help", "--help", "-h":
		c.PrintUsage()
		return 0
	case "service":
		return c.RunService(ctx, args[1:])
	case "update":
		return c.RunUpdate(ctx, args[1:])
	case "migrate":
		return c.RunMigrate(ctx, args[1:])
	default:
		fmt.Fprintf(c.ErrOut, "Unknown command: %s\n\n", subcmd)
		c.PrintUsage()
		return 1
	}
}

// PrintUsage outputs help information for the CLI.
func (c *CLI) PrintUsage() {
	helpText := fmt.Sprintf(`OmniDex — Hybrid DHT Indexer & Torznab Bridge v%s

Usage:
  omnidex [command] [options]

Commands:
  run, serve      Start the indexer daemon and web dashboard (default)
  service         Manage Linux systemd background service and auto-restart
  update          Check, download, and apply releases with auto-rollback
  migrate         Safely relocate database, configs, and service to a new folder
  version         Display version information and check for new releases
  help            Show this help message

Service Subcommands:
  omnidex service install [--dir <path>]   Install and start systemd user service with 24/7 linger
  omnidex service status                   Display current service status, PID, and uptime
  omnidex service start                    Start the background service
  omnidex service stop                     Stop the background service
  omnidex service restart                  Restart the background service
  omnidex service logs [-f]                View or follow service journal logs
  omnidex service uninstall                Stop and remove systemd service definition

Update Options:
  omnidex update [--check-only]            Check for updates without downloading
  omnidex update [--force]                 Force re-download and re-install latest release

Migration Options:
  omnidex migrate --to <target_path>       Move database, config, and update systemd unit
  omnidex migrate --to <path> --copy       Copy data instead of moving (keeps source backup)

Run 'omnidex [command] --help' for more details on any subcommand.
`, c.Version)
	fmt.Fprint(c.Out, helpText)
}

// RunVersion prints the version and optionally checks for updates.
func (c *CLI) RunVersion(ctx context.Context, args []string) int {
	fmt.Fprintf(c.Out, "OmniDex v%s\n", c.Version)
	return 0
}
