package ingester

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"local-indexer/internal/config"
	"local-indexer/internal/db"
	"local-indexer/internal/metadata"
	"local-indexer/internal/spamfilter"
)

// FeedStatus tracks the runtime status of an external feed
type FeedStatus struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	LastSync  time.Time `json:"last_sync"`
	LastCount int       `json:"last_count"`
	LastError string    `json:"last_error,omitempty"`
}

type Ingester struct {
	store      *db.Store
	httpClient *http.Client
	feeds      []config.FeedConfig
	statusMu   sync.RWMutex
	statusMap  map[string]*FeedStatus
	ctx        context.Context
	cancel     context.CancelFunc
	filterMu   sync.RWMutex
	filterFunc func(category, name string) bool
}

func (ing *Ingester) SetFilterFunc(fn func(category, name string) bool) {
	ing.filterMu.Lock()
	defer ing.filterMu.Unlock()
	ing.filterFunc = fn
}

func NewIngester(store *db.Store, initialFeeds []config.FeedConfig) *Ingester {
	ctx, cancel := context.WithCancel(context.Background())
	ing := &Ingester{
		store: store,
		httpClient: &http.Client{
			Timeout: 25 * time.Second,
		},
		feeds:     initialFeeds,
		statusMap: make(map[string]*FeedStatus),
		ctx:       ctx,
		cancel:    cancel,
	}

	for _, f := range initialFeeds {
		ing.statusMap[f.ID] = &FeedStatus{
			ID:      f.ID,
			Name:    f.Name,
			Type:    f.Type,
			URL:     f.URL,
			Enabled: f.Enabled,
		}
	}

	return ing
}

func (ing *Ingester) UpdateFeeds(feeds []config.FeedConfig) {
	ing.statusMu.Lock()
	defer ing.statusMu.Unlock()

	ing.feeds = feeds
	activeIDs := make(map[string]bool, len(feeds))
	for _, f := range feeds {
		activeIDs[f.ID] = true
		if st, ok := ing.statusMap[f.ID]; ok {
			st.Name = f.Name
			st.Type = f.Type
			st.URL = f.URL
			st.Enabled = f.Enabled
		} else {
			ing.statusMap[f.ID] = &FeedStatus{
				ID:      f.ID,
				Name:    f.Name,
				Type:    f.Type,
				URL:     f.URL,
				Enabled: f.Enabled,
			}
		}
	}

	// Purge feeds that were deleted
	for id := range ing.statusMap {
		if !activeIDs[id] {
			delete(ing.statusMap, id)
		}
	}
}

func (ing *Ingester) GetStatuses() []FeedStatus {
	ing.statusMu.RLock()
	defer ing.statusMu.RUnlock()

	res := make([]FeedStatus, 0, len(ing.statusMap))
	for _, s := range ing.statusMap {
		res = append(res, *s)
	}
	return res
}

func (ing *Ingester) Start() {
	go ing.pollLoop()
}

func (ing *Ingester) Stop() {
	ing.cancel()
}

func (ing *Ingester) pollLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	// Initial delay to avoid slowing down startup
	select {
	case <-ing.ctx.Done():
		return
	case <-time.After(15 * time.Second):
		ing.syncDueFeeds()
	}

	for {
		select {
		case <-ing.ctx.Done():
			return
		case <-ticker.C:
			ing.syncDueFeeds()
		}
	}
}

func (ing *Ingester) syncDueFeeds() {
	ing.statusMu.RLock()
	feeds := make([]config.FeedConfig, len(ing.feeds))
	copy(feeds, ing.feeds)
	ing.statusMu.RUnlock()

	now := time.Now()
	for _, f := range feeds {
		if !f.Enabled {
			continue
		}

		interval := f.IntervalMinutes
		if interval <= 0 {
			interval = 30
		}

		ing.statusMu.RLock()
		st := ing.statusMap[f.ID]
		ing.statusMu.RUnlock()

		if st == nil || st.LastSync.IsZero() || now.Sub(st.LastSync) >= time.Duration(interval)*time.Minute {
			go func(feed config.FeedConfig) {
				_, _ = ing.SyncFeed(ing.ctx, feed.ID)
			}(f)
		}
	}
}

