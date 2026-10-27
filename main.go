package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/dhtcrawler"
	"local-indexer/internal/feed"
	"local-indexer/internal/ingester"
	"local-indexer/internal/prowlarr"
	"local-indexer/internal/sysinfo"
	"local-indexer/internal/torznab"
)

const Version = "1.2.2"

//go:embed web/static/*
var staticFS embed.FS

type Server struct {
	cfg      config.Config
	cfgPath  string
	store    *db.Store
	crawler  *dhtcrawler.Crawler
	torznab  *torznab.Server
	prowl    *prowlarr.Client
	feedGen  *feed.Generator
	ingester *ingester.Ingester
}

func main() {
	cfgPath := "config.json"
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Initialize SQLite store with WAL mode and FTS5
	store, err := db.NewStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("Error opening database store: %v", err)
	}
	defer store.Close()

	// Initialize passive DHT crawler
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
		log.Printf("[Warning] DHT Crawler failed to start: %v (running in aggregator/torznab mode)", err)
	} else {
		crawler.Start()
		defer crawler.Stop()
	}

	// Initialize Torznab inbound server
	baseURL := fmt.Sprintf("http://localhost:%d", cfg.WebPort)
	torznabServer := torznab.NewServer(store, cfg.TorznabAPIKey, baseURL)

	// Initialize Prowlarr client
	prowlClient := prowlarr.NewClient(cfg.ProwlarrURL, cfg.ProwlarrAPIKey)

	// Initialize Dynamic Feed Generator
	feedGen := feed.NewGenerator(store, baseURL)

	// Initialize External Feed Ingester
	feedIngester := ingester.NewIngester(store, cfg.ExternalFeeds)
	feedIngester.Start()
	defer feedIngester.Stop()

	srv := &Server{
		cfg:      cfg,
		cfgPath:  cfgPath,
		store:    store,
		crawler:  crawler,
		torznab:  torznabServer,
		prowl:    prowlClient,
		feedGen:  feedGen,
		ingester: feedIngester,
	}

	if crawler != nil {
		crawler.SetFilterFunc(srv.cfg.MatchesRetentionFilter)
	}
	if feedIngester != nil {
		feedIngester.SetFilterFunc(srv.cfg.MatchesRetentionFilter)
	}

	// Periodic Memory Janitor (runs every 10 min: checkpoints WAL, shrinks cache, frees OS memory)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			store.PruneMemory()
			sysinfo.FreeMemory()
		}
	}()

	// Periodic Auto-Pruning Janitor (runs every 6 hours if enabled)
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if srv.cfg.AutoPruneDead && srv.cfg.AutoPruneDays > 0 {
				cutoff := time.Now().AddDate(0, 0, -srv.cfg.AutoPruneDays)
				n, err := store.PruneTorrents(db.PruneOptions{MaxSeeders: 0, OlderThan: cutoff})
				if err == nil && n > 0 {
					log.Printf("[Auto-Prune] Removed %d dead torrents older than %d days", n, srv.cfg.AutoPruneDays)
				}
			}
		}
	}()

	mux := http.NewServeMux()

	// RSS 2.0 & Atom Feed Endpoints
	mux.HandleFunc("/api/v1/feed/rss", srv.feedGen.HandleRSS)
	mux.HandleFunc("/api/v1/feed", srv.feedGen.HandleRSS)
	mux.HandleFunc("/rss", srv.feedGen.HandleRSS)
	mux.HandleFunc("/api/v1/feed/atom", srv.feedGen.HandleAtom)
	mux.HandleFunc("/atom", srv.feedGen.HandleAtom)

	// Torznab endpoints (supports exact, trailing slash, /api suffix for Prowlarr/Radarr/Sonarr, and root /api)
	mux.HandleFunc("/api/v1/torznab", srv.torznab.HandleTorznab)
	mux.HandleFunc("/api/v1/torznab/", srv.torznab.HandleTorznab)
	mux.HandleFunc("/torznab", srv.torznab.HandleTorznab)
	mux.HandleFunc("/torznab/", srv.torznab.HandleTorznab)
	mux.HandleFunc("/api", srv.torznab.HandleTorznab)

	// Direct single-torrent API and download endpoints
	mux.HandleFunc("/api/v1/torrent", srv.handleTorrentRoute)
	mux.HandleFunc("/api/v1/torrents/prune", srv.handlePruneTorrents)
	mux.HandleFunc("/api/v1/torrents/dead-count", srv.handleDeadCount)
	mux.HandleFunc("/api/v1/torrents/filter-preview", srv.handleFilterPreview)
	mux.HandleFunc("/api/v1/torrents/filter-purge", srv.handleFilterPurge)
	mux.HandleFunc("/api/v1/memory/shrink", srv.handleMemoryShrink)
	mux.HandleFunc("/api/v1/feeds", srv.handleFeeds)
	mux.HandleFunc("/api/v1/feeds/delete", srv.handleFeedsDelete)
	mux.HandleFunc("/api/v1/feeds/sync", srv.handleFeedsSync)
	mux.HandleFunc("/api/v1/import", srv.handleImport)

	mux.HandleFunc("/download/", srv.handleDownload)
	mux.HandleFunc("/api/v1/download", srv.handleDownload)
	mux.HandleFunc("/api/v1/download/", srv.handleDownload)
	mux.HandleFunc("/torrent/", func(w http.ResponseWriter, r *http.Request) {
		hash := strings.TrimPrefix(r.URL.Path, "/torrent/")
		http.Redirect(w, r, "/#"+hash, http.StatusFound)
	})

	// REST APIs for UI
	mux.HandleFunc("/api/v1/search", srv.handleSearch)
	mux.HandleFunc("/api/v1/prowlarr/search", srv.handleProwlarrSearch)
	mux.HandleFunc("/api/v1/stats", srv.handleStats)
	mux.HandleFunc("/api/v1/categories", srv.handleCategories)
	mux.HandleFunc("/api/v1/dht/graph", srv.handleDHTGraph)
	mux.HandleFunc("/api/v1/config", srv.handleConfig)

	// Static UI assets: preferentially serve from local disk if web/static exists for instant hot reloading without rebuild
	var liveFS fs.FS
	if info, err := os.Stat("web/static"); err == nil && info.IsDir() {
		liveFS = os.DirFS("web/static")
	} else {
		subFS, err := fs.Sub(staticFS, "web/static")
		if err != nil {
			log.Fatalf("Error sub static fs: %v", err)
		}
		liveFS = subFS
	}

	fileServer := http.FileServer(http.FS(liveFS))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// fallback to static file if exists or 404
			fileServer.ServeHTTP(w, r)
			return
		}
		indexBytes, _ := fs.ReadFile(liveFS, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.WebPort),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("=================================================================")
		log.Printf("⚡ OmniDex - Hybrid DHT Indexer & Torznab Bridge v%s is running!", Version)
		log.Printf("🌐 Web Dashboard:   http://localhost:%d", cfg.WebPort)
		log.Printf("📡 Torznab Endpoint: http://localhost:%d/api/v1/torznab?apikey=%s", cfg.WebPort, cfg.TorznabAPIKey)
		log.Printf("💾 Database:         %s (FTS5 enabled)", cfg.DBPath)
		log.Printf("=================================================================")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	category := r.URL.Query().Get("category")
	source := r.URL.Query().Get("source")
	resolution := r.URL.Query().Get("resolution")
	codec := r.URL.Query().Get("codec")
	quality := r.URL.Query().Get("quality")
	releaseGroup := r.URL.Query().Get("release_group")
	includeSuspicious := r.URL.Query().Get("include_suspicious") == "true" || r.URL.Query().Get("suspicious") == "true"
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = r.URL.Query().Get("sort_by")
	}
	minSize, _ := strconv.ParseInt(r.URL.Query().Get("min_size"), 10, 64)
	maxSize, _ := strconv.ParseInt(r.URL.Query().Get("max_size"), 10, 64)

	page := 1
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	if pageParam, _ := strconv.Atoi(r.URL.Query().Get("page")); pageParam > 0 {
		page = pageParam
		offset = (page - 1) * limit
	} else if limit > 0 {
		page = (offset / limit) + 1
	}

	filter := db.SearchFilter{
		Query:             q,
		Category:          category,
		Source:            source,
		Resolution:        resolution,
		Codec:             codec,
		Quality:           quality,
		ReleaseGroup:      releaseGroup,
		IncludeSuspicious: includeSuspicious,
		MinSize:           minSize,
		MaxSize:           maxSize,
		SortBy:            sortBy,
		Limit:             limit,
		Offset:            offset,
	}

	results, total, err := s.store.SearchWithFilter(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	totalPages := 0
	if total > 0 && limit > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	resp := map[string]any{
		"items":       results,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"offset":      offset,
		"total_pages": totalPages,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.GetCategories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cats)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	dbStats, _ := s.store.GetStats()
	var crawlerMetrics dhtcrawler.Metrics
	if s.crawler != nil {
		crawlerMetrics = s.crawler.Metrics()
	}
	memStats := sysinfo.GetMemoryStats()

	resp := map[string]any{
		"version": Version,
		"db":      dbStats,
		"dht":     crawlerMetrics,
		"memory":  memStats,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDHTGraph(w http.ResponseWriter, r *http.Request) {
	var graph dhtcrawler.DHTGraphData
	if s.crawler != nil {
		graph = s.crawler.GetGraphData()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(graph)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		// Obfuscate secret slightly for safety
		safeCfg := s.cfg
		_ = json.NewEncoder(w).Encode(safeCfg)
		return
	}

	if r.Method == http.MethodPost {
		var update struct {
			ProwlarrURL           string `json:"prowlarr_url"`
			ProwlarrAPIKey        string `json:"prowlarr_api_key"`
			TorznabAPIKey         string `json:"torznab_api_key"`
			DHTWorkers            int    `json:"dht_workers"`
			MaxDownloadRate       *int64 `json:"max_download_rate"`
			MaxUploadRate         *int64 `json:"max_upload_rate"`
			MonthlyDataLimitBytes *int64 `json:"monthly_data_limit_bytes"`
			AutoPruneDead         *bool    `json:"auto_prune_dead"`
			AutoPruneDays         *int     `json:"auto_prune_days"`
			FilterEnabled         *bool    `json:"filter_enabled"`
			FilterCategoryMode    string   `json:"filter_category_mode"`
			FilterCategories      []string `json:"filter_categories"`
			FilterKeywords        []string `json:"filter_keywords"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if update.ProwlarrURL != "" {
			s.cfg.ProwlarrURL = update.ProwlarrURL
			s.prowl.BaseURL = update.ProwlarrURL
		}
		if update.ProwlarrAPIKey != "" {
			s.cfg.ProwlarrAPIKey = update.ProwlarrAPIKey
			s.prowl.APIKey = update.ProwlarrAPIKey
		}
		if update.TorznabAPIKey != "" {
			s.cfg.TorznabAPIKey = update.TorznabAPIKey
			s.torznab = torznab.NewServer(s.store, s.cfg.TorznabAPIKey, fmt.Sprintf("http://localhost:%d", s.cfg.WebPort))
		}
		if update.DHTWorkers > 0 {
			s.cfg.DHTWorkers = update.DHTWorkers
		}
		if update.MaxDownloadRate != nil {
			dl := *update.MaxDownloadRate
			if dl < 0 {
				dl = 0
			}
			s.cfg.MaxDownloadRate = dl
		}
		if update.MaxUploadRate != nil {
			ul := *update.MaxUploadRate
			if ul < 0 {
				ul = 0
			}
			s.cfg.MaxUploadRate = ul
		}
		if update.MonthlyDataLimitBytes != nil {
			ml := *update.MonthlyDataLimitBytes
			if ml < 0 {
				ml = 0
			}
			s.cfg.MonthlyDataLimitBytes = ml
			if s.crawler != nil {
				s.crawler.SetMonthlyLimit(ml)
			}
		}
		if update.AutoPruneDead != nil {
			s.cfg.AutoPruneDead = *update.AutoPruneDead
		}
		if update.AutoPruneDays != nil && *update.AutoPruneDays > 0 {
			s.cfg.AutoPruneDays = *update.AutoPruneDays
		}

		if update.FilterEnabled != nil {
			s.cfg.FilterEnabled = *update.FilterEnabled
		}
		if update.FilterCategoryMode != "" {
			s.cfg.FilterCategoryMode = update.FilterCategoryMode
		}
		if update.FilterCategories != nil {
			s.cfg.FilterCategories = update.FilterCategories
		}
		if update.FilterKeywords != nil {
			s.cfg.FilterKeywords = update.FilterKeywords
		}

		if s.crawler != nil {
			s.crawler.SetFilterFunc(s.cfg.MatchesRetentionFilter)
		}
		if s.ingester != nil {
			s.ingester.SetFilterFunc(s.cfg.MatchesRetentionFilter)
		}

		if s.crawler != nil && (update.MaxDownloadRate != nil || update.MaxUploadRate != nil) {
			s.crawler.SetRateLimits(s.cfg.MaxDownloadRate, s.cfg.MaxUploadRate)
		}

		_ = config.SaveConfig(s.cfgPath, s.cfg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleTorrentRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		hash := strings.TrimSpace(r.URL.Query().Get("hash"))
		if hash == "" {
			hash = strings.TrimSpace(r.URL.Query().Get("info_hash"))
		}
		if hash == "" {
			http.Error(w, "missing hash parameter", http.StatusBadRequest)
			return
		}
		if err := s.store.DeleteTorrent(hash); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "deleted": hash})
		return
	}
	s.handleGetTorrent(w, r)
}

func (s *Server) handlePruneTorrents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Days       int    `json:"days"`
		MaxSeeders int    `json:"max_seeders"`
		Source     string `json:"source"`
		PruneSpam  bool   `json:"prune_spam"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var cutoff time.Time
	if req.Days > 0 {
		cutoff = time.Now().AddDate(0, 0, -req.Days)
	}

	opts := db.PruneOptions{
		MaxSeeders: req.MaxSeeders,
		OlderThan:  cutoff,
		Source:     req.Source,
		PruneSpam:  req.PruneSpam,
	}

	affected, err := s.store.PruneTorrents(opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"deleted": affected,
	})
}

func (s *Server) handleDeadCount(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	maxSeeders, _ := strconv.Atoi(r.URL.Query().Get("max_seeders"))
	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().AddDate(0, 0, -days)
	}
	count, err := s.store.CountDeadTorrents(maxSeeders, cutoff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"count": count})
}

func (s *Server) handleFilterPreview(w http.ResponseWriter, r *http.Request) {
	rules := db.FilterRules{
		Enabled:      s.cfg.FilterEnabled,
		CategoryMode: s.cfg.FilterCategoryMode,
		Categories:   s.cfg.FilterCategories,
		Keywords:     s.cfg.FilterKeywords,
	}

	if r.URL.Query().Get("enabled") != "" {
		rules.Enabled = r.URL.Query().Get("enabled") == "true"
	}
	if mode := r.URL.Query().Get("category_mode"); mode != "" {
		rules.CategoryMode = mode
	}
	if cats := r.URL.Query().Get("categories"); cats != "" {
		rules.Categories = strings.Split(cats, ",")
	}
	if kws := r.URL.Query().Get("keywords"); kws != "" {
		rules.Keywords = strings.Split(kws, ",")
	}

	count, err := s.store.CountFilterViolations(rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"violations": count,
		"rules":      rules,
	})
}

