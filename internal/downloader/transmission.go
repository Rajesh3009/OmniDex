package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
)

type TransmissionClient struct {
	cfg        config.DownloadClientConfig
	httpClient *http.Client
	sessionID  string
	sessionMu  sync.RWMutex
}

func NewTransmissionClient(cfg config.DownloadClientConfig) *TransmissionClient {
	return &TransmissionClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

func (t *TransmissionClient) Name() string { return t.cfg.Name }
func (t *TransmissionClient) Type() string { return "transmission" }

func (t *TransmissionClient) doRPC(ctx context.Context, method string, args any) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(t.cfg.Host), "/")
	if !strings.HasSuffix(base, "/transmission/rpc") {
		base = base + "/transmission/rpc"
	}

	payload := map[string]any{
		"method":    method,
		"arguments": args,
	}
	bodyJSON, _ := json.Marshal(payload)

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		if t.cfg.Username != "" || t.cfg.Password != "" {
			req.SetBasicAuth(t.cfg.Username, t.cfg.Password)
		}

		t.sessionMu.RLock()
		sess := t.sessionID
		t.sessionMu.RUnlock()
		if sess != "" {
			req.Header.Set("X-Transmission-Session-Id", sess)
		}

		resp, err := t.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("transmission connection failed: %w", err)
		}

		// Handle 409 Conflict handshake for session token
		if resp.StatusCode == http.StatusConflict {
			newSess := resp.Header.Get("X-Transmission-Session-Id")
			resp.Body.Close()
			if newSess == "" {
				return nil, fmt.Errorf("transmission returned 409 Conflict without X-Transmission-Session-Id header")
			}
			t.sessionMu.Lock()
			t.sessionID = newSess
			t.sessionMu.Unlock()
			continue
		}

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("transmission RPC error (HTTP %d): %s", resp.StatusCode, string(respBytes))
		}

		var rpcResp struct {
			Result    string          `json:"result"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
			return nil, fmt.Errorf("decode transmission response: %w", err)
		}

		if rpcResp.Result != "success" {
			return nil, fmt.Errorf("transmission RPC returned error: %s", rpcResp.Result)
		}

		return rpcResp.Arguments, nil
	}

	return nil, fmt.Errorf("transmission session handshake failed after retry")
}

func (t *TransmissionClient) TestConnection(ctx context.Context) error {
	_, err := t.doRPC(ctx, "session-get", map[string]any{})
	return err
}

func (t *TransmissionClient) AddTorrent(ctx context.Context, opt AddOptions) error {
	savePath := opt.SavePath
	if savePath == "" {
		savePath = t.cfg.SavePath
	}

	args := map[string]any{
		"filename": opt.MagnetURI,
		"paused":   opt.Paused,
	}
	if !t.cfg.AutoStart {
		args["paused"] = true
	}
	if savePath != "" {
		args["download-dir"] = savePath
	}

	_, err := t.doRPC(ctx, "torrent-add", args)
	return err
}
