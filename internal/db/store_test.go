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

func TestRetentionFiltering(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "omnidex-filter-test-*")
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

	// Insert test records
	records := []*TorrentRecord{
		{
			InfoHash:    "1111111111111111111111111111111111111111",
			Name:        "Inception 2010 1080p BluRay",
			Size:        2048,
			CreatedTime: time.Now(),
			Category:    "Video",
		},
		{
			InfoHash:    "2222222222222222222222222222222222222222",
			Name:        "Ubuntu 24.04 LTS Desktop",
			Size:        4096,
			CreatedTime: time.Now(),
			Category:    "Software",
		},
		{
			InfoHash:    "3333333333333333333333333333333333333333",
			Name:        "Windows 11 Activator Toolkit",
			Size:        1024,
			CreatedTime: time.Now(),
			Category:    "Software",
		},
		{
			InfoHash:    "4444444444444444444444444444444444444444",
			Name:        "Random Audio Book Collection",
			Size:        512,
			CreatedTime: time.Now(),
			Category:    "Audio",
		},
	}

	for _, rec := range records {
		if err := store.UpsertTorrent(rec); err != nil {
			t.Fatalf("UpsertTorrent failed for %s: %v", rec.Name, err)
		}
	}

	// Case 1: Allowlist Video only, no keywords.
	// Violations should be: Ubuntu (Software), Windows (Software), Audio Book (Audio) -> 3 violations
	v1, err := store.CountFilterViolations(FilterRules{
		Enabled:      true,
		CategoryMode: "allow",
		Categories:   []string{"Video"},
		Keywords:     nil,
	})
	if err != nil {
		t.Fatalf("CountFilterViolations v1 failed: %v", err)
	}
	if v1 != 3 {
		t.Errorf("expected 3 violations for Video allowlist, got %d", v1)
	}

	// Case 2: Allowlist Video only, but with keyword "Ubuntu".
	// Video is kept (category match), Ubuntu is kept (keyword match).
	// Violations should be: Windows (Software), Audio Book (Audio) -> 2 violations
	v2, err := store.CountFilterViolations(FilterRules{
		Enabled:      true,
		CategoryMode: "allow",
		Categories:   []string{"Video"},
		Keywords:     []string{"ubuntu"},
	})
	if err != nil {
		t.Fatalf("CountFilterViolations v2 failed: %v", err)
	}
	if v2 != 2 {
		t.Errorf("expected 2 violations for Video allowlist with Ubuntu keyword, got %d", v2)
	}

	// Case 3: Blocklist "Software", no keywords.
	// Violations: Ubuntu (Software), Windows (Software) -> 2 violations
	v3, err := store.CountFilterViolations(FilterRules{
		Enabled:      true,
		CategoryMode: "block",
		Categories:   []string{"Software"},
		Keywords:     nil,
	})
	if err != nil {
		t.Fatalf("CountFilterViolations v3 failed: %v", err)
	}
	if v3 != 2 {
		t.Errorf("expected 2 violations for Software blocklist, got %d", v3)
	}

	// Case 4: Blocklist "Software", but with keyword "Ubuntu".
	// Ubuntu is saved by keyword!
	// Violation: only Windows (Software) -> 1 violation
	v4, err := store.CountFilterViolations(FilterRules{
		Enabled:      true,
		CategoryMode: "block",
		Categories:   []string{"Software"},
		Keywords:     []string{"ubuntu"},
	})
	if err != nil {
		t.Fatalf("CountFilterViolations v4 failed: %v", err)
	}
	if v4 != 1 {
		t.Errorf("expected 1 violation for Software blocklist with Ubuntu keyword, got %d", v4)
	}

	// Case 5: Execute PruneFilterViolations with Case 4 rules
	pruned, err := store.PruneFilterViolations(FilterRules{
		Enabled:      true,
		CategoryMode: "block",
		Categories:   []string{"Software"},
		Keywords:     []string{"ubuntu"},
	})
	if err != nil {
		t.Fatalf("PruneFilterViolations failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned record, got %d", pruned)
	}

	// Verify Windows 11 was deleted, but Ubuntu was kept
	winExists, _ := store.Exists("3333333333333333333333333333333333333333")
	ubuntuExists, _ := store.Exists("2222222222222222222222222222222222222222")
	if winExists {
		t.Errorf("expected Windows 11 to be pruned")
	}
	if !ubuntuExists {
		t.Errorf("expected Ubuntu to be preserved due to keyword rule")
	}
}

