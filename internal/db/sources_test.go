package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceFilterAndGetSources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omnidex-sources-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	torrents := []*TorrentRecord{
		{
			InfoHash:    "1111111111111111111111111111111111111111",
			Name:        "DHT Torrent",
			Size:        1000,
			CreatedTime: time.Now(),
			Seeders:     10,
			Source:      "dht",
		},
		{
			InfoHash:    "2222222222222222222222222222222222222222",
			Name:        "Prowlarr Torrent",
			Size:        2000,
			CreatedTime: time.Now(),
			Seeders:     15,
			Source:      "prowlarr",
		},
		{
			InfoHash:    "3333333333333333333333333333333333333333",
			Name:        "Trending EZTV",
			Size:        3000,
			CreatedTime: time.Now(),
			Seeders:     25,
			Source:      "trending:eztv",
		},
		{
			InfoHash:    "4444444444444444444444444444444444444444",
			Name:        "Trending YTS",
			Size:        4000,
			CreatedTime: time.Now(),
			Seeders:     50,
			Source:      "trending:yts",
		},
		{
			InfoHash:    "5555555555555555555555555555555555555555",
			Name:        "TokyoTosho Anime",
			Size:        5000,
			CreatedTime: time.Now(),
			Seeders:     30,
			Source:      "tokyotosho",
		},
		{
			InfoHash:    "6666666666666666666666666666666666666666",
			Name:        "Manual Torrent",
			Size:        6000,
			CreatedTime: time.Now(),
			Seeders:     5,
			Source:      "manual",
		},
	}

	for _, tor := range torrents {
		if err := store.UpsertTorrent(tor); err != nil {
			t.Fatalf("UpsertTorrent failed: %v", err)
		}
	}

	// 1. Test GetSources
	sources, err := store.GetSources()
	if err != nil {
		t.Fatalf("GetSources failed: %v", err)
	}
	expectedSources := map[string]bool{
		"dht":           true,
		"prowlarr":      true,
		"trending:eztv": true,
		"trending:yts":  true,
		"tokyotosho":    true,
		"manual":        true,
	}
	if len(sources) != len(expectedSources) {
		t.Fatalf("expected %d distinct sources, got %d: %v", len(expectedSources), len(sources), sources)
	}
	for _, s := range sources {
		if !expectedSources[s] {
			t.Errorf("unexpected source in GetSources: %s", s)
		}
	}

	// 2. Test search with source="all"
	recs, total, err := store.SearchWithFilter(SearchFilter{Source: "all"})
	if err != nil || total != 6 || len(recs) != 6 {
		t.Fatalf("expected 6 torrents for source=all, got total %d, recs %d", total, len(recs))
	}

	// 3. Test search with source="dht"
	recs, total, err = store.SearchWithFilter(SearchFilter{Source: "dht"})
	if err != nil || total != 1 || recs[0].Source != "dht" {
		t.Fatalf("expected 1 dht torrent, got total %d", total)
	}

	// 4. Test search with grouped source="trending"
	recs, total, err = store.SearchWithFilter(SearchFilter{Source: "trending"})
	if err != nil || total != 2 {
		t.Fatalf("expected 2 trending torrents, got %d", total)
	}
	for _, r := range recs {
		if r.Source != "trending:eztv" && r.Source != "trending:yts" {
			t.Errorf("expected trending source, got %s", r.Source)
		}
	}

	// 5. Test search with specific trending source="trending:yts"
	recs, total, err = store.SearchWithFilter(SearchFilter{Source: "trending:yts"})
	if err != nil || total != 1 || recs[0].Name != "Trending YTS" {
		t.Fatalf("expected 1 trending:yts torrent, got %d", total)
	}

	// 6. Test search with grouped source="feeds"
	recs, total, err = store.SearchWithFilter(SearchFilter{Source: "feeds"})
	if err != nil || total != 1 || recs[0].Source != "tokyotosho" {
		t.Fatalf("expected 1 feed torrent (tokyotosho), got %d", total)
	}

	// 7. Test FTS search with source filter
	recs, total, err = store.SearchWithFilter(SearchFilter{Query: "Anime", Source: "feeds"})
	if err != nil || total != 1 || recs[0].Name != "TokyoTosho Anime" {
		t.Fatalf("expected 1 TokyoTosho Anime via FTS, got total %d", total)
	}

	// 8. Test Prune with source="trending"
	pruned, err := store.PruneTorrents(PruneOptions{
		MaxSeeders: 100, // all match seeders
		Source:     "trending",
	})
	if err != nil || pruned != 2 {
		t.Fatalf("expected 2 pruned trending torrents, got %d (err: %v)", pruned, err)
	}

	// Verify remaining sources
	remaining, _, _ := store.SearchWithFilter(SearchFilter{Source: "all"})
	if len(remaining) != 4 {
		t.Fatalf("expected 4 remaining torrents, got %d", len(remaining))
	}
}
