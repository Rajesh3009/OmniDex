package dhtcrawler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/int160"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"local-indexer/internal/db"
)

type AnnounceJob struct {
	Hash metainfo.Hash
	IP   net.IP
	Port int
}

type CrawlerConfig struct {
	DHTListenAddr   string
	MaxTorrentPeers int
	Workers         int
}

type Metrics struct {
	AnnouncesReceived int64 `json:"announces_received"`
	MetadataResolved  int64 `json:"metadata_resolved"`
	MetadataFailed    int64 `json:"metadata_failed"`
	ActiveResolving   int64 `json:"active_resolving"`
	QueueLen          int   `json:"queue_len"`
}

type Crawler struct {
	cfg       CrawlerConfig
	store     *db.Store
	client    *torrent.Client
	dhtServer *dht.Server

	workChan chan AnnounceJob
	visited  sync.Map // hash string -> timestamp

	metrics Metrics
	ctx     context.Context
	cancel  context.CancelFunc
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

	c := &Crawler{
		cfg:      cfg,
		store:    store,
		workChan: make(chan AnnounceJob, 2500),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Setup lightweight anacrolix torrent client for wire metadata resolution
	tcfg := torrent.NewDefaultClientConfig()
	tcfg.ListenPort = 0 // random available port for TCP peer connections
	tcfg.NoUpload = true
	tcfg.DisableUTP = false
	tcfg.NoDHT = true // We run our own KRPC listener for passive crawling
	tcfg.DisableAcceptRateLimiting = true
	tcfg.EstablishedConnsPerTorrent = cfg.MaxTorrentPeers
	tcfg.DataDir = "" // memory/noop - we only fetch metadata, no payload pieces

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

	// Continuous random walk traversal to keep our node active in everyone's routing table
	go func() {
		// Wait a bit for initial bootstrap
		time.Sleep(5 * time.Second)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				nodes := c.dhtServer.Nodes()
				if len(nodes) == 0 {
					_, _ = c.dhtServer.Bootstrap()
					continue
				}
				// Query a random target to stimulate DHT find_node and get_peers traffic
				var randTarget [20]byte
				_, _ = rand.Read(randTarget[:])
				for i := 0; i < len(nodes) && i < 8; i++ {
					targetID := int160.FromByteArray(randTarget)
					addr := dht.NewAddr(nodes[i].Addr.UDP())
					c.dhtServer.FindNode(addr, targetID, dht.QueryRateLimiting{NotAny: true})
				}
			}
		}
	}()

	for i := 0; i < c.cfg.Workers; i++ {
		go c.worker(i)
	}

	// Cache pruning routine (prevents unbounded memory growth)
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				c.visited.Range(func(key, val any) bool {
					t, ok := val.(time.Time)
					if ok && now.Sub(t) > 2*time.Hour {
						c.visited.Delete(key)
					}
					return true
				})
			}
		}
	}()
}

func (c *Crawler) Stop() {
	c.cancel()
	if c.dhtServer != nil {
		c.dhtServer.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
}

func (c *Crawler) enqueue(hash metainfo.Hash, ip net.IP, port int) {
	hashHex := hex.EncodeToString(hash[:])
	if _, loaded := c.visited.LoadOrStore(hashHex, time.Now()); loaded {
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

	// Wait for metadata (BEP 9 / BEP 10 exchange with wire peers)
	timeoutCtx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
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

	rec := &db.TorrentRecord{
		InfoHash:    hashHex,
		Name:        info.Name,
		Size:        totalSize,
		FileCount:   len(info.Files),
		FilesJSON:   string(filesJSON),
		PieceLength: info.PieceLength,
		CreatedTime: time.Now().UTC(),
		Source:      "dht",
		Seeders:     0,
		Leechers:    0,
		Category:    guessCategory(info.Name, fileList),
		MagnetURI:   magnet,
	}
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

func (c *Crawler) Metrics() Metrics {
	m := c.metrics
	m.QueueLen = len(c.workChan)
	return m
}
