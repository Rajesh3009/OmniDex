package dhtcrawler

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/int160"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage/disabled"

	"golang.org/x/time/rate"

	"local-indexer/internal/db"
	"local-indexer/internal/metadata"
	"local-indexer/internal/spamfilter"
)

type AnnounceJob struct {
	Hash metainfo.Hash
	IP   net.IP
	Port int
}

type CrawlerConfig struct {
	DHTListenAddr         string
	MaxTorrentPeers       int
	Workers               int
	MaxDownloadRate       int64
	MaxUploadRate         int64
	MonthlyDataLimitBytes int64
}

type Metrics struct {
	AnnouncesReceived     int64   `json:"announces_received"`
	MetadataResolved      int64   `json:"metadata_resolved"`
	MetadataFailed        int64   `json:"metadata_failed"`
	ActiveResolving       int64   `json:"active_resolving"`
	QueueLen              int     `json:"queue_len"`
	GoodNodes             int     `json:"good_nodes"`
	TotalNodes            int     `json:"total_nodes"`
	DownloadBytesRate     float64 `json:"download_bytes_rate"`     // current rate bytes/sec
	UploadBytesRate       float64 `json:"upload_bytes_rate"`       // current rate bytes/sec
	TotalDownloaded       int64   `json:"total_downloaded"`        // persistent total bytes
	TotalUploaded         int64   `json:"total_uploaded"`          // persistent total bytes
	SessionDownloaded     int64   `json:"session_downloaded"`      // current session bytes
	SessionUploaded       int64   `json:"session_uploaded"`        // current session bytes
	MaxDownloadRate       int64   `json:"max_download_rate"`       // configured limit
	MaxUploadRate         int64   `json:"max_upload_rate"`         // configured limit
	MonthlyDownloaded     int64   `json:"monthly_downloaded"`      // monthly downloaded bytes
	MonthlyUploaded       int64   `json:"monthly_uploaded"`        // monthly uploaded bytes
	MonthlyTotalUsed      int64   `json:"monthly_total_used"`      // monthly combined bytes
	MonthlyDataLimitBytes int64   `json:"monthly_data_limit_bytes"` // configured monthly limit (0 = unlimited)
	MonthlyLimitExceeded  bool    `json:"monthly_limit_exceeded"`  // true if monthly limit reached
	CurrentMonth          string  `json:"current_month"`           // YYYY-MM
}

type GraphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "local", "router", "peer", "torrent"
	Addr     string `json:"addr,omitempty"`
	Distance int    `json:"distance,omitempty"`
}

type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "routing", "announce", "resolving"
}

type DHTGraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

type boundedCache struct {
	mu      sync.Mutex
	items   map[string]time.Time
	order   []string
	maxSize int
}

