package config

import (
	"encoding/json"
	"os"
)

type FeedConfig struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Type            string `json:"type"` // "torznab", "rss", "yts"
	APIKey          string `json:"api_key,omitempty"`
	IntervalMinutes int    `json:"interval_minutes"`
	Enabled         bool   `json:"enabled"`
}

type Config struct {
	WebPort               int          `json:"web_port"`
	DHTListenAddr         string       `json:"dht_listen_addr"`
	DHTWorkers            int          `json:"dht_workers"`
	MaxTorrentPeers       int          `json:"max_torrent_peers"`
	DBPath                string       `json:"db_path"`
	TorznabAPIKey         string       `json:"torznab_api_key"`
	ProwlarrURL           string       `json:"prowlarr_url"`
	ProwlarrAPIKey        string       `json:"prowlarr_api_key"`
	MaxDownloadRate       int64        `json:"max_download_rate"`        // bytes/sec (0 = unlimited)
	MaxUploadRate         int64        `json:"max_upload_rate"`          // bytes/sec (0 = unlimited)
	MonthlyDataLimitBytes int64        `json:"monthly_data_limit_bytes"` // bytes/month (0 = unlimited)
	ExternalFeeds         []FeedConfig `json:"external_feeds"`
	AutoPruneDead         bool         `json:"auto_prune_dead"`
	AutoPruneDays         int          `json:"auto_prune_days"`
	ProwlarrAutoSyncMin   int          `json:"prowlarr_auto_sync_min"`
}

func DefaultConfig() Config {
	return Config{
		WebPort:               8686,
		DHTListenAddr:         ":6881",
		DHTWorkers:            15,
		MaxTorrentPeers:       20,
		DBPath:                "indexer.db",
		TorznabAPIKey:         "local-indexer-secret-key",
		ProwlarrURL:           "http://localhost:9696",
		ProwlarrAPIKey:        "",
		MaxDownloadRate:       0,
		MaxUploadRate:         0,
		MonthlyDataLimitBytes: 0,
		ExternalFeeds: []FeedConfig{
			{
				ID:              "nyaa-all",
				Name:            "Nyaa (Anime/Media)",
				URL:             "https://nyaa.si/?page=rss",
				Type:            "rss",
				IntervalMinutes: 30,
				Enabled:         false,
			},
			{
				ID:              "eztv-shows",
				Name:            "EZTV (TV Shows)",
				URL:             "https://eztvx.to/ezrss.xml",
				Type:            "rss",
				IntervalMinutes: 30,
				Enabled:         false,
			},
		},
		AutoPruneDead:       false,
		AutoPruneDays:       30,
		ProwlarrAutoSyncMin: 0,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = SaveConfig(path, cfg)
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
