package scraper

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
)

// mockBEP15Tracker sets up a test UDP listener responding to BEP 15 connect and scrape packets
func startMockBEP15Tracker(t *testing.T, expectedHashHex string, respSeeders, respLeechers, respCompleted int) (string, func()) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock UDP tracker: %v", err)
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		buf := make([]byte, 2048)
		var connID uint64 = 0x1234567890abcdef

		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}

			if n < 16 {
				continue
			}

			action := binary.BigEndian.Uint32(buf[8:12])
			txID := binary.BigEndian.Uint32(buf[12:16])

			if action == actionConnect {
				// Send Connect Response: action (0), txID, connID
				resp := make([]byte, 16)
				binary.BigEndian.PutUint32(resp[0:4], actionConnect)
				binary.BigEndian.PutUint32(resp[4:8], txID)
				binary.BigEndian.PutUint64(resp[8:16], connID)
				_, _ = conn.WriteTo(resp, addr)
			} else {
				// Check if Scrape request
				scrapeAction := binary.BigEndian.Uint32(buf[8:12])
				scrapeTxID := binary.BigEndian.Uint32(buf[12:16])
				if scrapeAction == actionScrape {
					// 1 hash scrape response: 8 bytes header + 12 bytes
					resp := make([]byte, 8+12)
					binary.BigEndian.PutUint32(resp[0:4], actionScrape)
					binary.BigEndian.PutUint32(resp[4:8], scrapeTxID)
					binary.BigEndian.PutUint32(resp[8:12], uint32(respSeeders))
					binary.BigEndian.PutUint32(resp[12:16], uint32(respCompleted))
					binary.BigEndian.PutUint32(resp[16:20], uint32(respLeechers))
					_, _ = conn.WriteTo(resp, addr)
				}
			}
		}
	}()

	addrStr := fmt.Sprintf("udp://%s/announce", conn.LocalAddr().String())
	cleanup := func() {
		close(stopCh)
		_ = conn.Close()
		<-doneCh
	}

	return addrStr, cleanup
}

func TestMockUDPTrackerScrape(t *testing.T) {
	testHash := "4444444444444444444444444444444444444444"
	expectedSeeders := 25
	expectedLeechers := 8
	expectedCompleted := 150

	trackerURL, cleanup := startMockBEP15Tracker(t, testHash, expectedSeeders, expectedLeechers, expectedCompleted)
	defer cleanup()

	tmpDir, err := os.MkdirTemp("", "omnidex-mock-scraper-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Insert test torrent
	rec := &db.TorrentRecord{
		InfoHash:    testHash,
		Name:        "UDP Scrape Test",
		Size:        5000,
		CreatedTime: time.Now(),
		Seeders:     0,
		Leechers:    0,
		Category:    "Video",
		Source:      "dht",
	}
	if err := store.UpsertTorrent(rec); err != nil {
		t.Fatalf("UpsertTorrent failed: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.ScraperEnabled = true
	cfg.ScraperTrackers = []string{trackerURL}
	cfg.ScraperAutoUpdateTrackers = false

	scraper := NewScraper(cfg, store)

	// Test single scrape
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := scraper.ScrapeSingle(ctx, testHash)
	if err != nil {
		t.Fatalf("ScrapeSingle failed: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected scrape success, got error: %s", res.Error)
	}

	if res.Seeders != expectedSeeders {
		t.Errorf("expected seeders %d, got %d", expectedSeeders, res.Seeders)
	}
	if res.Leechers != expectedLeechers {
		t.Errorf("expected leechers %d, got %d", expectedLeechers, res.Leechers)
	}

	// Verify DB store was updated
	updated, err := store.GetTorrentByHash(testHash)
	if err != nil {
		t.Fatalf("GetTorrentByHash failed: %v", err)
	}
	if updated.Seeders != expectedSeeders || updated.Leechers != expectedLeechers {
		t.Errorf("database not updated properly: seeders=%d leechers=%d", updated.Seeders, updated.Leechers)
	}
	if updated.ScrapeFails != 0 {
		t.Errorf("expected scrape_fails 0, got %d", updated.ScrapeFails)
	}
}