func (s *Server) handleFilterPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rules := db.FilterRules{
		Enabled:      s.cfg.FilterEnabled,
		CategoryMode: s.cfg.FilterCategoryMode,
		Categories:   s.cfg.FilterCategories,
		Keywords:     s.cfg.FilterKeywords,
	}

	var req struct {
		Enabled      *bool    `json:"filter_enabled"`
		CategoryMode string   `json:"filter_category_mode"`
		Categories   []string `json:"filter_categories"`
		Keywords     []string `json:"filter_keywords"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		if req.Enabled != nil {
			rules.Enabled = *req.Enabled
		}
		if req.CategoryMode != "" {
			rules.CategoryMode = req.CategoryMode
		}
		if req.Categories != nil {
			rules.Categories = req.Categories
		}
		if req.Keywords != nil {
			rules.Keywords = req.Keywords
		}
	}

	deleted, err := s.store.PruneFilterViolations(rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"deleted": deleted,
	})
}

func (s *Server) handleMemoryShrink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.store.PruneMemory()
	sysinfo.FreeMemory()
	mem := sysinfo.GetMemoryStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"memory": mem,
	})
}

func (s *Server) deleteFeedByID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	found := false
	var updated []config.FeedConfig
	for _, f := range s.cfg.ExternalFeeds {
		if f.ID == id {
			found = true
		} else {
			updated = append(updated, f)
		}
	}
	s.cfg.ExternalFeeds = updated
	_ = config.SaveConfig(s.cfgPath, s.cfg)
	s.ingester.UpdateFeeds(s.cfg.ExternalFeeds)
	return found
}

func (s *Server) handleFeedsDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" && r.Body != nil {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = strings.TrimSpace(req.ID)
	}
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	s.deleteFeedByID(id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "deleted": id})
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		statuses := s.ingester.GetStatuses()
		_ = json.NewEncoder(w).Encode(statuses)
		return
	}

	if r.Method == http.MethodDelete || (r.Method == http.MethodPost && r.URL.Query().Get("action") == "delete") {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}
		s.deleteFeedByID(id)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	if r.Method == http.MethodPost {
		var f config.FeedConfig
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if f.ID == "" {
			f.ID = fmt.Sprintf("feed-%d", time.Now().UnixNano())
		}
		if f.Name == "" {
			f.Name = "Custom Feed"
		}
		if f.IntervalMinutes <= 0 {
			f.IntervalMinutes = 30
		}

		found := false
		for i, existing := range s.cfg.ExternalFeeds {
			if existing.ID == f.ID {
				s.cfg.ExternalFeeds[i] = f
				found = true
				break
			}
		}
		if !found {
			s.cfg.ExternalFeeds = append(s.cfg.ExternalFeeds, f)
		}
		_ = config.SaveConfig(s.cfgPath, s.cfg)
		s.ingester.UpdateFeeds(s.cfg.ExternalFeeds)

		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "feed": f})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleFeedsSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	w.Header().Set("Content-Type", "application/json")
	if id != "" {
		added, err := s.ingester.SyncFeed(r.Context(), id)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": err.Error(), "added": added})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "added": added})
		return
	}

	added, errs := s.ingester.SyncAll(r.Context())
	var errStrs []string
	for _, e := range errs {
		errStrs = append(errStrs, e.Error())
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"added":  added,
		"errors": errStrs,
	})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Input string `json:"input"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rec, err := s.ingester.ImportDirect(req.Input, req.Name)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "torrent": rec})
}

