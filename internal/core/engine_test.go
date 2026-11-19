package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
)

func TestEngineLifecycleAndSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_engine.db")

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.DHTListenAddr = ":0" // Ephemeral port
	cfg.ScraperEnabled = false
	cfg.TrendingEnabled = false

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to initialize engine: %v", err)
	}

	engine.Start(context.Background())
	defer func() {
		if err := engine.Close(); err != nil {
			t.Errorf("error closing engine: %v", err)
		}
	}()

	// Verify database query through engine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	records, total, err := engine.Search(ctx, db.SearchFilter{Limit: 10})
	if err != nil {
		t.Fatalf("engine search failed: %v", err)
	}
	if total != 0 || len(records) != 0 {
		t.Errorf("expected empty search results, got total %d", total)
	}

	// Verify categories
	cats, err := engine.Categories()
	if err != nil {
		t.Fatalf("engine categories failed: %v", err)
	}
	_ = cats

	// Verify stats
	stats, err := engine.Stats()
	if err != nil {
		t.Fatalf("engine stats failed: %v", err)
	}
	if stats.DB.TotalTorrents != 0 {
		t.Errorf("expected 0 total torrents, got %v", stats.DB.TotalTorrents)
	}

	// Verify memory shrink
	memStats, err := engine.ShrinkMemory()
	if err != nil {
		t.Fatalf("engine shrink memory failed: %v", err)
	}
	if memStats.AllocBytes == 0 {
		t.Error("expected non-zero memory stats")
	}
}

func TestEngineConfigUpdate(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_engine_cfg.db")

	cfg := config.DefaultConfig()
	cfg.DBPath = dbPath
	cfg.DHTListenAddr = ":0"
	cfg.ScraperEnabled = false
	cfg.TrendingEnabled = false

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to initialize engine: %v", err)
	}
	defer engine.Close()

	newCfg := cfg
	newCfg.AutoPruneDays = 45
	newCfg.FilterEnabled = true

	if err := engine.UpdateConfig(newCfg); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	current := engine.Config()
	if current.AutoPruneDays != 45 || !current.FilterEnabled {
		t.Errorf("updated config was not applied correctly: %+v", current)
	}
}
