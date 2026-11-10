package ingester

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/metadata"
	"local-indexer/internal/scraper"
	"local-indexer/internal/spamfilter"
)

// TrendingHarvesterStats holds runtime statistics for the trending harvester engine.
type TrendingHarvesterStats struct {
	Running          bool      `json:"running"`
	IsHarvesting     bool      `json:"is_harvesting"`
	LastRun          time.Time `json:"last_run"`
	LastCount        int       `json:"last_count"`
	LastError        string    `json:"last_error"`
	TotalHarvested   int64     `json:"total_harvested"`
	NextRunScheduled time.Time `json:"next_run_scheduled"`
	IntervalHours    int       `json:"interval_hours"`
	MinSeeds         int       `json:"min_seeds"`
}

// TrendingHarvester manages scheduled and manual top/trending torrent harvesting.
type TrendingHarvester struct {
	cfg        config.Config
	store      *db.Store
	registry   *scraper.Registry
	udpScraper *scraper.Scraper

	mu               sync.RWMutex
	stopChan         chan struct{}
	wg               sync.WaitGroup
	running          bool
	isHarvesting     atomic.Bool
	lastRun          time.Time
	lastCount        int
	lastError        string
	totalHarvested   atomic.Int64
	nextRunScheduled time.Time
}

// NewTrendingHarvester initializes a new trending harvester instance.
func NewTrendingHarvester(
	cfg config.Config,
	store *db.Store,
	registry *scraper.Registry,
	udpScraper *scraper.Scraper,
) *TrendingHarvester {
	return &TrendingHarvester{
		cfg:        cfg,
		store:      store,
		registry:   registry,
		udpScraper: udpScraper,
		stopChan:   make(chan struct{}),
	}
}

// Start launches the periodic background harvesting loop.
func (h *TrendingHarvester) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	if !h.cfg.TrendingEnabled {
		log.Println("[TrendingHarvester] Trending harvester is disabled in configuration")
		h.mu.Unlock()
		return
	}
	h.running = true
	h.stopChan = make(chan struct{})
	intervalHours := h.cfg.TrendingIntervalHours
	if intervalHours <= 0 {
		intervalHours = 4
	}
	h.nextRunScheduled = time.Now().Add(time.Duration(intervalHours) * time.Hour)
	h.mu.Unlock()

	log.Printf("[TrendingHarvester] Starting harvester loop (interval: %dh, min seeds: %d)",
		intervalHours, h.cfg.TrendingMinSeeds)

	h.wg.Add(1)
	go h.loop()
}

// Stop gracefully shuts down the background harvesting loop.
func (h *TrendingHarvester) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	close(h.stopChan)
	h.mu.Unlock()

	h.wg.Wait()
	log.Println("[TrendingHarvester] Harvester stopped")
}

