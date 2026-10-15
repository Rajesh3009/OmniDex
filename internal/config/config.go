package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	WebPort         int    `json:"web_port"`
	DHTListenAddr   string `json:"dht_listen_addr"`
	DHTWorkers      int    `json:"dht_workers"`
	MaxTorrentPeers int    `json:"max_torrent_peers"`
	DBPath          string `json:"db_path"`
	TorznabAPIKey   string `json:"torznab_api_key"`
	ProwlarrURL     string `json:"prowlarr_url"`
	ProwlarrAPIKey  string `json:"prowlarr_api_key"`
}

func DefaultConfig() Config {
	return Config{
		WebPort:         8080,
		DHTListenAddr:   ":6881",
		DHTWorkers:      15,
		MaxTorrentPeers: 20,
		DBPath:          "indexer.db",
		TorznabAPIKey:   "local-indexer-secret-key",
		ProwlarrURL:     "http://localhost:9696",
		ProwlarrAPIKey:  "",
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
