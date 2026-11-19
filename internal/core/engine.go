package core

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/dhtcrawler"
	"local-indexer/internal/downloader"
	"local-indexer/internal/ingester"
	"local-indexer/internal/prowlarr"
	"local-indexer/internal/scraper"
	"local-indexer/internal/sysinfo"
)

// Engine is the central coordinator encapsulating all crawlers, scrapers, ingesters,
// storage layers, and routine maintenance janitors behind a deep domain interface.
type Engine struct {
	cfgMu      sync.RWMutex
	cfg        config.Config
	store      *db.Store
	crawler    *dhtcrawler.Crawler
	prowl      *prowlarr.Client
	ingester   *ingester.Ingester
	scraper    *scraper.Scraper
	registry   *scraper.Registry
	trending   *ingester.TrendingHarvester
	downloader *downloader.Manager

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEngine initializes and wires all core subsystems according to configuration.
func NewEngine(cfg config.Config) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	store, err := db.NewStore(cfg.DBPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error opening database store: %w", err)
	}

	crawlerCfg := dhtcrawler.CrawlerConfig{
		DHTListenAddr:         cfg.DHTListenAddr,
		MaxTorrentPeers:       cfg.MaxTorrentPeers,
		Workers:               cfg.DHTWorkers,
		MaxDownloadRate:       cfg.MaxDownloadRate,
		MaxUploadRate:         cfg.MaxUploadRate,
		MonthlyDataLimitBytes: cfg.MonthlyDataLimitBytes,
	}
	crawler, err := dhtcrawler.NewCrawler(crawlerCfg, store)
	if err != nil {
		log.Printf("[Warning] DHT Crawler failed to initialize: %v (running in aggregator mode)", err)
	}

	prowlClient := prowlarr.NewClient(cfg.ProwlarrURL, cfg.ProwlarrAPIKey)
	feedIngester := ingester.NewIngester(store, cfg.ExternalFeeds)
	trackerScraper := scraper.NewScraper(cfg, store)
	scraperReg := scraper.DefaultRegistry()
	trendingHarvester := ingester.NewTrendingHarvester(cfg, store, scraperReg, trackerScraper)
	downloadMgr := downloader.NewManager(cfg.DownloadClients)

	eng := &Engine{
		cfg:        cfg,
		store:      store,
		crawler:    crawler,
		prowl:      prowlClient,
		ingester:   feedIngester,
		scraper:    trackerScraper,
		registry:   scraperReg,
		trending:   trendingHarvester,
		downloader: downloadMgr,
		ctx:        ctx,
		cancel:     cancel,
	}

	if crawler != nil {
		crawler.SetFilterFunc(eng.cfg.MatchesRetentionFilter)
	}
	if feedIngester != nil {
		feedIngester.SetFilterFunc(eng.cfg.MatchesRetentionFilter)
		if crawler != nil {
			feedIngester.SetOnZeroSizeTorrent(func(hash, magnet string) {
				var trackers []string
				if magnet != "" {
					if u, err := url.Parse(magnet); err == nil {
						trackers = u.Query()["tr"]
					}
				}
				crawler.EnqueueMetadataFetch(hash, trackers)
			})
		}
	}

	return eng, nil
}

// Start launches all background workers and routine maintenance janitors.
func (e *Engine) Start(parentCtx context.Context) {
	if e.crawler != nil {
		e.crawler.Start()
	}
	if e.ingester != nil {
		e.ingester.Start()
	}
	if e.scraper != nil {
		e.scraper.Start()
	}
	if e.trending != nil {
		e.trending.Start()
	}

	// Periodic WAL Memory Compactor (runs every 10 min: checkpoints WAL, shrinks cache, returns OS memory)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
				if e.store != nil {
					e.store.PruneMemory()
				}
				sysinfo.FreeMemory()
			}
		}
	}()

	// Periodic Auto-Pruning Janitor (runs every 6 hours if enabled)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
				e.cfgMu.RLock()
				autoPrune := e.cfg.AutoPruneDead
				pruneDays := e.cfg.AutoPruneDays
				e.cfgMu.RUnlock()

				if autoPrune && pruneDays > 0 && e.store != nil {
					cutoff := time.Now().AddDate(0, 0, -pruneDays)
					n, err := e.store.PruneTorrents(db.PruneOptions{
						MaxSeeders:     0,
						OlderThan:      cutoff,
						MinScrapeFails: 2,
					})
					if err == nil && n > 0 {
						log.Printf("[Auto-Prune] Removed %d dead torrents older than %d days (verified dead via scraper)", n, pruneDays)
					}
				}
			}
		}
	}()
}

