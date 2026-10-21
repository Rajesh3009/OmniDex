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
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/int160"
	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage/disabled"

	"local-indexer/internal/db"
	"local-indexer/internal/metadata"
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
	GoodNodes         int   `json:"good_nodes"`
	TotalNodes        int   `json:"total_nodes"`
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
	tcfg.DefaultStorage = disabled.Client{} // Purely memory-based no-op metadata crawler; no disk writes or dummy files

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

				// Sample across up to 15 randomly selected nodes in the table each tick
				for i := 0; i < len(nodes) && i < 15; i++ {
					node := nodes[i]
					nodeAddr := dht.NewAddr(node.Addr.UDP())

					var randTarget [20]byte
					_, _ = crand.Read(randTarget[:])
					targetID := int160.FromByteArray(randTarget)

					// 1. Send find_node to discover further nodes and insert into table
					go func(addr dht.Addr, target int160.T) {
						c.dhtServer.FindNode(addr, target, dht.QueryRateLimiting{NotAny: true})
					}(nodeAddr, targetID)

					// 2. Send BEP 51 sample_infohashes query directly to harvest torrent hashes
					go func(addr dht.Addr, target [20]byte, peerIP net.IP, peerPort int) {
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
				}
			}
		}
	}()

	for i := 0; i < c.cfg.Workers; i++ {
		go c.worker(i)
	}

	// Cache pruning routine (prevents unbounded memory growth).
	// Fix #4: run every 10 minutes instead of 30 to reduce GC pressure which
	// can delay the torrent client's peer connection goroutines.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				c.visited.Range(func(key, val any) bool {
					t, ok := val.(time.Time)
					if ok && now.Sub(t) > 1*time.Hour {
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
	if c.dhtServer != nil {
		stats := c.dhtServer.Stats()
		m.GoodNodes = stats.GoodNodes
		m.TotalNodes = stats.Nodes
	}
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