func (s *Server) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		hash = strings.TrimSpace(r.URL.Query().Get("info_hash"))
	}
	if hash == "" {
		http.Error(w, "missing hash parameter", http.StatusBadRequest)
		return
	}
	rec, err := s.store.GetTorrentByHash(hash)
	if err != nil || rec == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rec)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	hash := strings.TrimPrefix(path, "/download/")
	hash = strings.TrimPrefix(hash, "/api/v1/download/")
	hash = strings.TrimSpace(hash)
	if hash == "" {
		hash = strings.TrimSpace(r.URL.Query().Get("hash"))
	}
	if hash == "" {
		hash = strings.TrimSpace(r.URL.Query().Get("info_hash"))
	}
	hash = strings.TrimSuffix(hash, ".torrent")

	rec, err := s.store.GetTorrentByHash(hash)
	if err != nil || rec == nil {
		http.Error(w, "Torrent not found", http.StatusNotFound)
		return
	}

	magnet := rec.MagnetURI
	if magnet == "" {
		magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", rec.InfoHash, rec.Name)
	}

	raw := r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("magnet") == "1"
	accept := r.Header.Get("Accept")
	userAgent := strings.ToLower(r.Header.Get("User-Agent"))
	isAutomated := strings.Contains(userAgent, "sonarr") || strings.Contains(userAgent, "radarr") || strings.Contains(userAgent, "qbittorrent") || strings.Contains(userAgent, "transmission") || strings.Contains(userAgent, "deluge")

	if raw || isAutomated || (accept != "" && !strings.Contains(accept, "text/html")) {
		http.Redirect(w, r, magnet, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Download %s - OmniDex</title>
  <style>
    body {
      margin: 0;
      padding: 0;
      background: #090d16;
      color: #f8fafc;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .card {
      background: rgba(15, 23, 42, 0.9);
      border: 1px solid rgba(255, 255, 255, 0.12);
      border-radius: 16px;
      padding: 32px;
      max-width: 580px;
      width: 90%%;
      box-shadow: 0 20px 40px rgba(0,0,0,0.6);
      text-align: center;
    }
    .badge {
      display: inline-block;
      padding: 4px 12px;
      border-radius: 9999px;
      background: rgba(6, 182, 212, 0.15);
      color: #06b6d4;
      font-size: 0.8rem;
      font-weight: 600;
      margin-bottom: 16px;
    }
    h1 {
      font-size: 1.25rem;
      margin: 0 0 12px 0;
      word-break: break-word;
      line-height: 1.4;
    }
    .hash {
      font-family: monospace;
      font-size: 0.8rem;
      color: #94a3b8;
      background: rgba(0,0,0,0.3);
      padding: 6px 10px;
      border-radius: 6px;
      margin-bottom: 24px;
      word-break: break-all;
    }
    .btn-group {
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .btn {
      display: inline-block;
      padding: 12px 20px;
      border-radius: 10px;
      text-decoration: none;
      font-weight: 600;
      font-size: 0.95rem;
      cursor: pointer;
      transition: all 0.2s;
      border: none;
    }
    .btn-primary {
      background: #3b82f6;
      color: #fff;
    }
    .btn-primary:hover {
      background: #2563eb;
    }
    .btn-secondary {
      background: rgba(255, 255, 255, 0.08);
      color: #cbd5e1;
      border: 1px solid rgba(255, 255, 255, 0.15);
    }
    .btn-secondary:hover {
      background: rgba(255, 255, 255, 0.15);
      color: #fff;
    }
    .info {
      font-size: 0.8rem;
      color: #64748b;
      margin-top: 16px;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="badge">⚡ OmniDex Magnet Launcher</div>
    <h1>%s</h1>
    <div class="hash">%s</div>
    <div class="btn-group">
      <a href="%s" class="btn btn-primary" id="btn-open-magnet">🧲 Open in Torrent Client</a>
      <button class="btn btn-secondary" onclick="navigator.clipboard.writeText('%s'); this.textContent = 'Copied Magnet URI!';">📋 Copy Magnet Link</button>
      <a href="/#%s" class="btn btn-secondary">🔍 View on OmniDex Dashboard</a>
    </div>
    <div class="info">Opening magnet in your torrent client automatically...</div>
  </div>
  <script>
    setTimeout(function() {
      window.location.href = "%s";
    }, 100);
  </script>
</body>
</html>`, html.EscapeString(rec.Name), html.EscapeString(rec.Name), rec.InfoHash, magnet, magnet, rec.InfoHash, magnet)
	_, _ = w.Write([]byte(htmlContent))
}

func (s *Server) handleProwlarrSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "added": 0, "count": 0})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	if s.prowl == nil || s.cfg.ProwlarrAPIKey == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "unconfigured",
			"error":  "Prowlarr is not configured",
			"added":  0,
			"count":  0,
		})
		return
	}

	prowlResults, err := s.prowl.Search(q, limit)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  err.Error(),
			"added":  0,
			"count":  0,
		})
		return
	}

	addedCount := 0
	for _, item := range prowlResults {
		rec := item.ToRecord()
		if rec.InfoHash == "" || rec.Name == "" {
			// Skip truly empty/unusable records (no guid, no download URL, no title)
			continue
		}
		if !s.cfg.MatchesRetentionFilter(rec.Category, rec.Name) {
			continue
		}
		exists, _ := s.store.Exists(rec.InfoHash)
		if !exists {
			addedCount++
		}
		_ = s.store.UpsertTorrent(&rec)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"query":  q,
		"count":  len(prowlResults),
		"added":  addedCount,
	})
}