// loop runs the scheduled periodic harvest job.
func (h *TrendingHarvester) loop() {
	defer h.wg.Done()

	h.mu.RLock()
	hours := h.cfg.TrendingIntervalHours
	if hours <= 0 {
		hours = 4
	}
	h.mu.RUnlock()

	ticker := time.NewTicker(time.Duration(hours) * time.Hour)
	defer ticker.Stop()

	// Initial delayed harvest 15 seconds after startup to allow scraper registry initialization
	select {
	case <-time.After(15 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		_, _ = h.Harvest(ctx)
		cancel()
	case <-h.stopChan:
		return
	}

	for {
		select {
		case <-ticker.C:
			h.mu.RLock()
			interval := time.Duration(h.cfg.TrendingIntervalHours) * time.Hour
			h.mu.RUnlock()

			h.mu.Lock()
			h.nextRunScheduled = time.Now().Add(interval)
			h.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			_, _ = h.Harvest(ctx)
			cancel()

		case <-h.stopChan:
			return
		}
	}
}

// Harvest executes a single top/trending harvesting pass across all enabled scraper sources.
func (h *TrendingHarvester) Harvest(ctx context.Context) (int, error) {
	if !h.isHarvesting.CompareAndSwap(false, true) {
		return 0, fmt.Errorf("a harvest run is already in progress")
	}
	defer h.isHarvesting.Store(false)

	h.mu.RLock()
	minSeeds := h.cfg.TrendingMinSeeds
	if minSeeds <= 0 {
		minSeeds = 5
	}
	providers := make(map[string]bool)
	for k, v := range h.cfg.TrendingProviders {
		providers[k] = v
	}
	h.mu.RUnlock()

	log.Printf("[TrendingHarvester] Starting trending harvest across enabled providers (min seeds: %d)...", minSeeds)

	torrents, err := h.registry.TrendingScrapers(ctx, providers)
	if err != nil && len(torrents) == 0 {
		h.mu.Lock()
		h.lastRun = time.Now().UTC()
		h.lastError = err.Error()
		h.lastCount = 0
		h.mu.Unlock()
		log.Printf("[TrendingHarvester] Harvest failed: %v", err)
		return 0, err
	}

	// Filter, deduplicate, and ingest qualified torrents
	seenHashes := make(map[string]struct{})
	insertedCount := 0
	var qualifiedHashes []string

	for _, t := range torrents {
		hash := strings.ToLower(strings.TrimSpace(t.InfoHash))
		if hash == "" || len(hash) != 40 {
			continue
		}
		if _, seen := seenHashes[hash]; seen {
			continue
		}
		seenHashes[hash] = struct{}{}

		// Enforce minimum seed threshold
		if t.Seeds < minSeeds {
			continue
		}

		// Enforce anti-spam inspection
		spam := spamfilter.Inspect(t.Title, nil, t.Category, t.Size)
		if spam.Score >= 80 {
			continue
		}

		// Extract metadata tags
		tags := metadata.ExtractTags(t.Title, nil)

		sourceName := "trending"
		if t.Source != "" {
			sourceName = fmt.Sprintf("trending:%s", strings.ToLower(strings.ReplaceAll(t.Source, " ", "")))
		}

		record := db.TorrentRecord{
			InfoHash:     hash,
			Name:         t.Title,
			Size:         t.Size,
			CreatedTime:  t.PublishedDate,
			LastSeen:     time.Now().UTC(),
			Source:       sourceName,
			Seeders:      t.Seeds,
			Leechers:     t.Leeches,
			Category:     t.Category,
			MagnetURI:    t.MagnetURI,
			Resolution:   tags.Resolution,
			Codec:        tags.Codec,
			Quality:      tags.Quality,
			Audio:        tags.Audio,
			HDR:          tags.HDR,
			ReleaseGroup: tags.ReleaseGroup,
			IsSuspicious: spam.IsSuspicious,
			SpamScore:    spam.Score,
			SpamReasons:  strings.Join(spam.Reasons, "; "),
			LastScraped:  time.Now().UTC(),
		}

		if err := h.store.UpsertTorrent(&record); err == nil {
			insertedCount++
			qualifiedHashes = append(qualifiedHashes, hash)
		}
	}

	h.totalHarvested.Add(int64(insertedCount))

	h.mu.Lock()
	h.lastRun = time.Now().UTC()
	h.lastCount = insertedCount
	h.lastError = ""
	h.mu.Unlock()

	log.Printf("[TrendingHarvester] Harvest completed: %d total scraped, %d qualified (>=%d seeds) ingested",
		len(torrents), insertedCount, minSeeds)

	// Trigger asynchronous UDP tracker verification for newly ingested hashes if scraper is available
	if h.udpScraper != nil && len(qualifiedHashes) > 0 {
		go func(hashes []string) {
			scrapeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = h.udpScraper.ScrapeHashes(scrapeCtx, hashes)
		}(qualifiedHashes)
	}

	return insertedCount, nil
}

// Stats returns current harvester status and statistics.
func (h *TrendingHarvester) Stats() TrendingHarvesterStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return TrendingHarvesterStats{
		Running:          h.running,
		IsHarvesting:     h.isHarvesting.Load(),
		LastRun:          h.lastRun,
		LastCount:        h.lastCount,
		LastError:        h.lastError,
		TotalHarvested:   h.totalHarvested.Load(),
		NextRunScheduled: h.nextRunScheduled,
		IntervalHours:    h.cfg.TrendingIntervalHours,
		MinSeeds:         h.cfg.TrendingMinSeeds,
	}
}

// UpdateConfig updates the harvester configuration dynamically.
func (h *TrendingHarvester) UpdateConfig(newCfg config.Config) {
	h.mu.Lock()
	h.cfg = newCfg
	h.mu.Unlock()
}