// Close gracefully stops all background engines, closes database handles, and checkpoints WAL.
func (e *Engine) Close() error {
	e.cancel()

	if e.crawler != nil {
		e.crawler.Stop()
	}
	if e.ingester != nil {
		e.ingester.Stop()
	}
	if e.scraper != nil {
		e.scraper.Stop()
	}
	if e.trending != nil {
		e.trending.Stop()
	}

	e.wg.Wait()

	if e.store != nil {
		return e.store.Close()
	}
	return nil
}

// Config returns a copy of the current active engine configuration.
func (e *Engine) Config() config.Config {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg
}

// Store provides direct access to the database for outbound formatting engines (Torznab / Feed).
func (e *Engine) Store() *db.Store {
	return e.store
}

// UpdateConfig dynamically updates configurations across all internal worker subsystems.
func (e *Engine) UpdateConfig(newCfg config.Config) error {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()

	e.cfg = newCfg

	if e.prowl != nil {
		e.prowl.BaseURL = newCfg.ProwlarrURL
		e.prowl.APIKey = newCfg.ProwlarrAPIKey
	}

	if e.crawler != nil {
		e.crawler.SetFilterFunc(newCfg.MatchesRetentionFilter)
		e.crawler.SetRateLimits(newCfg.MaxDownloadRate, newCfg.MaxUploadRate)
		e.crawler.SetMonthlyLimit(newCfg.MonthlyDataLimitBytes)
	}

	if e.ingester != nil {
		e.ingester.SetFilterFunc(newCfg.MatchesRetentionFilter)
		e.ingester.UpdateFeeds(newCfg.ExternalFeeds)
	}

	if e.scraper != nil {
		e.scraper.SetTrackers(newCfg.ScraperTrackers)
	}

	if e.trending != nil {
		e.trending.UpdateConfig(newCfg)
		if newCfg.TrendingEnabled {
			e.trending.Start()
		} else {
			e.trending.Stop()
		}
	}

	if e.downloader != nil {
		e.downloader.UpdateClients(newCfg.DownloadClients)
	}

	return nil
}

// Search queries the underlying torrent catalogue with filtering and pagination.
func (e *Engine) Search(ctx context.Context, filter db.SearchFilter) ([]db.TorrentRecord, int64, error) {
	if e.store == nil {
		return nil, 0, fmt.Errorf("database store not available")
	}
	return e.store.SearchWithFilter(filter)
}

// SearchProwlarr aggregates searches across remote Prowlarr indexers.
func (e *Engine) SearchProwlarr(query string, limit int) ([]prowlarr.SearchResultItem, error) {
	if e.prowl == nil {
		return nil, fmt.Errorf("prowlarr client not configured")
	}
	return e.prowl.Search(query, limit)
}

// SearchAndIndexProwlarr queries Prowlarr, filters results against retention rules, and indexes new torrents.
func (e *Engine) SearchAndIndexProwlarr(ctx context.Context, query string, limit int) (int, int, error) {
	if e.prowl == nil || e.cfg.ProwlarrAPIKey == "" {
		return 0, 0, fmt.Errorf("Prowlarr is not configured")
	}
	items, err := e.prowl.Search(query, limit)
	if err != nil {
		return 0, 0, err
	}

	e.cfgMu.RLock()
	filterFunc := e.cfg.MatchesRetentionFilter
	e.cfgMu.RUnlock()

	addedCount := 0
	for _, item := range items {
		rec := item.ToRecord()
		if rec.InfoHash == "" || rec.Name == "" {
			continue
		}
		if !filterFunc(rec.Category, rec.Name) {
			continue
		}
		if e.store != nil {
			exists, _ := e.store.Exists(rec.InfoHash)
			if !exists {
				addedCount++
			}
			_ = e.store.UpsertTorrent(&rec)
		}
	}
	return len(items), addedCount, nil
}

// GetTorrent fetches a single torrent by its 40-character hex infohash.
func (e *Engine) GetTorrent(hash string) (*db.TorrentRecord, error) {
	if e.store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	return e.store.GetTorrentByHash(hash)
}

// DeleteTorrent removes a torrent from the database and search index.
func (e *Engine) DeleteTorrent(hash string) error {
	if e.store == nil {
		return fmt.Errorf("database store not available")
	}
	return e.store.DeleteTorrent(hash)
}