func newBoundedCache(maxSize int) *boundedCache {
	return &boundedCache{
		items:   make(map[string]time.Time, maxSize),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

func (b *boundedCache) HasOrAdd(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.items[key]; exists {
		return true
	}

	if len(b.order) >= b.maxSize {
		oldest := b.order[0]
		b.order = b.order[1:]
		delete(b.items, oldest)
	}

	b.items[key] = time.Now()
	b.order = append(b.order, key)
	return false
}

type Crawler struct {
	cfg       CrawlerConfig
	store     *db.Store
	client    *torrent.Client
	dhtServer *dht.Server

	workChan chan AnnounceJob
	visited  *boundedCache

	metrics Metrics
	ctx     context.Context
	cancel  context.CancelFunc

	downloadLimiter *rate.Limiter
	uploadLimiter   *rate.Limiter

	bwMu              sync.Mutex
	lastStatsSample   time.Time
	lastClientRead    int64
	lastClientWritten int64
	totalDownloaded   int64 // persistent base + session
	totalUploaded     int64 // persistent base + session
	lastPersistRead   int64
	lastPersistWrite  int64
	currentDlRate     float64
	currentUlRate     float64
}

func makeLimiter(bytesPerSec int64) *rate.Limiter {
	if bytesPerSec <= 0 {
		return rate.NewLimiter(rate.Inf, 256*1024)
	}
	burst := int(bytesPerSec)
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return rate.NewLimiter(rate.Limit(bytesPerSec), burst)
}

func (c *Crawler) SetRateLimits(maxDownloadBytesPerSec, maxUploadBytesPerSec int64) {
	c.bwMu.Lock()
	defer c.bwMu.Unlock()

	c.cfg.MaxDownloadRate = maxDownloadBytesPerSec
	c.cfg.MaxUploadRate = maxUploadBytesPerSec

	if maxDownloadBytesPerSec <= 0 {
		c.downloadLimiter.SetLimit(rate.Inf)
	} else {
		c.downloadLimiter.SetLimit(rate.Limit(maxDownloadBytesPerSec))
		burst := int(maxDownloadBytesPerSec)
		if burst < 64*1024 {
			burst = 64 * 1024
		}
		c.downloadLimiter.SetBurst(burst)
	}

	if maxUploadBytesPerSec <= 0 {
		c.uploadLimiter.SetLimit(rate.Inf)
	} else {
		c.uploadLimiter.SetLimit(rate.Limit(maxUploadBytesPerSec))
		burst := int(maxUploadBytesPerSec)
		if burst < 64*1024 {
			burst = 64 * 1024
		}
		c.uploadLimiter.SetBurst(burst)
	}
}

func (c *Crawler) SetMonthlyLimit(limitBytes int64) {
	c.bwMu.Lock()
	defer c.bwMu.Unlock()
	c.cfg.MonthlyDataLimitBytes = limitBytes
}

func (c *Crawler) IsMonthlyLimitExceeded() bool {
	c.bwMu.Lock()
	defer c.bwMu.Unlock()
	if c.cfg.MonthlyDataLimitBytes <= 0 || c.store == nil {
		return false
	}
	bs, err := c.store.GetBandwidthStats()
	if err != nil {
		return false
	}
	return (bs.MonthlyDownloadedBytes + bs.MonthlyUploadedBytes) >= c.cfg.MonthlyDataLimitBytes
}

func NewCrawler(cfg CrawlerConfig, store *db.Store) (*Crawler, error) {
	if cfg.DHTListenAddr == "" {
		cfg.DHTListenAddr = ":6881"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 15
	}
	if cfg.MaxTorrentPeers <= 0 {
		cfg.MaxTorrentPeers = 20
	}

	ctx, cancel := context.WithCancel(context.Background())

	dlLimiter := makeLimiter(cfg.MaxDownloadRate)
	ulLimiter := makeLimiter(cfg.MaxUploadRate)

	// Load persistent bandwidth stats from store
	var initDownloaded, initUploaded int64
	if store != nil {
		if bs, err := store.GetBandwidthStats(); err == nil {
			initDownloaded = bs.TotalDownloadedBytes
			initUploaded = bs.TotalUploadedBytes
		}
	}

	c := &Crawler{
		cfg:             cfg,
		store:           store,
		workChan:        make(chan AnnounceJob, 2500),
		visited:         newBoundedCache(15000),
		ctx:             ctx,
		cancel:          cancel,
		downloadLimiter: dlLimiter,
		uploadLimiter:   ulLimiter,
		lastStatsSample: time.Now(),
		totalDownloaded: initDownloaded,
		totalUploaded:   initUploaded,
	}

	// Setup lightweight anacrolix torrent client for wire metadata resolution
	tcfg := torrent.NewDefaultClientConfig()
	tcfg.ListenPort = 0 // random available port for TCP peer connections
	tcfg.NoUpload = true
	tcfg.DisableUTP = false
	tcfg.NoDHT = true // We run our own KRPC listener for passive crawling
	tcfg.DisableAcceptRateLimiting = true
	tcfg.EstablishedConnsPerTorrent = cfg.MaxTorrentPeers
	tcfg.DefaultStorage = disabled.Client{} // Purely memory-based no-op metadata crawler; no disk writes or dummy files
	tcfg.DownloadRateLimiter = dlLimiter
	tcfg.UploadRateLimiter = ulLimiter

	cl, err := torrent.NewClient(tcfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	c.client = cl

	// Bind UDP packet connection for DHT
	conn, err := net.ListenPacket("udp4", cfg.DHTListenAddr)
	if err != nil {
		// Fallback to random port if 6881 is occupied
		conn, err = net.ListenPacket("udp4", ":0")
		if err != nil {
			cl.Close()
			cancel()
			return nil, fmt.Errorf("listen dht udp: %w", err)
		}
	}

	dhtCfg := dht.NewDefaultServerConfig()
	dhtCfg.Conn = conn
	dhtCfg.Passive = false
	dhtCfg.NoSecurity = true // accept from all public nodes

	dhtCfg.OnQuery = func(query *krpc.Msg, source net.Addr) bool {
		if query.A != nil {
			// If a node asks get_peers for an info_hash, intercept it immediately
			if query.Q == "get_peers" && query.A.InfoHash != [20]byte{} {
				atomic.AddInt64(&c.metrics.AnnouncesReceived, 1)
				var ip net.IP
				var port int
				if udpAddr, ok := source.(*net.UDPAddr); ok {
					ip = udpAddr.IP
					port = udpAddr.Port
				}
				c.enqueue(metainfo.Hash(query.A.InfoHash), ip, port)
			}
		}
		return true // allow normal DHT handling so other nodes keep communicating with us
	}

	dhtCfg.OnAnnouncePeer = func(infoHash metainfo.Hash, ip net.IP, port int, portOk bool) {
		atomic.AddInt64(&c.metrics.AnnouncesReceived, 1)
		c.enqueue(infoHash, ip, port)
	}

	dhtServer, err := dht.NewServer(dhtCfg)
	if err != nil {
		_ = conn.Close()
		cl.Close()
		cancel()
		return nil, fmt.Errorf("create dht server: %w", err)
	}
	c.dhtServer = dhtServer

	return c, nil
}

func (c *Crawler) Start() {
	log.Printf("[DHT Crawler] Started DHT passive listener on %v with %d workers", c.dhtServer.Addr(), c.cfg.Workers)

	// Bootstrap to insert our node into global DHT routing tables
	go func() {
		log.Println("[DHT Crawler] Bootstrapping DHT node with global router nodes...")
		stats, err := c.dhtServer.Bootstrap()
		if err != nil {
			log.Printf("[DHT Crawler] Bootstrap note: %v", err)
		} else {
			log.Printf("[DHT Crawler] Bootstrap complete. Responses received: %d", stats.NumResponses)
		}
	}()

	// Active DHT Infohash Discovery Loop:
	// Uses BEP 51 (sample_infohashes) + find_node random walk traversal
	go func() {
		time.Sleep(3 * time.Second)
		ticker := time.NewTicker(2 * time.Second)
		rebootstrapTicker := time.NewTicker(10 * time.Minute) // Fix #1: periodic re-bootstrap to refresh stale routing table
		defer ticker.Stop()
		defer rebootstrapTicker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				return
			case <-rebootstrapTicker.C:
				// Periodically re-insert ourselves into the DHT regardless of node count.
				// Over time stale nodes stop responding; re-bootstrapping renews the routing table.
				log.Println("[DHT Crawler] Periodic re-bootstrap to refresh routing table...")
				go func() {
					stats, err := c.dhtServer.Bootstrap()
					if err != nil {
						log.Printf("[DHT Crawler] Re-bootstrap note: %v", err)
					} else {
						log.Printf("[DHT Crawler] Re-bootstrap done. Responses: %d", stats.NumResponses)
					}
				}()
			case <-ticker.C:
				nodes := c.dhtServer.Nodes()
				if len(nodes) == 0 {
					_, _ = c.dhtServer.Bootstrap()
					continue
				}

				// Fix #2: Shuffle nodes so we don't always query the same top-15 stale entries.
				// Without shuffling, the crawler exhausts the same stale peers every tick.
				rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })

				querySem := make(chan struct{}, 20)
				// Sample across up to 15 randomly selected nodes in the table each tick
				for i := 0; i < len(nodes) && i < 15; i++ {
					node := nodes[i]
					nodeAddr := dht.NewAddr(node.Addr.UDP())

					var randTarget [20]byte
					_, _ = crand.Read(randTarget[:])
					targetID := int160.FromByteArray(randTarget)

					// 1. Send find_node to discover further nodes and insert into table
					select {
					case querySem <- struct{}{}:
						go func(addr dht.Addr, target int160.T) {
							defer func() { <-querySem }()
							c.dhtServer.FindNode(addr, target, dht.QueryRateLimiting{NotAny: true})
						}(nodeAddr, targetID)
					default:
					}

					// 2. Send BEP 51 sample_infohashes query directly to harvest torrent hashes
					select {
					case querySem <- struct{}{}:
						go func(addr dht.Addr, target [20]byte, peerIP net.IP, peerPort int) {
							defer func() { <-querySem }()
							ctx, cancel := context.WithTimeout(c.ctx, 4*time.Second)
							defer cancel()

							res := c.dhtServer.Query(ctx, addr, "sample_infohashes", dht.QueryInput{
								MsgArgs: krpc.MsgArgs{
									Target: target,
								},
								RateLimiting: dht.QueryRateLimiting{NotAny: true},
							})

							if res.Err == nil && res.Reply.R != nil {
								// Ingest new nodes discovered from response
								for _, n := range res.Reply.R.Nodes {
									_ = c.dhtServer.AddNode(n)
								}

								// Ingest all sampled infohashes returned by the peer
								if res.Reply.R.Samples != nil {
									for _, sample := range *res.Reply.R.Samples {
										atomic.AddInt64(&c.metrics.AnnouncesReceived, 1)
										c.enqueue(metainfo.Hash(sample), peerIP, peerPort)
									}
								}
							}
						}(nodeAddr, randTarget, node.Addr.IP, node.Addr.Port)
					default:
					}
				}
			}
		}
	}()

	for i := 0; i < c.cfg.Workers; i++ {
		go c.worker(i)
	}
}

