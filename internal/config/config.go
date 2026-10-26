package config

import (
	"encoding/json"
	"os"
	"strings"
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
	FilterEnabled         bool         `json:"filter_enabled"`
	FilterCategoryMode    string       `json:"filter_category_mode"` // "allowlist" or "blocklist"
	FilterCategories      []string     `json:"filter_categories"`
	FilterKeywords        []string     `json:"filter_keywords"`
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
		FilterEnabled:       false,
		FilterCategoryMode:  "allowlist",
		FilterCategories:    []string{},
		FilterKeywords:      []string{},
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

// MatchesRetentionFilter returns true if a torrent should be kept based on current filter rules.
// If filtering is disabled or no rules are set, it returns true for all torrents.
func (c *Config) MatchesRetentionFilter(category, name string) bool {
	if !c.FilterEnabled {
		return true
	}

	hasCategories := len(c.FilterCategories) > 0
	hasKeywords := len(c.FilterKeywords) > 0

	// If enabled but no rules configured at all, do not drop everything
	if !hasCategories && !hasKeywords {
		return true
	}

	categoryMatch := false
	if hasCategories {
		found := false
		for _, cat := range c.FilterCategories {
			if strings.EqualFold(cat, category) {
				found = true
				break
			}
		}
		if strings.HasPrefix(strings.ToLower(c.FilterCategoryMode), "block") {
			categoryMatch = !found
		} else { // default "allowlist"
			categoryMatch = found
		}
	} else {
		// If no category rule is configured, category match is true if blocklist mode, false if allowlist
		if strings.HasPrefix(strings.ToLower(c.FilterCategoryMode), "block") {
			categoryMatch = true
		}
	}

	keywordMatch := false
	if hasKeywords {
		lowerName := strings.ToLower(name)
		for _, kw := range c.FilterKeywords {
			trimmed := strings.ToLower(strings.TrimSpace(kw))
			if trimmed != "" && strings.Contains(lowerName, trimmed) {
				keywordMatch = true
				break
			}
		}
	}

	// Combination logic: OR logic (matches either valid category OR contains required keyword)
	return categoryMatch || keywordMatch
}

