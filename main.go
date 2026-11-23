package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"local-indexer/internal/cli"
	"local-indexer/internal/config"
	"local-indexer/internal/core"
	"local-indexer/internal/db"
	"local-indexer/internal/downloader"
	"local-indexer/internal/feed"
	"local-indexer/internal/logger"
	"local-indexer/internal/torznab"
)

const Version = "1.3.9"

//go:embed web/static/*
var staticFS embed.FS

type Server struct {
	cfg       config.Config
	cfgPath   string
	engine    *core.Engine
	torznab   *torznab.Server
	feedGen   *feed.Generator
	appLogger *logger.Logger
}

func main() {
	if handled := cli.HandleArgs(os.Args[1:], Version); handled {
		return
	}

	cfgPath := "config.json"
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Initialize unified rotating logger and crash handler
	appLogger, err := logger.Init(cfg.Logging, Version)
	if err != nil {
		log.Printf("[Warning] Failed to initialize logger: %v", err)
	} else {
		defer appLogger.RecoverAndDump()
		defer appLogger.Close(true)
		appLogger.RegisterSecret(cfg.TorznabAPIKey)
		appLogger.RegisterSecret(cfg.ProwlarrAPIKey)
	}

	if appLogger != nil && appLogger.CrashHandler() != nil && appLogger.CrashHandler().PreviousCrashDetected {
		log.Printf("[Warning] Previous abnormal crash detected! Forensic dump available at: %s", appLogger.CrashHandler().LatestCrashFile)
	}

	// Initialize Deep Core Coordinator Engine
	engine, err := core.NewEngine(cfg)
	if err != nil {
		log.Fatalf("Error opening core engine: %v", err)
	}
	defer engine.Close()
	engine.Start(context.Background())

	// Initialize Torznab inbound server
	baseURLHost := cfg.WebBindAddr
	if baseURLHost == "" || baseURLHost == "0.0.0.0" {
		baseURLHost = "localhost"
	}
	baseURL := fmt.Sprintf("http://%s:%d", baseURLHost, cfg.WebPort)
	torznabServer := torznab.NewServer(engine.Store(), cfg.TorznabAPIKey, baseURL)

	// Initialize Dynamic Feed Generator
	feedGen := feed.NewGenerator(engine.Store(), baseURL)

	srv := &Server{
		cfg:       cfg,
		cfgPath:   cfgPath,
		engine:    engine,
		torznab:   torznabServer,
		feedGen:   feedGen,
		appLogger: appLogger,
	}

	mux := http.NewServeMux()

	// Tracker Scraper endpoints
	mux.HandleFunc("/api/v1/torrent/scrape", srv.handleScrapeTorrent)
	mux.HandleFunc("/api/v1/torrent/scrape-batch", srv.handleScrapeBatch)
	mux.HandleFunc("/api/v1/torrent/resolve-metadata", srv.handleResolveMetadata)
	mux.HandleFunc("/api/v1/scraper/status", srv.handleScraperStatus)
	mux.HandleFunc("/api/v1/trending/harvest", srv.handleTrendingHarvest)
	mux.HandleFunc("/api/v1/trending/status", srv.handleTrendingStatus)

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
	mux.HandleFunc("/api/v1/sources", srv.handleSources)
	mux.HandleFunc("/api/v1/config", srv.handleConfig)
	mux.HandleFunc("/api/v1/clients", srv.handleDownloadClients)
	mux.HandleFunc("/api/v1/clients/test", srv.handleTestDownloadClient)
	mux.HandleFunc("/api/v1/client/send", srv.handleSendToClient)
	mux.HandleFunc("/api/v1/logs", srv.handleGetLogs)
	mux.HandleFunc("/api/v1/logs/download", srv.handleDownloadLogs)
	mux.HandleFunc("/api/v1/logs/crash-download", srv.handleDownloadCrash)
	mux.HandleFunc("/api/v1/logs/clear", srv.handleClearLogs)
	mux.HandleFunc("/api/v1/logs/level", srv.handleSetLogLevel)

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
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		settingsBytes, err := fs.ReadFile(liveFS, "settings.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(settingsBytes)
	})
	mux.HandleFunc("/settings/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings", http.StatusMovedPermanently)
	})
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
		Addr:         fmt.Sprintf("%s:%d", cfg.WebBindAddr, cfg.WebPort),
		Handler:      securityMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("=================================================================")
		log.Printf("⚡ OmniDex - Hybrid DHT Indexer & Torznab Bridge v%s is running!", Version)
		log.Printf("🌐 Web Dashboard:   http://%s:%d", cfg.WebBindAddr, cfg.WebPort)
		log.Printf("📡 Torznab Endpoint: http://%s:%d/api/v1/torznab?apikey=%s", cfg.WebBindAddr, cfg.WebPort, cfg.TorznabAPIKey)
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