func (c *Crawler) flushBandwidthLocked() {
	if c.store == nil {
		return
	}
	deltaDown := c.lastClientRead - c.lastPersistRead
	deltaUp := c.lastClientWritten - c.lastPersistWrite
	if deltaDown > 0 || deltaUp > 0 {
		_ = c.store.AddBandwidthStats(deltaDown, deltaUp)
		c.lastPersistRead = c.lastClientRead
		c.lastPersistWrite = c.lastClientWritten
	}
}

func (c *Crawler) Stop() {
	c.cancel()
	if c.dhtServer != nil {
		c.dhtServer.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
	c.bwMu.Lock()
	c.flushBandwidthLocked()
	c.bwMu.Unlock()
}

func (c *Crawler) enqueue(hash metainfo.Hash, ip net.IP, port int) {
	if c.IsMonthlyLimitExceeded() {
		return
	}

	hashHex := hex.EncodeToString(hash[:])
	if c.visited.HasOrAdd(hashHex) {
		return
	}

	// Check if already in DB
	exists, err := c.store.Exists(hashHex)
	if err == nil && exists {
		return
	}

	select {
	case c.workChan <- AnnounceJob{Hash: hash, IP: ip, Port: port}:
	default:
		// Drop if queue full to stay lightweight on RAM/CPU
	}
}

func (c *Crawler) worker(id int) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case job := <-c.workChan:
			c.resolveMetadata(job)
		}
	}
}

