package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteAndPruneTorrents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omnidex-test-*")
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

	// Insert test torrents
	t1 := &TorrentRecord{
		InfoHash:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:        "Active Torrent 1",
		Size:        1024,
		CreatedTime: time.Now().AddDate(0, 0, -40),
		Seeders:     10,
		Category:    "Video",
		Source:      "dht",
	}
	t2 := &TorrentRecord{
		InfoHash:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Name:        "Dead Torrent Old",
		Size:        2048,
		CreatedTime: time.Now().AddDate(0, 0, -35),
		Seeders:     0,
		Category:    "Software",
		Source:      "dht",
	}
	t3 := &TorrentRecord{
		InfoHash:    "cccccccccccccccccccccccccccccccccccccccc",
		Name:        "Dead Torrent Fresh",
		Size:        4096,
		CreatedTime: time.Now().AddDate(0, 0, -2),
		Seeders:     0,
		Category:    "Books",
		Source:      "feed",
	}

	for _, rec := range []*TorrentRecord{t1, t2, t3} {
		if err := store.UpsertTorrent(rec); err != nil {
			t.Fatalf("UpsertTorrent failed for %s: %v", rec.Name, err)
		}
	}

	// Verify Search finds all 3
	results, total, err := store.Search("Torrent", 10, 0)
	if err != nil || total != 3 {
		t.Fatalf("expected 3 torrents in search, got total %d, err %v", total, err)
	}

	// 1. Test DeleteTorrent for t1
	if err := store.DeleteTorrent(t1.InfoHash); err != nil {
		t.Fatalf("DeleteTorrent failed: %v", err)
	}

	// Verify t1 is gone from store and FTS5 search
	exists, err := store.Exists(t1.InfoHash)
	if err != nil || exists {
		t.Errorf("expected t1 to be deleted, exists: %v", exists)
	}
	results, total, _ = store.Search("Active", 10, 0)
	if len(results) != 0 || total != 0 {
		t.Errorf("expected Active Torrent to be gone from FTS search, got %d results", total)
	}

	// 2. Test CountDeadTorrents (seeders <= 0 older than 30 days)
	deadCount, err := store.CountDeadTorrents(0, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("CountDeadTorrents failed: %v", err)
	}
	if deadCount != 1 {
		t.Errorf("expected 1 dead torrent older than 30 days, got %d", deadCount)
	}

	// 3. Test PruneTorrents (seeders <= 0 older than 30 days)
	pruned, err := store.PruneTorrents(PruneOptions{
		MaxSeeders: 0,
		OlderThan:  time.Now().AddDate(0, 0, -30),
	})
	if err != nil {
		t.Fatalf("PruneTorrents failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned torrent, got %d", pruned)
	}

	// Verify t2 is pruned, but t3 (fresh dead torrent) remains
	existsT2, _ := store.Exists(t2.InfoHash)
	existsT3, _ := store.Exists(t3.InfoHash)
	if existsT2 {
		t.Errorf("expected t2 to be pruned")
	}
	if !existsT3 {
		t.Errorf("expected fresh dead torrent t3 to remain")
	}
}
