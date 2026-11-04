package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultSukebeiBaseURL is the primary base URL for Sukebei.nyaa.si.
	DefaultSukebeiBaseURL = "https://sukebei.nyaa.si"
)

// SukebeiTrackers holds the dedicated Sukebei tracker (port 8888) and 4 public mirrors.
var SukebeiTrackers = []string{
	"http://sukebei.tracker.wf:8888/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// SukebeiScraper implements SourceScraper for Sukebei (adult Nyaa).
type SukebeiScraper struct {
	baseURL string
	client  *http.Client
}

// NewSukebeiScraper creates a new Sukebei scraper with optional configuration overrides.
func NewSukebeiScraper(opts ...Option) *SukebeiScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultSukebeiBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &SukebeiScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *SukebeiScraper) Name() string {
	return "Sukebei"
}

// Search queries Sukebei using its RSS feed.
func (s *SukebeiScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	cfg := Options{
		BaseURL:    s.baseURL,
		HTTPClient: s.client,
		Timeout:    15 * time.Second,
		Limit:      50,
		Page:       1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	params := url.Values{}
	params.Set("page", "rss")
	params.Set("q", query)

	endpoint := fmt.Sprintf("%s/?%s", strings.TrimRight(cfg.BaseURL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("sukebei: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sukebei: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sukebei: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

// ParseHTML parses raw RSS 2.0 XML or HTML table content from an io.Reader.
// It uses the same parsing engine as Nyaa, but maps ALL categories exclusively to adult taxonomy.
func (s *SukebeiScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	return parseNyaaFeed(r, "Sukebei", s.baseURL, SukebeiTrackers, mapSukebeiCategory)
}

// mapSukebeiCategory maps Sukebei category IDs to adult Torznab categories.
// Crucially, it must NEVER fall back to non-adult categories.
func mapSukebeiCategory(catID string) string {
	switch strings.TrimSpace(catID) {
	case "1_0", "1_1", "2_0", "2_2":
		return "XXX"
	case "1_2", "1_4", "1_5", "2_1":
		return "XXX/ImageSet"
	case "1_3":
		return "XXX/Other"
	default:
		return "XXX"
	}
}