func isMaskedSecret(s string) bool {
	return strings.Contains(s, "••••") || strings.Contains(s, "****")
}

func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set defensive security response headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self';")

		// Prevent Cross-Site Request Forgery (CSRF) on state-altering API methods
		if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			path := r.URL.Path
			// Exclude Torznab endpoints which authenticate via API key
			if !strings.HasPrefix(path, "/api/v1/torznab") && !strings.HasPrefix(path, "/torznab") && path != "/api" {
				origin := r.Header.Get("Origin")
				if origin != "" {
					u, err := url.Parse(origin)
					if err != nil || !strings.EqualFold(u.Host, r.Host) {
						http.Error(w, "Forbidden: cross-origin request rejected", http.StatusForbidden)
						return
					}
				} else {
					referer := r.Header.Get("Referer")
					if referer != "" {
						u, err := url.Parse(referer)
						if err != nil || !strings.EqualFold(u.Host, r.Host) {
							http.Error(w, "Forbidden: cross-origin request rejected", http.StatusForbidden)
							return
						}
					}
				}
			}
		}

		next.ServeHTTP(w, r)
	})
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

	results, total, err := s.engine.Search(r.Context(), filter)
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
	cats, err := s.engine.Categories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cats)
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.engine.Sources()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sources)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.engine.Stats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"version": Version,
		"db":      stats.DB,
		"dht":     stats.DHT,
		"memory":  stats.Memory,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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
			FilterCategoryMode        string   `json:"filter_category_mode"`
			FilterCategories          []string `json:"filter_categories"`
			FilterKeywords            []string `json:"filter_keywords"`
			ScraperEnabled            *bool    `json:"scraper_enabled"`
			ScraperIntervalMin        *int     `json:"scraper_interval_min"`
			ScraperBatchSize          *int     `json:"scraper_batch_size"`
			ScraperTrackers           []string `json:"scraper_trackers"`
			ScraperAutoUpdateTrackers *bool                          `json:"scraper_auto_update_trackers"`
			ScraperTrackersURL        string                         `json:"scraper_trackers_url"`
			DownloadClients           []config.DownloadClientConfig  `json:"download_clients"`
			TrendingEnabled           *bool                          `json:"trending_enabled"`
			TrendingIntervalHours     *int                           `json:"trending_interval_hours"`
			TrendingMinSeeds          *int                           `json:"trending_min_seeds"`
			TrendingProviders         map[string]bool                `json:"trending_providers"`
			Logging                   *config.LoggingConfig          `json:"logging"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if update.ProwlarrURL != "" {
			s.cfg.ProwlarrURL = update.ProwlarrURL
		}
		if update.ProwlarrAPIKey != "" {
			s.cfg.ProwlarrAPIKey = update.ProwlarrAPIKey
		}
		if update.TorznabAPIKey != "" {
			s.cfg.TorznabAPIKey = update.TorznabAPIKey
			if s.appLogger != nil {
				s.appLogger.RegisterSecret(update.TorznabAPIKey)
			}
			s.torznab = torznab.NewServer(s.engine.Store(), s.cfg.TorznabAPIKey, fmt.Sprintf("http://localhost:%d", s.cfg.WebPort))
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

		if update.ScraperEnabled != nil {
			s.cfg.ScraperEnabled = *update.ScraperEnabled
		}
		if update.ScraperIntervalMin != nil && *update.ScraperIntervalMin > 0 {
			s.cfg.ScraperIntervalMin = *update.ScraperIntervalMin
		}
		if update.ScraperBatchSize != nil && *update.ScraperBatchSize > 0 {
			s.cfg.ScraperBatchSize = *update.ScraperBatchSize
		}
		if update.ScraperTrackers != nil {
			s.cfg.ScraperTrackers = update.ScraperTrackers
		}
		if update.ScraperAutoUpdateTrackers != nil {
			s.cfg.ScraperAutoUpdateTrackers = *update.ScraperAutoUpdateTrackers
		}
		if update.ScraperTrackersURL != "" {
			s.cfg.ScraperTrackersURL = update.ScraperTrackersURL
		}
		if update.DownloadClients != nil {
			s.cfg.DownloadClients = update.DownloadClients
		}

		if update.TrendingEnabled != nil {
			s.cfg.TrendingEnabled = *update.TrendingEnabled
		}
		if update.TrendingIntervalHours != nil && *update.TrendingIntervalHours > 0 {
			s.cfg.TrendingIntervalHours = *update.TrendingIntervalHours
		}
		if update.TrendingMinSeeds != nil && *update.TrendingMinSeeds >= 0 {
			s.cfg.TrendingMinSeeds = *update.TrendingMinSeeds
		}
		if update.TrendingProviders != nil {
			s.cfg.TrendingProviders = update.TrendingProviders
		}

		if update.Logging != nil {
			if update.Logging.Level != "" {
				s.cfg.Logging.Level = update.Logging.Level
				if s.appLogger != nil {
					s.appLogger.SetLevel(update.Logging.Level)
				}
			}
			s.cfg.Logging.LogToConsole = update.Logging.LogToConsole
			if s.appLogger != nil {
				s.appLogger.SetConsoleLogging(update.Logging.LogToConsole)
			}
			if update.Logging.FilePath != "" {
				s.cfg.Logging.FilePath = update.Logging.FilePath
			}
			if update.Logging.MaxSizeMB > 0 {
				s.cfg.Logging.MaxSizeMB = update.Logging.MaxSizeMB
			}
			if update.Logging.MaxBackups > 0 {
				s.cfg.Logging.MaxBackups = update.Logging.MaxBackups
			}
		}

		_ = s.engine.UpdateConfig(s.cfg)
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
		if err := s.engine.DeleteTorrent(hash); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "deleted": hash})
		return
	}
	s.handleGetTorrent(w, r)
}

func (s *Server) handleScrapeTorrent(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		hash = strings.TrimSpace(r.URL.Query().Get("info_hash"))
	}
	if hash == "" && r.Method == http.MethodPost {
		var req struct {
			Hash string `json:"hash"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		hash = strings.TrimSpace(req.Hash)
	}

	if hash == "" {
		http.Error(w, "missing hash parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	res, err := s.engine.ScrapeTorrent(ctx, hash)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":   false,
			"info_hash": hash,
			"error":     err.Error(),
		})
		return
	}

	resMap := map[string]any{
		"success":   res.Success,
		"info_hash": res.InfoHash,
		"seeders":   res.Seeders,
		"leechers":  res.Leechers,
		"completed": res.Completed,
	}

	// If this torrent has size == 0, attempt metadata resolution with peers
	rec, _ := s.engine.GetTorrent(hash)
	if rec != nil {
		if rec.Size <= 0 {
			if updatedRec, _ := s.engine.ResolveMetadata(r.Context(), hash); updatedRec != nil {
				rec = updatedRec
			}
		}
		resMap["size"] = rec.Size
		resMap["file_count"] = rec.FileCount
	}

	_ = json.NewEncoder(w).Encode(resMap)
}

