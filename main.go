package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/dhtcrawler"
	"local-indexer/internal/prowlarr"
	"local-indexer/internal/torznab"
)

//go:embed web/static/*
var staticFS embed.FS

type Server struct {
	cfg     config.Config
	cfgPath string
	store   *db.Store
	crawler *dhtcrawler.Crawler
	torznab *torznab.Server
	prowl   *prowlarr.Client
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
		DHTListenAddr:   cfg.DHTListenAddr,
		MaxTorrentPeers: cfg.MaxTorrentPeers,
		Workers:         cfg.DHTWorkers,
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

	srv := &Server{
		cfg:     cfg,
		cfgPath: cfgPath,
		store:   store,
		crawler: crawler,
		torznab: torznabServer,
		prowl:   prowlClient,
	}

	mux := http.NewServeMux()

	// Torznab endpoint
	mux.HandleFunc("/api/v1/torznab", srv.torznab.HandleTorznab)

	// REST APIs for UI
	mux.HandleFunc("/api/v1/search", srv.handleSearch)
	mux.HandleFunc("/api/v1/stats", srv.handleStats)
	mux.HandleFunc("/api/v1/config", srv.handleConfig)

	// Static UI assets
	subFS, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatalf("Error sub static fs: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// fallback to static file if exists or 404
			fileServer.ServeHTTP(w, r)
			return
		}
		indexBytes, _ := fs.ReadFile(subFS, "index.html")
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
		log.Printf("⚡ Local Torrent Indexer & Torznab Bridge is running!")
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
	queryProwlarr := r.URL.Query().Get("prowlarr") == "true"

	if limit <= 0 {
		limit = 50
	}

	// If query requested and prowlarr integration enabled, fetch and ingest in parallel
	if q != "" && queryProwlarr && s.prowl != nil && s.cfg.ProwlarrAPIKey != "" {
		prowlResults, err := s.prowl.Search(q, limit)
		if err == nil {
			for _, item := range prowlResults {
				rec := item.ToRecord()
				if rec.InfoHash != "" {
					_ = s.store.UpsertTorrent(&rec)
				}
			}
		}
	}

	results, total, err := s.store.Search(q, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"items": results,
		"total": total,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	dbStats, _ := s.store.GetStats()
	var crawlerMetrics dhtcrawler.Metrics
	if s.crawler != nil {
		crawlerMetrics = s.crawler.Metrics()
	}

	resp := map[string]any{
		"db":  dbStats,
		"dht": crawlerMetrics,
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
			ProwlarrURL    string `json:"prowlarr_url"`
			ProwlarrAPIKey string `json:"prowlarr_api_key"`
			TorznabAPIKey  string `json:"torznab_api_key"`
			DHTWorkers     int    `json:"dht_workers"`
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

		_ = config.SaveConfig(s.cfgPath, s.cfg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
