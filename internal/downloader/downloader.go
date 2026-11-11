package downloader

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
)

// AddOptions specifies parameters for adding a torrent to a download client
type AddOptions struct {
	MagnetURI   string   `json:"magnet_uri"`
	TorrentData []byte   `json:"torrent_data,omitempty"`
	Name        string   `json:"name"`
	SavePath    string   `json:"save_path,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Paused      bool     `json:"paused"`
}

// Client represents the common interface implemented by all BitTorrent download client adapters
type Client interface {
	Name() string
	Type() string
	TestConnection(ctx context.Context) error
	AddTorrent(ctx context.Context, opt AddOptions) error
}

// Manager orchestrates configured download clients
type Manager struct {
	mu      sync.RWMutex
	clients map[string]Client
	configs map[string]config.DownloadClientConfig
}

// NewManager creates a download client manager from the application configuration
func NewManager(cfg []config.DownloadClientConfig) *Manager {
	m := &Manager{
		clients: make(map[string]Client),
		configs: make(map[string]config.DownloadClientConfig),
	}
	m.UpdateClients(cfg)
	return m
}

// UpdateClients reloads active download client instances
func (m *Manager) UpdateClients(configs []config.DownloadClientConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clients = make(map[string]Client)
	m.configs = make(map[string]config.DownloadClientConfig)

	for _, c := range configs {
		m.configs[c.ID] = c
		if !c.Enabled {
			continue
		}

		client, err := createClientInstance(c)
		if err == nil {
			m.clients[c.ID] = client
		}
	}
}

// GetClient returns an active client by ID, or the default enabled client if ID is empty
func (m *Manager) GetClient(id string) (Client, *config.DownloadClientConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id != "" {
		c, ok := m.clients[id]
		cfg, cfgOk := m.configs[id]
		if !ok || !cfgOk {
			return nil, nil, fmt.Errorf("download client '%s' not found or disabled", id)
		}
		return c, &cfg, nil
	}

	// If no ID specified, look for default client, or first enabled client
	for _, cfg := range m.configs {
		if cfg.Enabled && cfg.IsDefault {
			if c, ok := m.clients[cfg.ID]; ok {
				return c, &cfg, nil
			}
		}
	}

	for _, cfg := range m.configs {
		if cfg.Enabled {
			if c, ok := m.clients[cfg.ID]; ok {
				return c, &cfg, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("no enabled download clients configured")
}

// TestConfig tests a transient or configured download client connection
func (m *Manager) TestConfig(ctx context.Context, cfg config.DownloadClientConfig) error {
	client, err := createClientInstance(cfg)
	if err != nil {
		return err
	}
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return client.TestConnection(testCtx)
}

func createClientInstance(cfg config.DownloadClientConfig) (Client, error) {
	cleanType := strings.ToLower(strings.TrimSpace(cfg.Type))
	switch cleanType {
	case "qbittorrent":
		return NewQBittorrentClient(cfg), nil
	case "transmission":
		return NewTransmissionClient(cfg), nil
	case "deluge":
		return NewDelugeClient(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported client type: %s", cfg.Type)
	}
}