func (c *Crawler) resolveMetadata(job AnnounceJob) {
	if c.IsMonthlyLimitExceeded() {
		return
	}

	hashHex := hex.EncodeToString(job.Hash[:])
	atomic.AddInt64(&c.metrics.ActiveResolving, 1)
	defer atomic.AddInt64(&c.metrics.ActiveResolving, -1)

	// Add torrent spec to torrent client with metadata-only goal
	t, _ := c.client.AddTorrentInfoHash(job.Hash)
	if t == nil {
		atomic.AddInt64(&c.metrics.MetadataFailed, 1)
		return
	}
	defer t.Drop()

	// Proactively connect to the announcing peer who has the torrent
	if job.Port > 0 && job.IP != nil {
		t.AddPeers([]torrent.PeerInfo{
			{
				Addr: &net.TCPAddr{
					IP:   job.IP,
					Port: job.Port,
				},
			},
		})
	}

	// Add popular high-speed open public trackers to help locate wire peers immediately
	t.AddTrackers([][]string{
		{"udp://tracker.opentrackr.org:1337/announce"},
		{"udp://open.demonii.com:1337/announce"},
		{"udp://open.stealth.si:80/announce"},
		{"udp://tracker.torrent.eu.org:451/announce"},
		{"udp://explodie.org:6969/announce"},
		{"udp://tracker.moeking.me:6969/announce"},
	})

	// Wait for metadata (BEP 9 / BEP 10 exchange with wire peers).
	// Fix #3: 20s timeout instead of 45s. Holding 15 workers × 45s = 675s worth of
	// open TCP connections saturates OS ports and starves the DHT UDP socket.
	timeoutCtx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
	defer cancel()

	select {
	case <-c.ctx.Done():
		return
	case <-timeoutCtx.Done():
		atomic.AddInt64(&c.metrics.MetadataFailed, 1)
		return
	case <-t.GotInfo():
	}

	info := t.Info()
	if info == nil {
		atomic.AddInt64(&c.metrics.MetadataFailed, 1)
		return
	}

	var totalSize int64
	var fileList []string
	if len(info.Files) > 0 {
		for _, f := range info.Files {
			totalSize += f.Length
			fileList = append(fileList, f.DisplayPath(info))
		}
	} else {
		totalSize = info.TotalLength()
		fileList = append(fileList, info.Name)
	}

	// Truncate fileList if excessively long
	if len(fileList) > 50 {
		fileList = fileList[:50]
	}
	filesJSON, _ := json.Marshal(fileList)

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hashHex, info.Name)

	stats := t.Stats()
	seeders := stats.ConnectedSeeders
	activePeers := stats.ActivePeers
	if seeders <= 0 && activePeers > 0 {
		seeders = 1
	} else if seeders <= 0 {
		seeders = 1 // An active announcer provided this infohash on the DHT
	}
	leechers := activePeers - seeders
	if leechers < 0 {
		leechers = 0
	}

	rec := &db.TorrentRecord{
		InfoHash:    hashHex,
		Name:        info.Name,
		Size:        totalSize,
		FileCount:   len(info.Files),
		FilesJSON:   string(filesJSON),
		PieceLength: info.PieceLength,
		CreatedTime: time.Now().UTC(),
		Source:      "dht",
		Seeders:     seeders,
		Leechers:    leechers,
		Category:    guessCategory(info.Name, fileList),
		MagnetURI:   magnet,
	}
	tags := metadata.ExtractTags(info.Name, fileList)
	rec.Resolution = tags.Resolution
	rec.Codec = tags.Codec
	rec.Quality = tags.Quality
	rec.Audio = tags.Audio
	rec.HDR = tags.HDR
	rec.ReleaseGroup = tags.ReleaseGroup

	spamVerdict := spamfilter.Inspect(info.Name, fileList, rec.Category, rec.Size)
	rec.IsSuspicious = spamVerdict.IsSuspicious
	rec.SpamScore = spamVerdict.Score
	rec.SpamReasons = strings.Join(spamVerdict.Reasons, " • ")

	if rec.FileCount == 0 {
		rec.FileCount = 1
	}

	if err := c.store.UpsertTorrent(rec); err != nil {
		log.Printf("[DHT Crawler] Failed to save metadata for %s: %v", hashHex, err)
		return
	}

	atomic.AddInt64(&c.metrics.MetadataResolved, 1)
	log.Printf("[DHT Crawler] Indexed: %s (%s)", rec.Name, hashHex)
}

