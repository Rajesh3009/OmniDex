package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"local-indexer/internal/config"
)

type DelugeClient struct {
	cfg        config.DownloadClientConfig
	httpClient *http.Client
	authMu     sync.Mutex
	lastAuth   time.Time
	reqID      int64
}

func NewDelugeClient(cfg config.DownloadClientConfig) *DelugeClient {
	jar, _ := cookiejar.New(nil)
	return &DelugeClient{
		cfg: cfg,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 12 * time.Second,
		},
	}
}

func (d *DelugeClient) Name() string { return d.cfg.Name }
func (d *DelugeClient) Type() string { return "deluge" }

func (d *DelugeClient) nextID() int64 {
	return atomic.AddInt64(&d.reqID, 1)
}

func (d *DelugeClient) ensureAuth(ctx context.Context) error {
	d.authMu.Lock()
	defer d.authMu.Unlock()

	if time.Since(d.lastAuth) < 10*time.Minute && !d.lastAuth.IsZero() {
		return nil
	}

	base := strings.TrimRight(strings.TrimSpace(d.cfg.Host), "/")
	if base == "" {
		return fmt.Errorf("deluge host URL is empty")
	}

	payload := map[string]any{
		"method": "auth.login",
		"params": []any{d.cfg.Password},
		"id":     d.nextID(),
	}
	bodyJSON, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/json", bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deluge connection failed: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result bool `json:"result"`
		Error  any  `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decode deluge login response: %w", err)
	}

	if !rpcResp.Result {
		return fmt.Errorf("deluge web authentication failed (check web password)")
	}

	d.lastAuth = time.Now()
	return nil
}

func (d *DelugeClient) doJSON(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	if err := d.ensureAuth(ctx); err != nil {
		return nil, err
	}

	base := strings.TrimRight(strings.TrimSpace(d.cfg.Host), "/")
	payload := map[string]any{
		"method": method,
		"params": params,
		"id":     d.nextID(),
	}
	bodyJSON, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/json", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deluge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("decode deluge response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("deluge RPC error: %v", rpcResp.Error)
	}

	return rpcResp.Result, nil
}

func (d *DelugeClient) TestConnection(ctx context.Context) error {
	_, err := d.doJSON(ctx, "web.connected", []any{})
	return err
}

func (d *DelugeClient) AddTorrent(ctx context.Context, opt AddOptions) error {
	savePath := opt.SavePath
	if savePath == "" {
		savePath = d.cfg.SavePath
	}

	options := map[string]any{}
	if savePath != "" {
		options["download_location"] = savePath
	}

	addPaused := opt.Paused || !d.cfg.AutoStart
	options["add_paused"] = addPaused

	params := []any{
		opt.MagnetURI,
		options,
	}

	_, err := d.doJSON(ctx, "core.add_torrent_magnet", params)
	return err
}
