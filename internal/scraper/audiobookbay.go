package scraper

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultAudioBookBayBaseURL is the default base URL for AudioBookBay.
	DefaultAudioBookBayBaseURL = "https://audiobookbay.lu"
)

// AudioBookBayScraper implements SourceScraper for AudioBookBay.
type AudioBookBayScraper struct {
	baseURL string
	client  *http.Client
}

// NewAudioBookBayScraper creates a new AudioBookBay scraper with optional configuration overrides.
func NewAudioBookBayScraper(opts ...Option) *AudioBookBayScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultAudioBookBayBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &AudioBookBayScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *AudioBookBayScraper) Name() string {
	return "AudioBookBay"
}

type abbListingItem struct {
	Title   string
	PageURL string
	Date    time.Time
	Size    string
}

var (
	abbPostRegex      = regexp.MustCompile(`(?is)<div\s+class="post"[^>]*>(.*?)</div>\s*<!--\s*end\s+post\s*-->|<div\s+class="post"[^>]*>(.*?)</div>\s*<div\s+class="post"`)
	abbPostAltRegex   = regexp.MustCompile(`(?is)<div\s+class="post"[^>]*>(.*?)<div\s+class="postContent"`)
	abbTitleLinkRegex = regexp.MustCompile(`(?is)<div\s+class="postTitle"[^>]*>\s*<h2>\s*<a\s+[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	abbDateRegex      = regexp.MustCompile(`(?i)Posted:\s*(\d{1,2}\s+[a-zA-Z]{3}\s+\d{4})`)
	abbSizeRegex      = regexp.MustCompile(`(?i)File\s+Size:\s*<span[^>]*>([\d.,]+)</span>\s*([a-zA-Z]+)`)

	abbDetailTitleRegex = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>|<div\s+class="postTitle"[^>]*>\s*<h1>(.*?)</h1>`)
	abbDetailHashRegex  = regexp.MustCompile(`(?i)Info\s+Hash:</td>\s*<td>\s*([0-9a-fA-F]{40})`)
	abbDetailSizeRegex  = regexp.MustCompile(`(?i)File\s+Size:</td>\s*<td>\s*<span[^>]*>([\d.,]+)</span>\s*([a-zA-Z]+)`)
	abbDetailTrackRegex = regexp.MustCompile(`(?is)<td[^>]*>(?:Announce\s+URL|Tracker):</td>\s*<td[^>]*>\s*([^<\s]+)\s*</td>`)
)