func guessCategory(name string, files []string) string {
	lower := name
	for _, f := range files {
		lower += " " + f
	}

	// Adult / XXX content detection (checked first before generic Video)
	if isAdultContent(lower) {
		return "XXX"
	}

	switch {
	case containsAny(lower, ".mkv", ".mp4", ".avi", "1080p", "720p", "2160p", "4k", "bluray", "web-dl", "x264", "x265"):
		return "Video"
	case containsAny(lower, ".flac", ".mp3", ".wav", ".aac", "album", "discography"):
		return "Audio"
	case containsAny(lower, ".exe", ".iso", ".dmg", ".pkg", "repack", "setup", "installer"):
		return "Software"
	case containsAny(lower, ".pdf", ".epub", ".mobi", ".cbr", ".cbz"):
		return "Books"
	default:
		return "Other"
	}
}

func isAdultContent(s string) bool {
	// Common keywords, studios, and acronyms for adult/XXX releases
	adultKeywords := []string{
		"xxx", "porn", "adult", "hentai", "erotica", "jav", "brazzers", "bangbros",
		"naughtyamerica", "realitykings", "blacked", "vixen", "tushy", "mofos",
		"passion-hd", "puremature", "rk-prime", "evilangel", "wicked", "digitalplayground",
		"sweetsinner", "twistys", "teamskeet", "femjoy", "met-art", "nubiles",
		"onlyfans", "manyvids", "fansly", "camsoda", "chaturbate", "stripper",
		"blowjob", "creampie", "milf", "anal", "threesome", "gangbang", "fetish",
		"bdsm", "hardcore", "softcore", "uncensored", "leaked", "babes",
	}

	for _, kw := range adultKeywords {
		if containsWord(s, kw) {
			return true
		}
	}
	return false
}

func containsWord(s, word string) bool {
	// Look for word bounded by non-alphanumeric delimiters (dots, spaces, dashes, brackets, etc.)
	sLen := len(s)
	wLen := len(word)
	for i := 0; i+wLen <= sLen; i++ {
		if s[i:i+wLen] == word {
			leftBoundary := (i == 0) || !isAlphaNum(s[i-1])
			rightBoundary := (i+wLen == sLen) || !isAlphaNum(s[i+wLen])
			if leftBoundary && rightBoundary {
				return true
			}
		}
	}
	return false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}

