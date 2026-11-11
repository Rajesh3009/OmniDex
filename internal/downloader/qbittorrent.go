package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
)

type QBittorrentClient struct {
	cfg        config.DownloadClientConfig
	httpClient *http.Client
	authMu     sync.Mutex
	lastAuth   time.Time
}

func NewQBittorrentClient(cfg config.DownloadClientConfig) *QBittorrentClient {
	jar, _ := cookiejar.New(nil)
	return &QBittorrentClient{
		cfg: cfg,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 12 * time.Second,
		},
	}
}

func (q *QBittorrentClient) Name() string { return q.cfg.Name }
func (q *QBittorrentClient) Type() string { return "qbittorrent" }

func (q *QBittorrentClient) ensureAuth(ctx context.Context) error {
	q.authMu.Lock()
	defer q.authMu.Unlock()

	// If authenticated in the last 10 minutes, keep cookie session
	if time.Since(q.lastAuth) < 10*time.Minute && !q.lastAuth.IsZero() {
		return nil
	}

	base := strings.TrimRight(strings.TrimSpace(q.cfg.Host), "/")
	if base == "" {
		return fmt.Errorf("qbittorrent host URL is empty")
	}

	data := url.Values{}
	data.Set("username", q.cfg.Username)
	data.Set("password", q.cfg.Password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v2/auth/login", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", base)

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to qBittorrent: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(bodyBytes))

	if resp.StatusCode != http.StatusOK || bodyStr == "Fails." {
		return fmt.Errorf("qBittorrent authentication failed (HTTP %d: %s)", resp.StatusCode, bodyStr)
	}

	q.lastAuth = time.Now()
	return nil
}

func (q *QBittorrentClient) TestConnection(ctx context.Context) error {
	if err := q.ensureAuth(ctx); err != nil {
		return err
	}

	base := strings.TrimRight(strings.TrimSpace(q.cfg.Host), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v2/app/version", nil)
	if err != nil {
		return err
	}
	resp, err := q.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("query version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

func (q *QBittorrentClient) AddTorrent(ctx context.Context, opt AddOptions) error {
	if err := q.ensureAuth(ctx); err != nil {
		return err
	}

	base := strings.TrimRight(strings.TrimSpace(q.cfg.Host), "/")

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	if opt.MagnetURI != "" {
		_ = w.WriteField("urls", opt.MagnetURI)
	}

	savePath := opt.SavePath
	if savePath == "" {
		savePath = q.cfg.SavePath
	}
	if savePath != "" {
		_ = w.WriteField("savepath", savePath)
	}

	category := opt.Category
	if category == "" {
		category = q.cfg.Category
	}
	if category != "" {
		_ = w.WriteField("category", category)
	}

	if len(opt.Tags) > 0 {
		_ = w.WriteField("tags", strings.Join(opt.Tags, ","))
	}

	// paused parameter: "true" = paused, "false" = start immediately
	pausedVal := "false"
	if opt.Paused || !q.cfg.AutoStart {
		if opt.Paused {
			pausedVal = "true"
		}
	}
	_ = w.WriteField("paused", pausedVal)

	if err := w.Close(); err != nil {
		return fmt.Errorf("build multipart form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v2/torrents/add", &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Referer", base)

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send add torrent request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("qBittorrent add torrent failed (HTTP %d: %s)", resp.StatusCode, string(respBody))
	}

	return nil
}