// ResolveMetadata resolves torrent metadata from wire peers with fallback queueing.
func (e *Engine) ResolveMetadata(ctx context.Context, hash string) (*db.TorrentRecord, error) {
	if e.crawler == nil {
		return nil, fmt.Errorf("crawler engine not active")
	}

	rec, _ := e.GetTorrent(hash)
	var trackers []string
	if rec != nil && rec.MagnetURI != "" {
		if u, err := url.Parse(rec.MagnetURI); err == nil {
			trackers = u.Query()["tr"]
		}
	}

	resolveCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	updatedRec, err := e.crawler.ResolveMetadataNow(resolveCtx, hash, trackers)
	if err == nil && updatedRec != nil {
		return updatedRec, nil
	}

	// Fallback to asynchronous resolution
	e.crawler.EnqueueMetadataFetch(hash, trackers)
	if rec != nil {
		return rec, nil
	}
	return nil, err
}

// EnqueueMetadataFetch queues an infohash for background BEP 9 metadata resolution.
func (e *Engine) EnqueueMetadataFetch(hash string, trackers []string) {
	if e.crawler != nil {
		e.crawler.EnqueueMetadataFetch(hash, trackers)
	}
}

// ScrapeTorrent performs live tracker health verification for a single torrent.
func (e *Engine) ScrapeTorrent(ctx context.Context, hash string) (*scraper.ScrapeResult, error) {
	if e.scraper == nil {
		return nil, fmt.Errorf("scraper engine not initialized")
	}
	return e.scraper.ScrapeSingle(ctx, hash)
}

// ScrapeBatch performs batch UDP tracker scraping for up to 70 infohashes.
func (e *Engine) ScrapeBatch(ctx context.Context, hashes []string) map[string]scraper.ScrapeResult {
	if e.scraper == nil {
		return map[string]scraper.ScrapeResult{}
	}
	return e.scraper.ScrapeHashes(ctx, hashes)
}

// TriggerTrendingHarvest triggers an immediate harvest across enabled scrape providers.
func (e *Engine) TriggerTrendingHarvest(ctx context.Context) (int, error) {
	if e.trending == nil {
		return 0, fmt.Errorf("trending harvester not available")
	}
	return e.trending.Harvest(ctx)
}

// TrendingStats returns operational statistics for the trending harvester.
func (e *Engine) TrendingStats() ingester.TrendingHarvesterStats {
	if e.trending == nil {
		return ingester.TrendingHarvesterStats{}
	}
	return e.trending.Stats()
}

// ScraperStats returns operational metrics for the live UDP scraper.
func (e *Engine) ScraperStats() scraper.ScraperStats {
	if e.scraper == nil {
		return scraper.ScraperStats{}
	}
	return e.scraper.Stats()
}

// ImportCSV imports torrent records from CSV text.
func (e *Engine) ImportCSV(data []byte, sourceName string) (int, error) {
	if e.ingester == nil {
		return 0, fmt.Errorf("ingester not available")
	}
	return e.ingester.ImportCSV(data, sourceName)
}

// ImportDirect imports a single magnet link or raw infohash.
func (e *Engine) ImportDirect(input, name string) (*db.TorrentRecord, error) {
	if e.ingester == nil {
		return nil, fmt.Errorf("ingester not available")
	}
	return e.ingester.ImportDirect(input, name)
}

// SendToClient resolves a torrent, synthesizes magnet URI if needed, and dispatches to download client.
func (e *Engine) SendToClient(ctx context.Context, clientID, hash, magnet string, opts downloader.AddOptions) (*config.DownloadClientConfig, error) {
	if e.downloader == nil {
		return nil, fmt.Errorf("download manager not configured")
	}

	hash = strings.TrimSpace(hash)
	magnet = strings.TrimSpace(magnet)

	if magnet == "" && hash != "" {
		rec, _ := e.GetTorrent(hash)
		if rec != nil && rec.MagnetURI != "" {
			magnet = rec.MagnetURI
			if opts.Name == "" {
				opts.Name = rec.Name
			}
			if opts.Category == "" {
				opts.Category = rec.Category
			}
		} else {
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(opts.Name))
		}
	}

	if magnet == "" {
		return nil, fmt.Errorf("missing magnet URI or valid infohash")
	}
	opts.MagnetURI = magnet

	client, clientCfg, err := e.downloader.GetClient(clientID)
	if err != nil {
		return nil, err
	}

	if err := client.AddTorrent(ctx, opts); err != nil {
		return nil, fmt.Errorf("failed to add torrent to %s: %w", client.Name(), err)
	}

	return clientCfg, nil
}

// PruneTorrents removes dead or filter-violating torrents.
func (e *Engine) PruneTorrents(opts db.PruneOptions) (int64, error) {
	if e.store == nil {
		return 0, fmt.Errorf("database store not available")
	}
	return e.store.PruneTorrents(opts)
}

