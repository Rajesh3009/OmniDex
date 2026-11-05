package scraper

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
)

const (
	// BEP 15 Magic Protocol ID for UDP Tracker handshake
	bep15ProtocolID uint64 = 0x41727101980

	// BEP 15 Actions
	actionConnect uint32 = 0
	actionAnnounce uint32 = 1
	actionScrape  uint32 = 2
	actionError   uint32 = 3

	// Max hashes per BEP 15 UDP scrape packet
	maxScrapeHashes = 70

	// UDP socket read/write timeouts
	udpConnectTimeout = 4 * time.Second
	udpScrapeTimeout  = 5 * time.Second
)

// ScrapeResult represents the health stats returned by trackers for a torrent
type ScrapeResult struct {
	InfoHash    string    `json:"info_hash"`
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	Completed   int       `json:"completed"`
	LastScraped time.Time `json:"last_scraped"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
}

// ScraperStats holds runtime statistics for the scraper engine
type ScraperStats struct {
	TotalScraped   int64     `json:"total_scraped"`
	Successful     int64     `json:"successful"`
	Failed         int64     `json:"failed"`
	LastScrapeTime time.Time `json:"last_scrape_time"`
	ActiveTrackers int       `json:"active_trackers"`
	Running        bool      `json:"running"`
}

// Scraper manages UDP tracker scraping and background health updates
type Scraper struct {
	cfg        config.Config
	store      *db.Store
	trackers   []string
	trackersMu sync.RWMutex
	httpClient *http.Client
	stopCh     chan struct{}
	wg         sync.WaitGroup

	totalScraped int64
	successful   int64
	failed       int64
	lastScrapeMu sync.RWMutex
	lastScrape   time.Time
}

// NewScraper initializes a new live UDP tracker scraper
func NewScraper(cfg config.Config, store *db.Store) *Scraper {
	trackers := make([]string, len(cfg.ScraperTrackers))
	copy(trackers, cfg.ScraperTrackers)

	return &Scraper{
		cfg:      cfg,
		store:    store,
		trackers: trackers,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

// Start begins background scraping and remote tracker list updates
func (s *Scraper) Start() {
	if !s.cfg.ScraperEnabled {
		log.Println("[Scraper] Live tracker scraper is disabled in configuration")
		return
	}

	log.Printf("[Scraper] Starting live UDP scraper with %d initial trackers (interval: %dm, batch size: %d)",
		len(s.GetTrackers()), s.cfg.ScraperIntervalMin, s.cfg.ScraperBatchSize)

	// Fetch fresh public trackers asynchronously on boot if enabled
	if s.cfg.ScraperAutoUpdateTrackers && s.cfg.ScraperTrackersURL != "" {
		go func() {
			s.UpdateTrackersFromRemote()
		}()
	}

	s.wg.Add(1)
	go s.workerLoop()

	// Periodic tracker list updater (every 24 hours)
	s.wg.Add(1)
	go s.trackerRefreshLoop()
}

// Stop cleanly shuts down background scraping workers
func (s *Scraper) Stop() {
	select {
	case <-s.stopCh:
		return
	default:
		close(s.stopCh)
	}
	s.wg.Wait()
	log.Println("[Scraper] Live tracker scraper stopped")
}

// GetTrackers returns the current list of UDP trackers
func (s *Scraper) GetTrackers() []string {
	s.trackersMu.RLock()
	defer s.trackersMu.RUnlock()
	out := make([]string, len(s.trackers))
	copy(out, s.trackers)
	return out
}

// SetTrackers replaces the active UDP tracker list
func (s *Scraper) SetTrackers(newTrackers []string) {
	s.trackersMu.Lock()
	defer s.trackersMu.Unlock()
	s.trackers = make([]string, 0, len(newTrackers))
	for _, t := range newTrackers {
		cleaned := strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(cleaned), "udp://") {
			s.trackers = append(s.trackers, cleaned)
		}
	}
}

// Stats returns live runtime statistics
func (s *Scraper) Stats() ScraperStats {
	s.lastScrapeMu.RLock()
	last := s.lastScrape
	s.lastScrapeMu.RUnlock()

	return ScraperStats{
		TotalScraped:   atomic.LoadInt64(&s.totalScraped),
		Successful:     atomic.LoadInt64(&s.successful),
		Failed:         atomic.LoadInt64(&s.failed),
		LastScrapeTime: last,
		ActiveTrackers: len(s.GetTrackers()),
		Running:        s.cfg.ScraperEnabled,
	}
}

// workerLoop executes round-robin periodic scraping of stale torrents
func (s *Scraper) workerLoop() {
	defer s.wg.Done()

	interval := time.Duration(s.cfg.ScraperIntervalMin) * time.Minute
	if interval < 2*time.Minute {
		interval = 2 * time.Minute
	}

	// Initial short delay to let the server start and DHT stabilize
	select {
	case <-time.After(30 * time.Second):
	case <-s.stopCh:
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial batch
	s.scrapeStaleBatch()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.scrapeStaleBatch()
		}
	}
}

// trackerRefreshLoop refreshes the public tracker list every 24 hours
func (s *Scraper) trackerRefreshLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.cfg.ScraperAutoUpdateTrackers && s.cfg.ScraperTrackersURL != "" {
				s.UpdateTrackersFromRemote()
			}
		}
	}
}

// scrapeStaleBatch retrieves oldest/unscraped torrents and scrapes them
func (s *Scraper) scrapeStaleBatch() {
	limit := s.cfg.ScraperBatchSize
	if limit <= 0 {
		limit = 50
	}

	hashes, err := s.store.GetTorrentsToScrape(limit)
	if err != nil || len(hashes) == 0 {
		return
	}

	log.Printf("[Scraper] Scraping batch of %d stale torrents...", len(hashes))
	s.ScrapeHashes(context.Background(), hashes)
}

// UpdateTrackersFromRemote downloads fresh public trackers from remote URL
func (s *Scraper) UpdateTrackersFromRemote() {
	urlStr := s.cfg.ScraperTrackersURL
	if urlStr == "" {
		return
	}
	if err := config.ValidateOutboundURL(urlStr); err != nil {
		log.Printf("[Scraper] Notice: Remote tracker URL rejected: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
	if err != nil {
		return
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[Scraper] Notice: Failed to pull remote tracker list: %v (using fallback trackers)", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Scraper] Notice: Remote tracker list returned HTTP %d", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return
	}

	lines := strings.Split(string(body), "\n")
	seen := make(map[string]bool)
	var newTrackers []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "udp://") {
			if !seen[trimmed] {
				seen[trimmed] = true
				newTrackers = append(newTrackers, trimmed)
			}
		}
	}

	if len(newTrackers) >= 3 {
		s.SetTrackers(newTrackers)
		log.Printf("[Scraper] Successfully updated public tracker list: %d active UDP trackers", len(newTrackers))
	}
}

// ScrapeSingle scrapes a single infohash immediately and returns results
func (s *Scraper) ScrapeSingle(ctx context.Context, infoHash string) (*ScrapeResult, error) {
	cleanHash := strings.ToLower(strings.TrimSpace(infoHash))
	if len(cleanHash) != 40 {
		return nil, fmt.Errorf("invalid infohash length: expected 40 hex characters")
	}

	results := s.ScrapeHashes(ctx, []string{cleanHash})
	res, ok := results[cleanHash]
	if !ok {
		return nil, fmt.Errorf("tracker scrape failed to return data for infohash %s", cleanHash)
	}
	return &res, nil
}

// ScrapeHashes scrapes a slice of infohashes in chunks across active trackers
func (s *Scraper) ScrapeHashes(ctx context.Context, hashes []string) map[string]ScrapeResult {
	out := make(map[string]ScrapeResult, len(hashes))
	now := time.Now()

	for _, h := range hashes {
		out[h] = ScrapeResult{
			InfoHash:    h,
			LastScraped: now,
			Success:     false,
		}
	}

	trackers := s.GetTrackers()
	if len(trackers) == 0 {
		for h := range out {
			res := out[h]
			res.Error = "no UDP trackers configured"
			out[h] = res
		}
		return out
	}

	// Chunk hashes into maxScrapeHashes (70 hashes) per BEP 15
	for i := 0; i < len(hashes); i += maxScrapeHashes {
		end := i + maxScrapeHashes
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[i:end]

		chunkRes := s.scrapeChunkAcrossTrackers(ctx, chunk, trackers)
		for h, r := range chunkRes {
			out[h] = r

			// Update database store
			if r.Success {
				_ = s.store.UpdateScrapeResult(h, r.Seeders, r.Leechers, true)
				atomic.AddInt64(&s.successful, 1)
			} else {
				_ = s.store.UpdateScrapeResult(h, 0, 0, false)
				atomic.AddInt64(&s.failed, 1)
			}
			atomic.AddInt64(&s.totalScraped, 1)
		}

		s.lastScrapeMu.Lock()
		s.lastScrape = time.Now()
		s.lastScrapeMu.Unlock()

		// Inter-chunk rate limit delay (500ms)
		if end < len(hashes) {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-s.stopCh:
				return out
			case <-ctx.Done():
				return out
			}
		}
	}

	return out
}

// scrapeChunkAcrossTrackers queries up to 4 responsive trackers for a chunk of hashes
func (s *Scraper) scrapeChunkAcrossTrackers(ctx context.Context, hashes []string, trackers []string) map[string]ScrapeResult {
	results := make(map[string]ScrapeResult, len(hashes))
	for _, h := range hashes {
		results[h] = ScrapeResult{
			InfoHash:    h,
			LastScraped: time.Now(),
			Success:     false,
		}
	}

	// Shuffle trackers to distribute load
	shuffled := make([]string, len(trackers))
	copy(shuffled, trackers)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	// Query up to 4 trackers or until we get valid positive seeders
	maxTrackersToQuery := 4
	if len(shuffled) < maxTrackersToQuery {
		maxTrackersToQuery = len(shuffled)
	}

	type trackerAttemptResult struct {
		tracker string
		data    map[string]singleScrapeData
		err     error
	}

	attemptsChan := make(chan trackerAttemptResult, maxTrackersToQuery)
	var wg sync.WaitGroup

	for i := 0; i < maxTrackersToQuery; i++ {
		tURL := shuffled[i]
		wg.Add(1)
		go func(trackerURL string) {
			defer wg.Done()
			data, err := scrapeSingleTracker(ctx, trackerURL, hashes)
			attemptsChan <- trackerAttemptResult{tracker: trackerURL, data: data, err: err}
		}(tURL)
	}

	wg.Wait()
	close(attemptsChan)

	anySuccess := false
	for att := range attemptsChan {
		if att.err == nil && len(att.data) > 0 {
			anySuccess = true
			for h, d := range att.data {
				cur := results[h]
				cur.Success = true
				if d.Seeders > cur.Seeders {
					cur.Seeders = d.Seeders
				}
				if d.Leechers > cur.Leechers {
					cur.Leechers = d.Leechers
				}
				if d.Completed > cur.Completed {
					cur.Completed = d.Completed
				}
				results[h] = cur
			}
		}
	}

	if !anySuccess {
		for h := range results {
			r := results[h]
			r.Error = "trackers unresponsive or timed out"
			results[h] = r
		}
	}

	return results
}

type singleScrapeData struct {
	Seeders   int
	Leechers  int
	Completed int
}

// scrapeSingleTracker implements the BEP 15 UDP connect & scrape handshake
func scrapeSingleTracker(ctx context.Context, trackerURL string, hashes []string) (map[string]singleScrapeData, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, fmt.Errorf("parse tracker URL: %w", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "80")
	}

	// Resolve tracker UDP address
	udpAddr, err := net.ResolveUDPAddr("udp4", host)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP addr %s: %w", host, err)
	}

	conn, err := net.DialUDP("udp4", nil, udpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial UDP: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(udpConnectTimeout + udpScrapeTimeout))

	// 1. Send BEP 15 Connect Request (16 bytes)
	// uint64 protocol_id (0x41727101980)
	// uint32 action (0 = connect)
	// uint32 transaction_id
	txID := rand.Uint32()
	connectReq := make([]byte, 16)
	binary.BigEndian.PutUint64(connectReq[0:8], bep15ProtocolID)
	binary.BigEndian.PutUint32(connectReq[8:12], actionConnect)
	binary.BigEndian.PutUint32(connectReq[12:16], txID)

	if _, err := conn.Write(connectReq); err != nil {
		return nil, fmt.Errorf("send connect: %w", err)
	}

	// Read Connect Response (16 bytes)
	// uint32 action (0)
	// uint32 transaction_id (matches)
	// uint64 connection_id
	connectResp := make([]byte, 16)
	n, err := conn.Read(connectResp)
	if err != nil {
		return nil, fmt.Errorf("read connect resp: %w", err)
	}
	if n < 16 {
		return nil, fmt.Errorf("short connect resp: got %d bytes, expected 16", n)
	}

	respAction := binary.BigEndian.Uint32(connectResp[0:4])
	respTxID := binary.BigEndian.Uint32(connectResp[4:8])
	if respAction != actionConnect || respTxID != txID {
		return nil, fmt.Errorf("invalid connect response header: action=%d txID=%d", respAction, respTxID)
	}

	connectionID := binary.BigEndian.Uint64(connectResp[8:16])

	// 2. Send BEP 15 Scrape Request
	// uint64 connection_id
	// uint32 action (2 = scrape)
	// uint32 transaction_id
	// N * 20 bytes info_hashes
	scrapeTxID := rand.Uint32()
	scrapeReq := make([]byte, 16+len(hashes)*20)
	binary.BigEndian.PutUint64(scrapeReq[0:8], connectionID)
	binary.BigEndian.PutUint32(scrapeReq[8:12], actionScrape)
	binary.BigEndian.PutUint32(scrapeReq[12:16], scrapeTxID)

	offset := 16
	for _, h := range hashes {
		rawHash, err := hex.DecodeString(h)
		if err != nil || len(rawHash) != 20 {
			// Zero fill invalid hash
			rawHash = make([]byte, 20)
		}
		copy(scrapeReq[offset:offset+20], rawHash)
		offset += 20
	}

	if _, err := conn.Write(scrapeReq); err != nil {
		return nil, fmt.Errorf("send scrape: %w", err)
	}

	// Read Scrape Response
	// uint32 action (2)
	// uint32 transaction_id (matches)
	// N * 12 bytes:
	//   uint32 seeders
	//   uint32 completed
	//   uint32 leechers
	expectedLen := 8 + len(hashes)*12
	scrapeResp := make([]byte, expectedLen+100)
	n, err = conn.Read(scrapeResp)
	if err != nil {
		return nil, fmt.Errorf("read scrape resp: %w", err)
	}

	if n < 8 {
		return nil, fmt.Errorf("short scrape resp: %d bytes", n)
	}

	respScrapeAction := binary.BigEndian.Uint32(scrapeResp[0:4])
	respScrapeTxID := binary.BigEndian.Uint32(scrapeResp[4:8])

	if respScrapeAction == actionError {
		errMsg := string(scrapeResp[8:n])
		return nil, fmt.Errorf("tracker returned scrape error: %s", errMsg)
	}

	if respScrapeAction != actionScrape || respScrapeTxID != scrapeTxID {
		return nil, fmt.Errorf("mismatched scrape response: action=%d txID=%d", respScrapeAction, respScrapeTxID)
	}

	out := make(map[string]singleScrapeData, len(hashes))
	dataOffset := 8
	for _, h := range hashes {
		if dataOffset+12 > n {
			break
		}
		seeders := int(binary.BigEndian.Uint32(scrapeResp[dataOffset : dataOffset+4]))
		completed := int(binary.BigEndian.Uint32(scrapeResp[dataOffset+4 : dataOffset+8]))
		leechers := int(binary.BigEndian.Uint32(scrapeResp[dataOffset+8 : dataOffset+12]))

		out[h] = singleScrapeData{
			Seeders:   seeders,
			Completed: completed,
			Leechers:  leechers,
		}
		dataOffset += 12
	}

	return out, nil
}