func (ing *Ingester) SyncFeed(ctx context.Context, feedID string) (int, error) {
	ing.statusMu.RLock()
	var targetFeed *config.FeedConfig
	for _, f := range ing.feeds {
		if f.ID == feedID {
			cpy := f
			targetFeed = &cpy
			break
		}
	}
	st := ing.statusMap[feedID]
	ing.statusMu.RUnlock()

	if targetFeed == nil {
		return 0, fmt.Errorf("feed not found: %s", feedID)
	}

	addedCount, err := ing.fetchAndIngest(ctx, *targetFeed)

	ing.statusMu.Lock()
	if st != nil {
		st.LastSync = time.Now()
		st.LastCount = addedCount
		if err != nil {
			st.LastError = err.Error()
		} else {
			st.LastError = ""
		}
	}
	ing.statusMu.Unlock()

	if err != nil {
		log.Printf("[Feed Ingester] Sync error for %s (%s): %v", targetFeed.Name, targetFeed.URL, err)
		return 0, err
	}

	log.Printf("[Feed Ingester] Successfully synced %s: added %d new torrents", targetFeed.Name, addedCount)
	return addedCount, nil
}

func (ing *Ingester) SyncAll(ctx context.Context) (int, []error) {
	ing.statusMu.RLock()
	feeds := make([]config.FeedConfig, len(ing.feeds))
	copy(feeds, ing.feeds)
	ing.statusMu.RUnlock()

	totalAdded := 0
	var errs []error

	for _, f := range feeds {
		added, err := ing.SyncFeed(ctx, f.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Name, err))
		} else {
			totalAdded += added
		}
	}

	return totalAdded, errs
}

func (ing *Ingester) fetchAndIngest(ctx context.Context, f config.FeedConfig) (int, error) {
	reqURL := f.URL
	if f.Type == "torznab" {
		parsed, err := url.Parse(f.URL)
		if err == nil {
			q := parsed.Query()
			if q.Get("t") == "" {
				q.Set("t", "search")
			}
			if f.APIKey != "" && q.Get("apikey") == "" {
				q.Set("apikey", f.APIKey)
			}
			parsed.RawQuery = q.Encode()
			reqURL = parsed.String()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "OmniDex/1.2.2 (Feed-Ingester)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, application/json, */*")

	resp, err := ing.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d from %s", resp.StatusCode, f.URL)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return 0, err
	}

	// Route parser based on explicit type or content auto-detection
	fType := strings.ToLower(f.Type)
	trimmed := strings.TrimSpace(string(bodyBytes))
	isJSON := strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")

	if fType == "eztv" || (isJSON && strings.Contains(trimmed, `"torrents"`)) {
		return ing.ingestEZTVJSON(bodyBytes, f.Name)
	}
	if fType == "yts" || (isJSON && strings.Contains(trimmed, `"movies"`)) {
		return ing.ingestYTSJSON(bodyBytes, f.Name)
	}

	if isJSON {
		// Attempt EZTV then YTS fallback
		if added, err := ing.ingestEZTVJSON(bodyBytes, f.Name); err == nil {
			return added, nil
		}
		if added, err := ing.ingestYTSJSON(bodyBytes, f.Name); err == nil {
			return added, nil
		}
		return 0, fmt.Errorf("unrecognized JSON API response format from %s", f.URL)
	}

	// Try RSS / Torznab XML
	return ing.ingestXMLFeed(bodyBytes, f.Name)
}