func (c *Crawler) sampleBandwidthLocked() {
	now := time.Now()
	elapsed := now.Sub(c.lastStatsSample).Seconds()

	if c.client != nil {
		cs := c.client.Stats()
		currentRead := cs.BytesRead.Int64()
		currentWritten := cs.BytesWritten.Int64()

		deltaRead := currentRead - c.lastClientRead
		deltaWritten := currentWritten - c.lastClientWritten

		if deltaRead < 0 {
			deltaRead = 0
		}
		if deltaWritten < 0 {
			deltaWritten = 0
		}

		c.lastClientRead = currentRead
		c.lastClientWritten = currentWritten

		c.totalDownloaded += deltaRead
		c.totalUploaded += deltaWritten

		if elapsed > 0 {
			c.currentDlRate = float64(deltaRead) / elapsed
			c.currentUlRate = float64(deltaWritten) / elapsed
		}
		c.lastStatsSample = now

		// Persist deltas to SQLite store if accumulated > 32KB or periodically
		deltaPersistDown := c.lastClientRead - c.lastPersistRead
		deltaPersistUp := c.lastClientWritten - c.lastPersistWrite
		if deltaPersistDown >= 32*1024 || deltaPersistUp >= 32*1024 {
			c.flushBandwidthLocked()
		}
	}
}

func (c *Crawler) Metrics() Metrics {
	m := c.metrics
	m.QueueLen = len(c.workChan)
	if c.dhtServer != nil {
		stats := c.dhtServer.Stats()
		m.GoodNodes = stats.GoodNodes
		m.TotalNodes = stats.Nodes
	}

	c.bwMu.Lock()
	c.sampleBandwidthLocked()
	m.DownloadBytesRate = c.currentDlRate
	m.UploadBytesRate = c.currentUlRate
	m.TotalDownloaded = c.totalDownloaded
	m.TotalUploaded = c.totalUploaded
	m.SessionDownloaded = c.lastClientRead
	m.SessionUploaded = c.lastClientWritten
	m.MaxDownloadRate = c.cfg.MaxDownloadRate
	m.MaxUploadRate = c.cfg.MaxUploadRate
	m.MonthlyDataLimitBytes = c.cfg.MonthlyDataLimitBytes

	if c.store != nil {
		if bs, err := c.store.GetBandwidthStats(); err == nil {
			m.MonthlyDownloaded = bs.MonthlyDownloadedBytes
			m.MonthlyUploaded = bs.MonthlyUploadedBytes
			m.MonthlyTotalUsed = bs.MonthlyDownloadedBytes + bs.MonthlyUploadedBytes
			m.CurrentMonth = bs.CurrentMonth
			if c.cfg.MonthlyDataLimitBytes > 0 && m.MonthlyTotalUsed >= c.cfg.MonthlyDataLimitBytes {
				m.MonthlyLimitExceeded = true
			}
		}
	}
	c.bwMu.Unlock()

	return m
}

func (c *Crawler) GetGraphData() DHTGraphData {
	data := DHTGraphData{
		Nodes: make([]GraphNode, 0),
		Links: make([]GraphLink, 0),
	}

	if c.dhtServer == nil {
		return data
	}

	srvID := c.dhtServer.ID()
	localID := hex.EncodeToString(srvID[:])
	localShort := localID
	if len(localShort) > 8 {
		localShort = localShort[:8]
	}

	// 1. Central Local Node
	data.Nodes = append(data.Nodes, GraphNode{
		ID:    localID,
		Label: "Local Node (" + localShort + ")",
		Type:  "local",
		Addr:  c.dhtServer.Addr().String(),
	})

	// 2. Discovered DHT Routing Table Nodes
	nodes := c.dhtServer.Nodes()
	maxRender := 50
	for i, n := range nodes {
		if i >= maxRender {
			break
		}
		nodeHex := hex.EncodeToString(n.ID[:])
		short := nodeHex
		if len(short) > 6 {
			short = short[:6]
		}
		addrStr := n.Addr.String()

		data.Nodes = append(data.Nodes, GraphNode{
			ID:    nodeHex,
			Label: short + "@" + addrStr,
			Type:  "peer",
			Addr:  addrStr,
		})

		data.Links = append(data.Links, GraphLink{
			Source: localID,
			Target: nodeHex,
			Type:   "routing",
		})
	}

	return data
}