// Search queries AudioBookBay and fetches detail pages for infohashes with bounded concurrency.
func (s *AudioBookBayScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	cfg := Options{
		BaseURL:    s.baseURL,
		HTTPClient: s.client,
		Timeout:    15 * time.Second,
		Limit:      10,
		Page:       1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	searchURL := fmt.Sprintf("%s/?s=%s", strings.TrimRight(cfg.BaseURL, "/"), url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("audiobookbay: create search request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audiobookbay: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audiobookbay: server returned status %d", resp.StatusCode)
	}

	searchBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("audiobookbay: read search response: %w", err)
	}

	listings := s.extractListings(string(searchBody), cfg.BaseURL)
	if len(listings) == 0 {
		// If direct search returned full detail page or alternative format
		return s.ParseHTML(strings.NewReader(string(searchBody)))
	}

	maxFetch := cfg.Limit
	if maxFetch <= 0 || maxFetch > len(listings) {
		maxFetch = len(listings)
	}
	if maxFetch > 10 {
		maxFetch = 10
	}
	toFetch := listings[:maxFetch]

	// Bounded concurrency detail page fetching (2 workers, rate paced)
	results := make([]Torrent, 0, len(toFetch))
	var mu sync.Mutex

	type job struct {
		item abbListingItem
	}
	jobs := make(chan job, len(toFetch))
	for _, it := range toFetch {
		jobs <- job{item: it}
	}
	close(jobs)

	concurrency := 2
	if len(toFetch) < concurrency {
		concurrency = len(toFetch)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Rate pacing delay
				time.Sleep(200 * time.Millisecond)

				dReq, err := http.NewRequestWithContext(ctx, http.MethodGet, j.item.PageURL, nil)
				if err != nil {
					continue
				}
				dReq.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
				dReq.Header.Set("Accept", "text/html,application/xhtml+xml")

				dResp, err := cfg.HTTPClient.Do(dReq)
				if err != nil {
					continue
				}

				torrents, pErr := s.parseDetailPage(dResp.Body, j.item.PageURL, j.item.Title, j.item.Date)
				dResp.Body.Close()
				if pErr == nil && len(torrents) > 0 {
					mu.Lock()
					results = append(results, torrents...)
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return results, nil
}

// ParseHTML parses raw book detail HTML or search listing HTML from an io.Reader.
func (s *AudioBookBayScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("audiobookbay: read input: %w", err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return []Torrent{}, nil
	}

	// Case 1: Detail page (contains "Info Hash:" table row)
	if abbDetailHashRegex.MatchString(content) {
		return s.parseDetailPage(strings.NewReader(content), s.baseURL, "", time.Time{})
	}

	// Case 2: Search listing HTML
	listings := s.extractListings(content, s.baseURL)
	var results []Torrent
	for _, it := range listings {
		sizeBytes, sizeFormatted, _ := ParseSize(it.Size)
		results = append(results, Torrent{
			Title:         it.Title,
			PageURL:       it.PageURL,
			PublishedDate: it.Date,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Source:        "AudioBookBay",
			Category:      "Audio/Audiobook",
			Seeds:         0,
			Leeches:       0,
		})
	}

	return results, nil
}

func (s *AudioBookBayScraper) extractListings(content, baseURL string) []abbListingItem {
	var items []abbListingItem

	// Split posts or find title links
	linkMatches := abbTitleLinkRegex.FindAllStringSubmatch(content, -1)
	dateMatches := abbDateRegex.FindAllStringSubmatch(content, -1)
	sizeMatches := abbSizeRegex.FindAllStringSubmatch(content, -1)

	for i, lm := range linkMatches {
		if len(lm) < 3 {
			continue
		}
		rawPath := strings.TrimSpace(lm[1])
		pageURL := rawPath
		if !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
			pageURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(rawPath, "/")
		}

		title := html.UnescapeString(strings.TrimSpace(stripHTMLTags(lm[2])))
		if title == "" {
			continue
		}

		var pubDate time.Time
		if i < len(dateMatches) && len(dateMatches[i]) >= 2 {
			pubDate = parseDateDefensive(dateMatches[i][1])
		}

		sizeStr := ""
		if i < len(sizeMatches) && len(sizeMatches[i]) >= 3 {
			sizeStr = fmt.Sprintf("%s %s", sizeMatches[i][1], sizeMatches[i][2])
		}

		items = append(items, abbListingItem{
			Title:   title,
			PageURL: pageURL,
			Date:    pubDate,
			Size:    sizeStr,
		})
	}

	return items
}

func (s *AudioBookBayScraper) parseDetailPage(r io.Reader, pageURL, fallbackTitle string, fallbackDate time.Time) ([]Torrent, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	content := string(data)

	hashMatch := abbDetailHashRegex.FindStringSubmatch(content)
	if len(hashMatch) < 2 {
		return nil, fmt.Errorf("audiobookbay: info hash not found in detail page")
	}
	hash := NormalizeInfoHash(hashMatch[1])
	if hash == "" {
		return nil, fmt.Errorf("audiobookbay: invalid info hash %q", hashMatch[1])
	}

	title := fallbackTitle
	if titleMatch := abbDetailTitleRegex.FindStringSubmatch(content); len(titleMatch) >= 2 {
		tCandidate := titleMatch[1]
		if tCandidate == "" && len(titleMatch) >= 3 {
			tCandidate = titleMatch[2]
		}
		cleanCandidate := html.UnescapeString(strings.TrimSpace(stripHTMLTags(tCandidate)))
		if cleanCandidate != "" {
			title = cleanCandidate
		}
	}
	if title == "" {
		title = "AudioBook " + hash[:8]
	}

	var sizeBytes int64
	var sizeFormatted string
	if sizeMatch := abbDetailSizeRegex.FindStringSubmatch(content); len(sizeMatch) >= 3 {
		rawSize := fmt.Sprintf("%s %s", sizeMatch[1], sizeMatch[2])
		sizeBytes, sizeFormatted, _ = ParseSize(rawSize)
	}

	pubDate := fallbackDate
	if pubDate.IsZero() {
		if dMatch := abbDateRegex.FindStringSubmatch(content); len(dMatch) >= 2 {
			pubDate = parseDateDefensive(dMatch[1])
		}
	}

	// Extract page trackers
	var trackers []string
	trackMatches := abbDetailTrackRegex.FindAllStringSubmatch(content, -1)
	for _, tm := range trackMatches {
		if len(tm) >= 2 {
			tr := strings.TrimSpace(tm[1])
			if tr != "" {
				trackers = append(trackers, tr)
			}
		}
	}
	if len(trackers) == 0 {
		trackers = PublicTrackers
	}

	magnet := BuildMagnetURI(hash, title, trackers)

	return []Torrent{
		{
			Title:         title,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         0,
			Leeches:       0,
			Source:        "AudioBookBay",
			Category:      "Audio/Audiobook",
			PublishedDate: pubDate,
			PageURL:       pageURL,
		},
	}, nil
}