func (s *Server) handleScrapeBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Hashes) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"count":   0,
			"results": map[string]any{},
		})
		return
	}

	// Limit to max 70 hashes per batch (fits in single BEP 15 packet)
	cleanHashes := make([]string, 0, len(req.Hashes))
	seen := make(map[string]bool)
	for _, h := range req.Hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if len(h) == 40 && !seen[h] {
			seen[h] = true
			cleanHashes = append(cleanHashes, h)
			if len(cleanHashes) >= 70 {
				break
			}
		}
	}

	if len(cleanHashes) == 0 {
		http.Error(w, "no valid 40-character hex infohashes provided", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	results := s.engine.ScrapeBatch(ctx, cleanHashes)

	resMap := make(map[string]any, len(results))
	successCount := 0
	for h, res := range results {
		resMap[h] = map[string]any{
			"success":      res.Success,
			"seeders":      res.Seeders,
			"leechers":     res.Leechers,
			"completed":    res.Completed,
			"last_scraped": res.LastScraped.UTC().Format(time.RFC3339),
			"error":        res.Error,
		}
		if res.Success {
			successCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":       true,
		"total":         len(cleanHashes),
		"success_count": successCount,
		"results":       resMap,
	})
}

func (s *Server) handleResolveMetadata(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		hash = strings.TrimSpace(r.URL.Query().Get("info_hash"))
	}
	if hash == "" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		var req struct {
			Hash string `json:"hash"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		hash = strings.TrimSpace(req.Hash)
	}

	if hash == "" {
		http.Error(w, "missing hash parameter", http.StatusBadRequest)
		return
	}

	updatedRec, err := s.engine.ResolveMetadata(r.Context(), hash)
	w.Header().Set("Content-Type", "application/json")
	if err != nil && updatedRec == nil {
		w.WriteHeader(http.StatusGatewayTimeout)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":   false,
			"info_hash": hash,
			"error":     fmt.Sprintf("Metadata resolution timed out (queued in background): %v", err),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"info_hash":  hash,
		"name":       updatedRec.Name,
		"size":       updatedRec.Size,
		"file_count": updatedRec.FileCount,
		"files":      updatedRec.FilesJSON,
		"category":   updatedRec.Category,
		"resolution": updatedRec.Resolution,
		"codec":      updatedRec.Codec,
		"quality":    updatedRec.Quality,
		"audio":      updatedRec.Audio,
		"hdr":        updatedRec.HDR,
		"record":     updatedRec,
	})
}

func (s *Server) handleTrendingHarvest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	count, err := s.engine.TriggerTrendingHarvest(ctx)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
			"count": count,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Successfully harvested %d qualified high-seed torrents", count),
		"count":   count,
		"stats":   s.engine.TrendingStats(),
	})
}

func (s *Server) handleTrendingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.engine.TrendingStats())
}

func (s *Server) handleScraperStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := s.engine.ScraperStats()
	resp := map[string]any{
		"total_scraped":    stats.TotalScraped,
		"successful":       stats.Successful,
		"failed":           stats.Failed,
		"last_scrape_time": stats.LastScrapeTime,
		"active_trackers":  stats.ActiveTrackers,
		"running":          stats.Running,
		"enabled":          s.cfg.ScraperEnabled,
	}
	_ = json.NewEncoder(w).Encode(resp)
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

	affected, err := s.engine.PruneTorrents(opts)
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
	count, err := s.engine.CountDeadTorrents(maxSeeders, cutoff)
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

	count, err := s.engine.CountFilterViolations(rules)
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

	deleted, err := s.engine.PurgeFilterViolations(rules)
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
	mem, err := s.engine.ShrinkMemory()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
	found := s.engine.DeleteFeed(id)
	if found {
		s.cfg.ExternalFeeds = s.engine.Feeds()
		_ = config.SaveConfig(s.cfgPath, s.cfg)
	}
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
		statuses := s.engine.FeedStatuses()
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

		s.engine.SaveFeed(f)
		s.cfg.ExternalFeeds = s.engine.Feeds()
		_ = config.SaveConfig(s.cfgPath, s.cfg)

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
		added, err := s.engine.SyncFeed(r.Context(), id)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": err.Error(), "added": added})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "added": added})
		return
	}

	added, errs := s.engine.SyncAllFeeds(r.Context())
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

	trimmedInput := strings.TrimSpace(req.Input)
	w.Header().Set("Content-Type", "application/json")

	// Check if this is a CSV bulk import (torrents.csv format)
	if strings.HasPrefix(strings.ToLower(trimmedInput), "infohash,") || strings.HasPrefix(strings.ToLower(trimmedInput), "info_hash,") || (strings.Contains(trimmedInput, "\n") && strings.Contains(trimmedInput, ",")) {
		sourceName := req.Name
		if sourceName == "" {
			sourceName = "csv-import"
		}
		count, err := s.engine.ImportCSV([]byte(trimmedInput), sourceName)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"mode":     "csv",
			"imported": count,
			"message":  fmt.Sprintf("Successfully imported %d torrents from CSV", count),
		})
		return
	}

	rec, err := s.engine.ImportDirect(req.Input, req.Name)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": err.Error()})
		return
	}
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
	rec, err := s.engine.GetTorrent(hash)
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

	rec, err := s.engine.GetTorrent(hash)
	if err != nil || rec == nil {
		http.Error(w, "Torrent not found", http.StatusNotFound)
		return
	}

	magnet := rec.MagnetURI
	if !strings.HasPrefix(magnet, "magnet:?") {
		magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", rec.InfoHash, url.QueryEscape(rec.Name))
	}

	raw := r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("magnet") == "1"
	accept := r.Header.Get("Accept")
	userAgent := strings.ToLower(r.Header.Get("User-Agent"))
	isAutomated := strings.Contains(userAgent, "sonarr") || strings.Contains(userAgent, "radarr") || strings.Contains(userAgent, "qbittorrent") || strings.Contains(userAgent, "transmission") || strings.Contains(userAgent, "deluge")

	if raw || isAutomated || (accept != "" && !strings.Contains(accept, "text/html")) {
		http.Redirect(w, r, magnet, http.StatusFound)
		return
	}

	safeTitle := html.EscapeString(rec.Name)
	safeHash := html.EscapeString(rec.InfoHash)
	safeMagnetAttr := html.EscapeString(magnet)
	magnetJSON, _ := json.Marshal(magnet)

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
      <button class="btn btn-secondary" id="btn-copy-magnet" data-magnet="%s">📋 Copy Magnet Link</button>
      <a href="/#%s" class="btn btn-secondary">🔍 View on OmniDex Dashboard</a>
    </div>
    <div class="info">Opening magnet in your torrent client automatically...</div>
  </div>
  <script>
    (function() {
      var magnetUri = %s;
      var copyBtn = document.getElementById('btn-copy-magnet');
      if (copyBtn) {
        copyBtn.addEventListener('click', function() {
          var m = this.getAttribute('data-magnet') || magnetUri;
          if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(m).then(function() {
              copyBtn.textContent = 'Copied Magnet URI!';
              setTimeout(function() { copyBtn.textContent = '📋 Copy Magnet Link'; }, 2000);
            });
          }
        });
      }
      if (magnetUri && magnetUri.indexOf('magnet:?') === 0) {
        setTimeout(function() {
          window.location.href = magnetUri;
        }, 100);
      }
    })();
  </script>
</body>
</html>`, safeTitle, safeTitle, safeHash, safeMagnetAttr, safeMagnetAttr, safeHash, string(magnetJSON))
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

	total, added, err := s.engine.SearchAndIndexProwlarr(r.Context(), q, limit)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := "error"
		if strings.Contains(err.Error(), "not configured") {
			status = "unconfigured"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"error":  err.Error(),
			"added":  0,
			"count":  0,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"query":  q,
		"count":  total,
		"added":  added,
	})
}


