package scraper

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// PublicTrackers contains the 9 canonical public BitTorrent tracker announce URLs
// derived from magnetissimo reference crawler specifications.
var PublicTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.tracker.cl:1337/announce",
	"udp://9.rarbg.me:2970/announce",
	"udp://p4p.arenabg.com:1337/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.dler.org:6969/announce",
	"udp://open.stealth.si:80/announce",
	"udp://ipv4.tracker.harry.lu:80/announce",
	"https://opentracker.i2p.rocks:443/announce",
}

// SourceScraper defines the common contract for a native torrent indexer source scraper.
//
// NOTE ON NAMING: This interface is named SourceScraper (with aliases TorrentScraper
// and ScraperInterface) because `type Scraper struct` is already declared in this package
// by `udpscraper.go` (the live BEP 15 UDP tracker health check engine used by main.go).
// In Go, an interface and struct with identical names cannot coexist in the same package.
type SourceScraper interface {
	// Name returns the canonical name of the source (e.g. "YTS", "Nyaa.si").
	Name() string

	// Search queries the source with the given term and functional options.
	Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error)

	// ParseHTML parses raw HTML (or feed XML / API JSON content) from an io.Reader.
	// This enables deterministic offline testing with mock fixtures and fallback parsing.
	ParseHTML(r io.Reader) ([]Torrent, error)
}

// TorrentScraper is an alias for SourceScraper.
type TorrentScraper = SourceScraper

// ScraperInterface is an alias for SourceScraper for interface-based access.
type ScraperInterface = SourceScraper

// TrendingScraper is an optional interface that scrapers can implement to fetch
// top, trending, or highest-seeded torrent releases without requiring an explicit search term.
type TrendingScraper interface {
	SourceScraper
	Trending(ctx context.Context, opts ...Option) ([]Torrent, error)
}

// FetchTrending fetches trending releases from a scraper if it implements TrendingScraper,
// or falls back to Search(ctx, "", opts...) if Trending is not explicitly implemented.
func FetchTrending(ctx context.Context, s SourceScraper, opts ...Option) ([]Torrent, error) {
	if ts, ok := s.(TrendingScraper); ok {
		return ts.Trending(ctx, opts...)
	}
	return s.Search(ctx, "", opts...)
}

// Options defines configuration options for scraper search operations.
type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	Limit      int
	Category   string
	Page       int
}

// Option is a functional option for configuring Options.
type Option func(*Options)

// WithBaseURL overrides the default base URL for the scraper.
func WithBaseURL(url string) Option {
	return func(o *Options) {
		o.BaseURL = strings.TrimRight(strings.TrimSpace(url), "/")
	}
}

// WithHTTPClient overrides the default HTTP client used for requests.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		if client != nil {
			o.HTTPClient = client
		}
	}
}

// WithTimeout sets the request timeout for search operations.
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.Timeout = timeout
		if o.HTTPClient != nil {
			o.HTTPClient.Timeout = timeout
		}
	}
}

// WithLimit sets the maximum number of results to return.
func WithLimit(limit int) Option {
	return func(o *Options) {
		if limit > 0 {
			o.Limit = limit
		}
	}
}

// WithCategory filters results by category if supported by the provider.
func WithCategory(cat string) Option {
	return func(o *Options) {
		o.Category = strings.TrimSpace(cat)
	}
}

// WithPage sets the pagination page number for search results.
func WithPage(page int) Option {
	return func(o *Options) {
		if page > 0 {
			o.Page = page
		}
	}
}

// DefaultOptions returns the baseline configuration with safe defaults.
func DefaultOptions() Options {
	timeout := 15 * time.Second
	return Options{
		HTTPClient: &http.Client{Timeout: timeout},
		Timeout:    timeout,
		Limit:      50,
		Page:       1,
	}
}
