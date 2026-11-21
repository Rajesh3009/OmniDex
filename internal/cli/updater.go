package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultReleaseRepo = "Rajesh3009/OmniDex"
)

// GitHubRelease represents the minimal schema returned by the GitHub Releases API.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a binary asset uploaded to a release.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Updater coordinates release querying, asset downloading, and atomic binary swapping with rollback.
type Updater struct {
	repo       string
	httpClient *http.Client
	runner     CommandRunner
	out        io.Writer
	errOut     io.Writer
}

// NewUpdater creates a new updater instance.
func NewUpdater(runner CommandRunner, out io.Writer, errOut io.Writer) *Updater {
	return &Updater{
		repo: defaultReleaseRepo,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		runner: runner,
		out:    out,
		errOut: errOut,
	}
}

// ExpectedAssetName returns the binary release name expected for the host operating system and architecture.
func ExpectedAssetName(goos, goarch string) string {
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("omnidex-%s-%s%s", goos, goarch, ext)
}

// FetchLatestRelease queries the GitHub Releases API for the latest release.
func (u *Updater) FetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OmniDex-Updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub releases API returned status %s", resp.Status)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode release response: %w", err)
	}
	return &rel, nil
}

// RunUpdate handles `omnidex update [options]`.
func (c *CLI) RunUpdate(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(c.ErrOut)
	checkOnly := fs.Bool("check-only", false, "Only check if a new release is available without updating")
	force := fs.Bool("force", false, "Force re-download even if already up to date")
	port := fs.Int("port", 8080, "Web dashboard port used for post-update healthcheck")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	updater := NewUpdater(c.Runner, c.Out, c.ErrOut)

	fmt.Fprintf(c.Out, "Checking for latest OmniDex release (current version: v%s)...\n", c.Version)
	rel, err := updater.FetchLatestRelease(ctx)
	if err != nil {
		fmt.Fprintf(c.ErrOut, "Error checking for updates: %v\n", err)
		return 1
	}

	latestTag := strings.TrimPrefix(rel.TagName, "v")
	currentTag := strings.TrimPrefix(c.Version, "v")

	fmt.Fprintf(c.Out, "Latest release on GitHub: %s (published: %s)\n", rel.TagName, rel.PublishedAt)

	if latestTag == currentTag && !*force {
		fmt.Fprintf(c.Out, "✅ OmniDex is already up to date (v%s).\n", c.Version)
		return 0
	}

	if *checkOnly {
		fmt.Fprintf(c.Out, "Update available: v%s -> %s. Run 'omnidex update' to apply.\n", c.Version, rel.TagName)
		return 0
	}

	// Find the asset matching current OS and architecture
	expectedName := ExpectedAssetName(runtime.GOOS, runtime.GOARCH)
	var targetAsset *GitHubAsset
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, expectedName) {
			targetAsset = &a
			break
		}
	}

	if targetAsset == nil {
		fmt.Fprintf(c.ErrOut, "Error: No matching asset '%s' found in release %s.\n", expectedName, rel.TagName)
		fmt.Fprintln(c.ErrOut, "Available assets:")
		for _, a := range rel.Assets {
			fmt.Fprintf(c.ErrOut, "  - %s (%d bytes)\n", a.Name, a.Size)
		}
		return 1
	}

	// Resolve executable path
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(c.ErrOut, "Failed to determine current executable path: %v\n", err)
		return 1
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	exeDir := filepath.Dir(exePath)

	tempFile := filepath.Join(exeDir, fmt.Sprintf(".omnidex-new-%d.tmp", time.Now().UnixNano()))
	backupFile := exePath + ".old"

	fmt.Fprintf(c.Out, "Downloading %s (%.2f MB)...\n", targetAsset.Name, float64(targetAsset.Size)/(1024*1024))
	if err := updater.downloadAsset(ctx, targetAsset.BrowserDownloadURL, tempFile); err != nil {
		_ = os.Remove(tempFile)
		fmt.Fprintf(c.ErrOut, "Download failed: %v\n", err)
		return 1
	}

	// Set executable permissions
	if err := os.Chmod(tempFile, 0755); err != nil {
		_ = os.Remove(tempFile)
		fmt.Fprintf(c.ErrOut, "Failed to set executable permissions: %v\n", err)
		return 1
	}

	// Validate downloaded binary by querying version
	out, err := c.Runner.Run(ctx, tempFile, "version")
	if err != nil {
		_ = os.Remove(tempFile)
		fmt.Fprintf(c.ErrOut, "Downloaded binary verification failed ('%s version'): %v (output: %s)\n", tempFile, err, out)
		return 1
	}
	fmt.Fprintf(c.Out, "Binary verification succeeded: %s\n", strings.TrimSpace(out))

	// Check if systemd service is active
	svcMgr := NewSystemdServiceManager(c.Runner)
	wasActive := svcMgr.IsActive(ctx)

	// Perform atomic binary swap
	_ = os.Remove(backupFile)
	if err := os.Rename(exePath, backupFile); err != nil {
		_ = os.Remove(tempFile)
		fmt.Fprintf(c.ErrOut, "Failed to backup existing binary: %v\n", err)
		return 1
	}

	if err := os.Rename(tempFile, exePath); err != nil {
		// Roll back immediate rename failure
		_ = os.Rename(backupFile, exePath)
		_ = os.Remove(tempFile)
		fmt.Fprintf(c.ErrOut, "Failed to install new binary: %v (restored previous binary)\n", err)
		return 1
	}

	fmt.Fprintln(c.Out, "New binary successfully installed.")

	// If systemd service was running, restart and perform health check
	if wasActive {
		fmt.Fprintln(c.Out, "Restarting systemd service and verifying health...")
		if _, err := svcMgr.Action(ctx, "restart"); err != nil {
			fmt.Fprintf(c.ErrOut, "Warning: failed to restart systemd service: %v\n", err)
		} else {
			healthOK := updater.pollHealth(ctx, *port, 15*time.Second)
			if !healthOK {
				fmt.Fprintln(c.ErrOut, "🚨 Health check failed after update! Rolling back to previous binary...")
				_ = os.Remove(exePath)
				_ = os.Rename(backupFile, exePath)
				_, _ = svcMgr.Action(ctx, "restart")
				fmt.Fprintln(c.ErrOut, "✅ Rollback completed. OmniDex restored to previous working version.")
				return 1
			}
			fmt.Fprintln(c.Out, "✅ Service healthy and responding to queries.")
		}
	}

	// Clean up backup file
	_ = os.Remove(backupFile)
	fmt.Fprintf(c.Out, "🎉 OmniDex successfully updated to %s!\n", rel.TagName)
	return 0
}

func (u *Updater) downloadAsset(ctx context.Context, url string, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "OmniDex-Updater")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (u *Updater) pollHealth(ctx context.Context, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	testURL := fmt.Sprintf("http://127.0.0.1:%d/api/v1/stats", port)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}