func (s *Server) handleDownloadClients(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		// Obfuscate client passwords
		safeClients := make([]config.DownloadClientConfig, len(s.cfg.DownloadClients))
		for i, c := range s.cfg.DownloadClients {
			safeClients[i] = c
			if safeClients[i].Password != "" {
				safeClients[i].Password = "••••••••"
			}
		}
		_ = json.NewEncoder(w).Encode(safeClients)
		return
	}

	if r.Method == http.MethodPost {
		var clients []config.DownloadClientConfig
		if err := json.NewDecoder(r.Body).Decode(&clients); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Preserve masked passwords
		for i, c := range clients {
			if isMaskedSecret(c.Password) {
				for _, old := range s.cfg.DownloadClients {
					if old.ID == c.ID && old.Password != "" {
						clients[i].Password = old.Password
						break
					}
				}
			}
		}

		s.cfg.DownloadClients = clients
		_ = s.engine.UpdateConfig(s.cfg)
		_ = config.SaveConfig(s.cfgPath, s.cfg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "count": len(clients)})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleTestDownloadClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var clientCfg config.DownloadClientConfig
	if err := json.NewDecoder(r.Body).Decode(&clientCfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Restore password if masked
	if isMaskedSecret(clientCfg.Password) {
		for _, old := range s.cfg.DownloadClients {
			if old.ID == clientCfg.ID {
				clientCfg.Password = old.Password
				break
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := s.engine.TestDownloadClient(ctx, clientCfg)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "Connected successfully to " + clientCfg.Name,
	})
}

func (s *Server) handleSendToClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ClientID string `json:"client_id"`
		Hash     string `json:"hash"`
		Magnet   string `json:"magnet"`
		Name     string `json:"name"`
		SavePath string `json:"save_path"`
		Category string `json:"category"`
		Paused   bool   `json:"paused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	addOpt := downloader.AddOptions{
		Name:     req.Name,
		Category: req.Category,
		SavePath: req.SavePath,
		Paused:   req.Paused,
	}

	clientCfg, err := s.engine.SendToClient(ctx, req.ClientID, req.Hash, req.Magnet, addOpt)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"client_id":   clientCfg.ID,
		"client_name": clientCfg.Name,
		"client_type": clientCfg.Type,
		"message":     fmt.Sprintf("Torrent sent to %s successfully", clientCfg.Name),
	})
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var entries []logger.LogEntry
	level := "INFO"
	logToConsole := true
	prevCrash := false
	latestCrash := ""

	if s.appLogger != nil {
		entries = s.appLogger.RingBuffer().Entries()
		level = s.appLogger.GetLevel()
		logToConsole = s.appLogger.GetConsoleLogging()
		if ch := s.appLogger.CrashHandler(); ch != nil {
			prevCrash = ch.PreviousCrashDetected
			latestCrash = ch.LatestCrashFile
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"level":                   level,
		"log_file_path":           s.cfg.Logging.FilePath,
		"log_to_console":          logToConsole,
		"previous_crash_detected": prevCrash,
		"latest_crash_file":       latestCrash,
		"entries":                 entries,
	})
}

func (s *Server) handleDownloadLogs(w http.ResponseWriter, r *http.Request) {
	filePath := s.cfg.Logging.FilePath
	if filePath == "" {
		filePath = "omnidex.log"
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "Log file does not exist yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(filePath)))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, filePath)
}

func (s *Server) handleDownloadCrash(w http.ResponseWriter, r *http.Request) {
	var crashFile string
	if s.appLogger != nil && s.appLogger.CrashHandler() != nil {
		crashFile = s.appLogger.CrashHandler().LatestCrashFile
	}
	if crashFile == "" {
		http.Error(w, "No previous crash log found", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(crashFile); os.IsNotExist(err) {
		http.Error(w, "Crash log file not found on disk", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(crashFile)))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, crashFile)
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.appLogger != nil {
		s.appLogger.RingBuffer().Clear()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Level != "" {
		s.cfg.Logging.Level = req.Level
		if s.appLogger != nil {
			s.appLogger.SetLevel(req.Level)
		}
		_ = config.SaveConfig(s.cfgPath, s.cfg)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "level": s.cfg.Logging.Level})
}