// CountDeadTorrents counts torrents with no seeders older than cutoff.
func (e *Engine) CountDeadTorrents(maxSeeders int, olderThan time.Time) (int64, error) {
	if e.store == nil {
		return 0, fmt.Errorf("database store not available")
	}
	return e.store.CountDeadTorrents(maxSeeders, olderThan)
}

// CountFilterViolations counts torrents that violate the given filter rules.
func (e *Engine) CountFilterViolations(rules db.FilterRules) (int64, error) {
	if e.store == nil {
		return 0, fmt.Errorf("database store not available")
	}
	return e.store.CountFilterViolations(rules)
}

// PurgeFilterViolations purges torrents that violate the given filter rules.
func (e *Engine) PurgeFilterViolations(rules db.FilterRules) (int64, error) {
	if e.store == nil {
		return 0, fmt.Errorf("database store not available")
	}
	return e.store.PruneFilterViolations(rules)
}

// ShrinkMemory compacts SQLite cache and releases virtual memory to OS.
func (e *Engine) ShrinkMemory() (sysinfo.MemoryStats, error) {
	if e.store != nil {
		e.store.PruneMemory()
	}
	sysinfo.FreeMemory()
	return sysinfo.GetMemoryStats(), nil
}

// EngineStats represents consolidated operational statistics for the system.
type EngineStats struct {
	DB     db.Stats           `json:"db"`
	DHT    dhtcrawler.Metrics `json:"dht"`
	Memory sysinfo.MemoryStats `json:"memory"`
}

// Stats returns system and indexer metrics.
func (e *Engine) Stats() (EngineStats, error) {
	var res EngineStats
	if e.store != nil {
		dbStats, err := e.store.GetStats()
		if err != nil {
			return res, err
		}
		res.DB = dbStats
	}
	if e.crawler != nil {
		res.DHT = e.crawler.Metrics()
	}
	res.Memory = sysinfo.GetMemoryStats()
	return res, nil
}

// Categories returns the distinct category list from the store.
func (e *Engine) Categories() ([]string, error) {
	if e.store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	return e.store.GetCategories()
}

// Sources returns the distinct source list from the store.
func (e *Engine) Sources() ([]string, error) {
	if e.store == nil {
		return nil, fmt.Errorf("database store not available")
	}
	return e.store.GetSources()
}

// FeedStatuses returns operational tracking status for all configured external feeds.
func (e *Engine) FeedStatuses() []ingester.FeedStatus {
	if e.ingester == nil {
		return nil
	}
	return e.ingester.GetStatuses()
}

// SaveFeed adds or updates an external feed configuration.
func (e *Engine) SaveFeed(f config.FeedConfig) {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()

	found := false
	for i, existing := range e.cfg.ExternalFeeds {
		if existing.ID == f.ID {
			e.cfg.ExternalFeeds[i] = f
			found = true
			break
		}
	}
	if !found {
		e.cfg.ExternalFeeds = append(e.cfg.ExternalFeeds, f)
	}
	if e.ingester != nil {
		e.ingester.UpdateFeeds(e.cfg.ExternalFeeds)
	}
}

// Feeds returns the configured external feeds.
func (e *Engine) Feeds() []config.FeedConfig {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg.ExternalFeeds
}

// DeleteFeed removes an external feed by ID.
func (e *Engine) DeleteFeed(id string) bool {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()

	found := false
	var updated []config.FeedConfig
	for _, f := range e.cfg.ExternalFeeds {
		if f.ID == id {
			found = true
		} else {
			updated = append(updated, f)
		}
	}
	e.cfg.ExternalFeeds = updated
	if e.ingester != nil {
		e.ingester.UpdateFeeds(updated)
	}
	return found
}

// SyncFeed forces immediate synchronization of a single feed by ID.
func (e *Engine) SyncFeed(ctx context.Context, id string) (int, error) {
	if e.ingester == nil {
		return 0, fmt.Errorf("feed ingester not initialized")
	}
	return e.ingester.SyncFeed(ctx, id)
}

// SyncAllFeeds forces immediate synchronization across all enabled external feeds.
func (e *Engine) SyncAllFeeds(ctx context.Context) (int, []error) {
	if e.ingester == nil {
		return 0, []error{fmt.Errorf("feed ingester not initialized")}
	}
	return e.ingester.SyncAll(ctx)
}

// DownloadClients returns the configured download clients.
func (e *Engine) DownloadClients() []config.DownloadClientConfig {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg.DownloadClients
}

// TestDownloadClient verifies connectivity with a target download client.
func (e *Engine) TestDownloadClient(ctx context.Context, clientCfg config.DownloadClientConfig) error {
	if e.downloader == nil {
		return fmt.Errorf("download manager not initialized")
	}
	return e.downloader.TestConfig(ctx, clientCfg)
}
