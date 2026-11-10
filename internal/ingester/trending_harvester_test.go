package ingester

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/scraper"
)

type mockTrendingScraper struct {
	name     string
	torrents []scraper.Torrent
	err      error
}

func (m *mockTrendingScraper) Name() string {
	return m.name
}

func (m *mockTrendingScraper) Search(ctx context.Context, query string, opts ...scraper.Option) ([]scraper.Torrent, error) {
	return m.torrents, m.err
}

func (m *mockTrendingScraper) ParseHTML(r io.Reader) ([]scraper.Torrent, error) {
	return m.torrents, m.err
}

func (m *mockTrendingScraper) Trending(ctx context.Context, opts ...scraper.Option) ([]scraper.Torrent, error) {
	return m.torrents, m.err
}

func TestTrendingHarvester_Harvest(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_trending.db")
	store, err := db.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	reg := scraper.NewRegistry()
	reg.Register(&mockTrendingScraper{
		name: "ThePirateBay",
		torrents: []scraper.Torrent{
			{
				Title:         "Ubuntu.24.04.LTS.Desktop.x86_64",
				InfoHash:      "1111111111111111111111111111111111111111",
				Size:          4500000000,
				Seeds:         250,
				Leeches:       30,
				Source:        "ThePirateBay",
				Category:      "Applications/Linux",
				PublishedDate: time.Now().UTC(),
			},
			{
				Title:         "Low.Seeder.Release.Sample",
				InfoHash:      "2222222222222222222222222222222222222222",
				Size:          100000000,
				Seeds:         2, // Below minSeeds (5) -> should be discarded
				Leeches:       1,
				Source:        "ThePirateBay",
				Category:      "Other",
				PublishedDate: time.Now().UTC(),
			},
			{
				Title:         "Malware.Free.Software.CRACK.exe",
				InfoHash:      "3333333333333333333333333333333333333333",
				Size:          500000,
				Seeds:         500, // High seeds but suspicious spam
				Leeches:       10,
				Source:        "ThePirateBay",
				Category:      "Applications",
				PublishedDate: time.Now().UTC(),
			},
		},
	})
	reg.Register(&mockTrendingScraper{
		name: "YTS",
		torrents: []scraper.Torrent{
			{
				Title:         "Inception.2010.1080p.BluRay.x264",
				InfoHash:      "4444444444444444444444444444444444444444",
				Size:          2100000000,
				Seeds:         85,
				Leeches:       12,
				Source:        "YTS",
				Category:      "Movies/HD",
				PublishedDate: time.Now().UTC(),
			},
		},
	})

	cfg := config.Config{
		TrendingEnabled:       true,
		TrendingIntervalHours: 4,
		TrendingMinSeeds:      5,
		TrendingProviders: map[string]bool{
			"thepiratebay": true,
			"yts":          true,
		},
	}

	harvester := NewTrendingHarvester(cfg, store, reg, nil)
	ctx := context.Background()

	count, err := harvester.Harvest(ctx)
	if err != nil {
		t.Fatalf("unexpected harvest error: %v", err)
	}

	// Ubuntu (250 seeds, safe) and Inception (85 seeds, safe) must be ingested.
	// Low seed (2 seeds) is filtered.
	if count < 2 {
		t.Errorf("expected at least 2 qualified torrents ingested, got %d", count)
	}

	// Verify Ubuntu is in DB
	rec1, err := store.GetByInfoHash("1111111111111111111111111111111111111111")
	if err != nil || rec1 == nil {
		t.Fatalf("expected Ubuntu torrent in store, err: %v", err)
	}
	if rec1.Seeders != 250 {
		t.Errorf("expected 250 seeders, got %d", rec1.Seeders)
	}

	// Verify low-seed torrent was NOT inserted
	rec2, _ := store.GetByInfoHash("2222222222222222222222222222222222222222")
	if rec2 != nil {
		t.Errorf("low-seed torrent should have been filtered out")
	}

	// Verify Stats()
	stats := harvester.Stats()
	if stats.LastCount != count {
		t.Errorf("expected LastCount=%d, got %d", count, stats.LastCount)
	}
	if stats.TotalHarvested != int64(count) {
		t.Errorf("expected TotalHarvested=%d, got %d", count, stats.TotalHarvested)
	}
}

func TestTrendingHarvester_ProviderFiltering(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_provider_filter.db")
	store, err := db.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	reg := scraper.NewRegistry()
	reg.Register(&mockTrendingScraper{
		name: "EnabledProvider",
		torrents: []scraper.Torrent{
			{
				Title:    "Valid.Release",
				InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Seeds:    20,
				Source:   "EnabledProvider",
			},
		},
	})
	reg.Register(&mockTrendingScraper{
		name: "DisabledProvider",
		torrents: []scraper.Torrent{
			{
				Title:    "Disabled.Release",
				InfoHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Seeds:    50,
				Source:   "DisabledProvider",
			},
		},
	})

	cfg := config.Config{
		TrendingEnabled:       true,
		TrendingIntervalHours: 4,
		TrendingMinSeeds:      5,
		TrendingProviders: map[string]bool{
			"enabledprovider":  true,
			"disabledprovider": false,
		},
	}

	harvester := NewTrendingHarvester(cfg, store, reg, nil)
	count, err := harvester.Harvest(context.Background())
	if err != nil {
		t.Fatalf("harvest error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 torrent from enabled provider, got %d", count)
	}

	recDisabled, _ := store.GetByInfoHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if recDisabled != nil {
		t.Errorf("disabled provider release should not have been fetched or saved")
	}
}