// XML Structs for Torznab & RSS 2.0
type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string        `xml:"title"`
	Link        string        `xml:"link"`
	PubDate     string        `xml:"pubDate"`
	Description string        `xml:"description"`
	Category    string        `xml:"category"`
	Enclosure   rssEnclosure  `xml:"enclosure"`
	Attrs       []torznabAttr `xml:"attr"`
	InfoHash    string        `xml:"infoHash"`
	MagnetURI   string        `xml:"magnetURI"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func (ing *Ingester) ingestXMLFeed(data []byte, sourceName string) (int, error) {
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return 0, err
	}

	added := 0
	for _, it := range feed.Channel.Items {
		rec := ing.parseRSSItemToRecord(it, sourceName)
		if rec == nil || rec.InfoHash == "" || rec.Name == "" {
			continue
		}

		ing.filterMu.RLock()
		filter := ing.filterFunc
		ing.filterMu.RUnlock()
		if filter != nil && !filter(rec.Category, rec.Name) {
			continue
		}

		exists, _ := ing.store.Exists(rec.InfoHash)
		if !exists {
			added++
		}
		_ = ing.store.UpsertTorrent(rec)
	}

	return added, nil
}

func (ing *Ingester) parseRSSItemToRecord(it rssItem, sourceName string) *db.TorrentRecord {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return nil
	}

	magnet := strings.TrimSpace(it.MagnetURI)
	var size int64 = it.Enclosure.Length
	seeders := 1
	leechers := 0
	hash := strings.ToLower(strings.TrimSpace(it.InfoHash))

	// Inspect Torznab attributes
	for _, attr := range it.Attrs {
		switch strings.ToLower(attr.Name) {
		case "infohash":
			if hash == "" {
				hash = strings.ToLower(strings.TrimSpace(attr.Value))
			}
		case "seeders":
			if s, err := strconv.Atoi(attr.Value); err == nil && s >= 0 {
				seeders = s
			}
		case "peers", "leechers":
			if l, err := strconv.Atoi(attr.Value); err == nil && l >= 0 {
				leechers = l
			}
		case "size":
			if sz, err := strconv.ParseInt(attr.Value, 10, 64); err == nil && sz > 0 {
				size = sz
			}
		case "magneturl":
			if magnet == "" {
				magnet = attr.Value
			}
		}
	}

	// Fallback to Link or Enclosure for magnet
	if magnet == "" && strings.HasPrefix(it.Link, "magnet:") {
		magnet = it.Link
	}
	if magnet == "" && strings.HasPrefix(it.Enclosure.URL, "magnet:") {
		magnet = it.Enclosure.URL
	}

	// Extract infohash from magnet if still empty
	if hash == "" && magnet != "" && strings.Contains(magnet, "btih:") {
		parts := strings.Split(magnet, "btih:")
		if len(parts) > 1 {
			hash = strings.ToLower(strings.Split(parts[1], "&")[0])
		}
	}

	// Synthesize deterministic SHA1 if infohash is not in the feed
	if hash == "" {
		key := it.Link
		if key == "" {
			key = it.Enclosure.URL
		}
		if key == "" {
			key = title
		}
		sum := sha1.Sum([]byte(key))
		hash = hex.EncodeToString(sum[:])
	}

	if magnet == "" && hash != "" {
		magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(title))
	}

	category := guessCategory(title, it.Category)
	tags := metadata.ExtractTags(title, nil)
	spamVerdict := spamfilter.Inspect(title, nil, category, size)

	created := time.Now().UTC()
	if it.PubDate != "" {
		formats := []string{
			time.RFC1123Z,
			time.RFC1123,
			time.RFC822Z,
			time.RFC822,
			time.RFC3339,
			"2006-01-02 15:04:05",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, it.PubDate); err == nil {
				created = t.UTC()
				break
			}
		}
	}

	source := "feed"
	if sourceName != "" {
		source = strings.ToLower(strings.ReplaceAll(sourceName, " ", "-"))
	}

	filesJSON, _ := json.Marshal([]string{title})

	return &db.TorrentRecord{
		InfoHash:     hash,
		Name:         title,
		Size:         size,
		FileCount:    1,
		FilesJSON:    string(filesJSON),
		PieceLength:  0,
		CreatedTime:  created,
		LastSeen:     time.Now().UTC(),
		Source:       source,
		Seeders:      seeders,
		Leechers:     leechers,
		Category:     category,
		MagnetURI:    magnet,
		Resolution:   tags.Resolution,
		Codec:        tags.Codec,
		Quality:      tags.Quality,
		Audio:        tags.Audio,
		HDR:          tags.HDR,
		ReleaseGroup: tags.ReleaseGroup,
		IsSuspicious: spamVerdict.IsSuspicious,
		SpamScore:    spamVerdict.Score,
		SpamReasons:  strings.Join(spamVerdict.Reasons, " • "),
	}
}

// YTS JSON API Models
type ytsResponse struct {
	Status string `json:"status"`
	Data   struct {
		Movies []struct {
			Title    string `json:"title_long"`
			Torrents []struct {
				Hash      string `json:"hash"`
				Quality   string `json:"quality"`
				Type      string `json:"type"`
				Seeds     int    `json:"seeds"`
				Peers     int    `json:"peers"`
				SizeBytes int64  `json:"size_bytes"`
			} `json:"torrents"`
		} `json:"movies"`
	} `json:"data"`
}

func (ing *Ingester) ingestYTSJSON(data []byte, sourceName string) (int, error) {
	var resp ytsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}

	added := 0
	for _, m := range resp.Data.Movies {
		for _, t := range m.Torrents {
			hash := strings.ToLower(strings.TrimSpace(t.Hash))
			if hash == "" {
				continue
			}

			title := fmt.Sprintf("%s [%s] [YTS]", m.Title, t.Quality)
			magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp://tracker.opentrackr.org:1337/announce", hash, url.QueryEscape(title))
			tags := metadata.ExtractTags(title, nil)
			filesJSON, _ := json.Marshal([]string{title})

			rec := &db.TorrentRecord{
				InfoHash:     hash,
				Name:         title,
				Size:         t.SizeBytes,
				FileCount:    1,
				FilesJSON:    string(filesJSON),
				CreatedTime:  time.Now().UTC(),
				LastSeen:     time.Now().UTC(),
				Source:       "yts",
				Seeders:      t.Seeds,
				Leechers:     t.Peers,
				Category:     "Video",
				MagnetURI:    magnet,
				Resolution:   tags.Resolution,
				Codec:        tags.Codec,
				Quality:      t.Quality,
				ReleaseGroup: "YTS",
			}

			ing.filterMu.RLock()
			filter := ing.filterFunc
			ing.filterMu.RUnlock()
			if filter != nil && !filter(rec.Category, rec.Name) {
				continue
			}

			exists, _ := ing.store.Exists(hash)
			if !exists {
				added++
			}
			_ = ing.store.UpsertTorrent(rec)
		}
	}

	return added, nil
}

// EZTV JSON API Models
type eztvResponse struct {
	TorrentsCount int `json:"torrents_count"`
	Limit         int `json:"limit"`
	Page          int `json:"page"`
	Torrents      []struct {
		ID               int         `json:"id"`
		Hash             string      `json:"hash"`
		Filename         string      `json:"filename"`
		Title            string      `json:"title"`
		MagnetURL        string      `json:"magnet_url"`
		Seeds            int         `json:"seeds"`
		Peers            int         `json:"peers"`
		DateReleasedUnix int64       `json:"date_released_unix"`
		SizeBytes        json.Number `json:"size_bytes"`
	} `json:"torrents"`
}

func (ing *Ingester) ingestEZTVJSON(data []byte, sourceName string) (int, error) {
	var resp eztvResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}

	if len(resp.Torrents) == 0 {
		return 0, nil
	}

	added := 0
	for _, t := range resp.Torrents {
		hash := strings.ToLower(strings.TrimSpace(t.Hash))
		if hash == "" && t.MagnetURL != "" && strings.Contains(t.MagnetURL, "btih:") {
			parts := strings.Split(t.MagnetURL, "btih:")
			if len(parts) > 1 {
				hash = strings.ToLower(strings.Split(parts[1], "&")[0])
			}
		}
		if hash == "" {
			continue
		}

		title := strings.TrimSpace(t.Title)
		if title == "" {
			title = strings.TrimSpace(t.Filename)
		}
		if title == "" {
			title = "EZTV Torrent " + hash[:8]
		}

		magnet := t.MagnetURL
		if magnet == "" {
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(title))
		}

		var size int64
		if sz, err := t.SizeBytes.Int64(); err == nil && sz > 0 {
			size = sz
		}

		tags := metadata.ExtractTags(title, nil)
		spamVerdict := spamfilter.Inspect(title, nil, "Video", size)
		filesJSON, _ := json.Marshal([]string{title})

		created := time.Now().UTC()
		if t.DateReleasedUnix > 0 {
			created = time.Unix(t.DateReleasedUnix, 0).UTC()
		}

		source := "eztv"
		if sourceName != "" {
			source = strings.ToLower(strings.ReplaceAll(sourceName, " ", "-"))
		}

		rec := &db.TorrentRecord{
			InfoHash:     hash,
			Name:         title,
			Size:         size,
			FileCount:    1,
			FilesJSON:    string(filesJSON),
			CreatedTime:  created,
			LastSeen:     time.Now().UTC(),
			Source:       source,
			Seeders:      t.Seeds,
			Leechers:     t.Peers,
			Category:     "Video",
			MagnetURI:    magnet,
			Resolution:   tags.Resolution,
			Codec:        tags.Codec,
			Quality:      tags.Quality,
			HDR:          tags.HDR,
			ReleaseGroup: tags.ReleaseGroup,
			IsSuspicious: spamVerdict.IsSuspicious,
			SpamScore:    spamVerdict.Score,
			SpamReasons:  strings.Join(spamVerdict.Reasons, " • "),
		}

		ing.filterMu.RLock()
		filter := ing.filterFunc
		ing.filterMu.RUnlock()
		if filter != nil && !filter(rec.Category, rec.Name) {
			continue
		}

		exists, _ := ing.store.Exists(hash)
		if !exists {
			added++
		}
		_ = ing.store.UpsertTorrent(rec)
	}

	return added, nil
}

// ImportDirect parses and inserts a raw magnet URI or 40-character infohash directly
func (ing *Ingester) ImportDirect(rawInput string, customName string) (*db.TorrentRecord, error) {
	clean := strings.TrimSpace(rawInput)
	if clean == "" {
		return nil, fmt.Errorf("empty input")
	}

	var hash string
	var magnet string
	name := strings.TrimSpace(customName)

	if strings.HasPrefix(strings.ToLower(clean), "magnet:") {
		magnet = clean
		if strings.Contains(magnet, "btih:") {
			parts := strings.Split(magnet, "btih:")
			if len(parts) > 1 {
				hash = strings.ToLower(strings.Split(parts[1], "&")[0])
			}
		}
		if name == "" && strings.Contains(magnet, "dn=") {
			dnParts := strings.Split(magnet, "dn=")
			if len(dnParts) > 1 {
				rawName := strings.Split(dnParts[1], "&")[0]
				if decoded, err := url.QueryUnescape(rawName); err == nil {
					name = decoded
				} else {
					name = rawName
				}
			}
		}
	} else if len(clean) == 40 || len(clean) == 32 {
		hash = strings.ToLower(clean)
	}

	if hash == "" {
		return nil, fmt.Errorf("could not extract valid infohash from input")
	}

	if name == "" {
		name = "Imported Torrent (" + hash[:8] + ")"
	}
	if magnet == "" {
		magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(name))
	}

	category := guessCategory(name, "")

	ing.filterMu.RLock()
	filter := ing.filterFunc
	ing.filterMu.RUnlock()
	if filter != nil && !filter(category, name) {
		return nil, fmt.Errorf("torrent does not match retention filter rules")
	}

	tags := metadata.ExtractTags(name, nil)
	filesJSON, _ := json.Marshal([]string{name})

	rec := &db.TorrentRecord{
		InfoHash:     hash,
		Name:         name,
		Size:         0,
		FileCount:    1,
		FilesJSON:    string(filesJSON),
		CreatedTime:  time.Now().UTC(),
		LastSeen:     time.Now().UTC(),
		Source:       "manual",
		Seeders:      1,
		Leechers:     0,
		Category:     category,
		MagnetURI:    magnet,
		Resolution:   tags.Resolution,
		Codec:        tags.Codec,
		Quality:      tags.Quality,
		Audio:        tags.Audio,
		HDR:          tags.HDR,
		ReleaseGroup: tags.ReleaseGroup,
	}

	if err := ing.store.UpsertTorrent(rec); err != nil {
		return nil, err
	}

	return rec, nil
}

func guessCategory(title, feedCategory string) string {
	lower := strings.ToLower(title + " " + feedCategory)
	if strings.Contains(lower, "xxx") || strings.Contains(lower, "adult") || strings.Contains(lower, "porn") {
		return "XXX"
	}
	switch {
	case containsWord(lower, "book") || containsWord(lower, "books") || containsWord(lower, "comic") || strings.Contains(lower, ".pdf") || strings.Contains(lower, ".epub") || strings.Contains(lower, ".mobi") || strings.Contains(lower, "e-books") || strings.Contains(lower, "ebook"):
		return "Books"
	case containsWord(lower, "audio") || containsWord(lower, "music") || strings.Contains(lower, ".flac") || strings.Contains(lower, ".mp3") || strings.Contains(lower, ".wav") || strings.Contains(lower, "album") || strings.Contains(lower, "discography"):
		return "Audio"
	case strings.Contains(lower, "video") || strings.Contains(lower, "movie") || strings.Contains(lower, "1080p") || strings.Contains(lower, "720p") || strings.Contains(lower, "2160p") || strings.Contains(lower, "bluray") || strings.Contains(lower, "web-dl") || strings.Contains(lower, ".mkv") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".avi") || strings.Contains(lower, "season") || strings.Contains(lower, "s0") || strings.Contains(lower, "e0"):
		return "Video"
	case strings.Contains(lower, "software") || strings.Contains(lower, "game") || strings.Contains(lower, "games") || strings.Contains(lower, ".exe") || strings.Contains(lower, ".iso") || strings.Contains(lower, "repack") || containsWord(lower, "app") || containsWord(lower, "apps") || strings.Contains(lower, "ubuntu") || strings.Contains(lower, "windows") || strings.Contains(lower, "macos") || strings.Contains(lower, "linux") || strings.Contains(lower, "desktop"):
		return "Software"
	default:
		return "Other"
	}
}

func containsWord(s, word string) bool {
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
