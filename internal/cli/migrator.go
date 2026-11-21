package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Migrator coordinates safe data relocation, WAL synchronization, and systemd service reconfiguration.
type Migrator struct {
	runner CommandRunner
	out    io.Writer
	errOut io.Writer
}

// NewMigrator creates a new migrator instance.
func NewMigrator(runner CommandRunner, out io.Writer, errOut io.Writer) *Migrator {
	return &Migrator{
		runner: runner,
		out:    out,
		errOut: errOut,
	}
}

// MigrationOptions configures the migration execution.
type MigrationOptions struct {
	SourceDir string
	TargetDir string
	CopyOnly  bool // If true, copies files rather than moving them (leaves source intact as backup)
}

// MigrateRelocation performs the end-to-end safe migration workflow.
func (m *Migrator) MigrateRelocation(ctx context.Context, opts MigrationOptions) error {
	srcAbs, err := filepath.Abs(opts.SourceDir)
	if err != nil {
		return fmt.Errorf("invalid source directory: %w", err)
	}
	tgtAbs, err := filepath.Abs(opts.TargetDir)
	if err != nil {
		return fmt.Errorf("invalid target directory: %w", err)
	}

	if srcAbs == tgtAbs {
		return fmt.Errorf("source and target directories are identical (%s)", srcAbs)
	}

	// 1. Check if systemd user service is running
	svcMgr := NewSystemdServiceManager(m.runner)
	wasRunning := svcMgr.IsActive(ctx)
	if wasRunning {
		fmt.Fprintln(m.out, "Stopping omnidex.service to safely checkpoint SQLite database...")
		if _, err := svcMgr.Action(ctx, "stop"); err != nil {
			return fmt.Errorf("failed to stop systemd service before migration: %w", err)
		}
		fmt.Fprintln(m.out, "✅ Service stopped.")
	}

	// 2. Ensure target directory exists
	if err := os.MkdirAll(tgtAbs, 0755); err != nil {
		return fmt.Errorf("failed to create target directory (%s): %w", tgtAbs, err)
	}

	// 3. Collect OmniDex state files to relocate
	filesToMigrate := m.findMigratableFiles(srcAbs)
	if len(filesToMigrate) == 0 {
		return fmt.Errorf("no OmniDex database, configuration, or log files found in %s", srcAbs)
	}

	fmt.Fprintf(m.out, "Migrating %d files from %s -> %s\n", len(filesToMigrate), srcAbs, tgtAbs)

	migratedCount := 0
	for _, relName := range filesToMigrate {
		srcFile := filepath.Join(srcAbs, relName)
		tgtFile := filepath.Join(tgtAbs, relName)

		actionDesc := "Moving"
		if opts.CopyOnly {
			actionDesc = "Copying"
		}
		fmt.Fprintf(m.out, "  %s %s... ", actionDesc, relName)

		if err := copyFile(srcFile, tgtFile); err != nil {
			fmt.Fprintln(m.out, "FAILED")
			return fmt.Errorf("failed to transfer %s: %w", relName, err)
		}

		if !opts.CopyOnly {
			if err := os.Remove(srcFile); err != nil {
				fmt.Fprintln(m.out, "FAILED TO REMOVE SOURCE")
				return fmt.Errorf("copied %s but failed to remove source file: %w", relName, err)
			}
		}

		fmt.Fprintln(m.out, "OK")
		migratedCount++
	}

	// 4. Update systemd user unit WorkingDirectory if unit exists
	unitPath := svcMgr.UnitPath()
	if unitBytes, err := os.ReadFile(unitPath); err == nil {
		unitContent := string(unitBytes)
		lines := strings.Split(unitContent, "\n")
		modified := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "WorkingDirectory=") {
				lines[i] = fmt.Sprintf("WorkingDirectory=%s", tgtAbs)
				modified = true
			}
		}

		if modified {
			newUnit := strings.Join(lines, "\n")
			if err := os.WriteFile(unitPath, []byte(newUnit), 0644); err == nil {
				fmt.Fprintf(m.out, "Updated systemd unit WorkingDirectory in %s\n", unitPath)
				_, _ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
			}
		}
	}

	// 5. Restart service if it was running previously
	if wasRunning {
		fmt.Fprintln(m.out, "Restarting omnidex.service in new directory...")
		if _, err := svcMgr.Action(ctx, "start"); err != nil {
			fmt.Fprintf(m.errOut, "Warning: failed to restart service in new directory: %v\n", err)
		} else {
			fmt.Fprintln(m.out, "✅ Service restarted and active.")
		}
	}

	fmt.Fprintf(m.out, "\n🎉 Migration completed successfully! (%d files migrated to %s)\n", migratedCount, tgtAbs)
	return nil
}

func (m *Migrator) findMigratableFiles(srcDir string) []string {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil
	}

	var results []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Match configuration, databases, WAL journals, logs, and crash dumps
		if name == "config.json" ||
			strings.HasPrefix(name, "indexer.db") ||
			strings.HasPrefix(name, "omnidex.") && strings.HasSuffix(name, ".log") ||
			name == "omnidex.log" ||
			strings.HasPrefix(name, "crash-") && strings.HasSuffix(name, ".log") {
			results = append(results, name)
		}
	}
	return results
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// RunMigrate handles `omnidex migrate [options]`.
func (c *CLI) RunMigrate(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(c.ErrOut)
	to := fs.String("to", "", "Target destination folder path (required)")
	from := fs.String("from", "", "Source folder path (defaults to current directory)")
	copyOnly := fs.Bool("copy", false, "Copy files instead of moving (preserves source backup)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *to == "" {
		fmt.Fprintln(c.ErrOut, "Error: missing required target directory flag: --to <path>")
		fmt.Fprintln(c.ErrOut, "Example: omnidex migrate --to /home/user/apps/omnidex")
		return 1
	}

	sourceDir := *from
	if sourceDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(c.ErrOut, "Error determining current directory: %v\n", err)
			return 1
		}
		sourceDir = wd
	}

	migrator := NewMigrator(c.Runner, c.Out, c.ErrOut)
	err := migrator.MigrateRelocation(ctx, MigrationOptions{
		SourceDir: sourceDir,
		TargetDir: *to,
		CopyOnly:  *copyOnly,
	})

	if err != nil {
		fmt.Fprintf(c.ErrOut, "Migration error: %v\n", err)
		return 1
	}
	return 0
}
