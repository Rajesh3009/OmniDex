package downloader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"local-indexer/internal/config"
)

func TestQBittorrentClient(t *testing.T) {
	loginCalled := false
	addCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginCalled = true
			if r.FormValue("username") == "admin" && r.FormValue("password") == "adminadmin" {
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fake-sid"})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("Ok."))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Fails."))
		case "/api/v2/app/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("v4.5.0"))
		case "/api/v2/torrents/add":
			addCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config.DownloadClientConfig{
		ID:        "qbit-test",
		Name:      "Local qBit",
		Type:      "qbittorrent",
		Host:      ts.URL,
		Username:  "admin",
		Password:  "adminadmin",
		AutoStart: true,
		Enabled:   true,
	}

	client := NewQBittorrentClient(cfg)
	ctx := context.Background()

	if err := client.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection failed: %v", err)
	}
	if !loginCalled {
		t.Errorf("expected login to be called")
	}

	err := client.AddTorrent(ctx, AddOptions{
		MagnetURI: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		Category:  "Movies",
		Paused:    false,
	})
	if err != nil {
		t.Fatalf("AddTorrent failed: %v", err)
	}
	if !addCalled {
		t.Errorf("expected add torrent to be called")
	}
}

func TestTransmissionClient(t *testing.T) {
	sessionID := "valid-session-1234"
	var addCalled bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") != sessionID {
			w.Header().Set("X-Transmission-Session-Id", sessionID)
			w.WriteHeader(http.StatusConflict)
			return
		}

		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "session-get" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    "success",
				"arguments": map[string]any{"version": "3.00"},
			})
			return
		}

		if req.Method == "torrent-add" {
			addCalled = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    "success",
				"arguments": map[string]any{"torrent-added": map[string]any{"id": 1, "name": "test"}},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	cfg := config.DownloadClientConfig{
		ID:        "trans-test",
		Name:      "Local Transmission",
		Type:      "transmission",
		Host:      ts.URL,
		AutoStart: true,
		Enabled:   true,
	}

	client := NewTransmissionClient(cfg)
	ctx := context.Background()

	if err := client.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection failed: %v", err)
	}

	err := client.AddTorrent(ctx, AddOptions{
		MagnetURI: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatalf("AddTorrent failed: %v", err)
	}
	if !addCalled {
		t.Errorf("expected add torrent to be called")
	}
}

func TestDelugeClient(t *testing.T) {
	loginCalled := false
	addCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int64  `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch req.Method {
		case "auth.login":
			loginCalled = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": true,
				"error":  nil,
				"id":     req.ID,
			})
		case "web.connected":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": true,
				"error":  nil,
				"id":     req.ID,
			})
		case "core.add_torrent_magnet":
			addCalled = true
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": "torrent-hash",
				"error":  nil,
				"id":     req.ID,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config.DownloadClientConfig{
		ID:        "deluge-test",
		Name:      "Local Deluge",
		Type:      "deluge",
		Host:      ts.URL,
		Password:  "deluge",
		AutoStart: true,
		Enabled:   true,
	}

	client := NewDelugeClient(cfg)
	ctx := context.Background()

	if err := client.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection failed: %v", err)
	}
	if !loginCalled {
		t.Errorf("expected deluge login to be called")
	}

	err := client.AddTorrent(ctx, AddOptions{
		MagnetURI: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatalf("AddTorrent failed: %v", err)
	}
	if !addCalled {
		t.Errorf("expected deluge add to be called")
	}
}
