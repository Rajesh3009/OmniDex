package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultYTSBaseURL is the default primary API base URL for YTS.
	DefaultYTSBaseURL = "https://movies-api.accel.li"
	// FallbackYTSBaseURL is the secondary fallback mirror for YTS.
	FallbackYTSBaseURL = "https://yts.mx"
)

// YTSScraper implements SourceScraper for the YTS movie torrent index.
type YTSScraper struct {
	baseURL string
	client  *http.Client
}

// NewYTSScraper creates a new YTS scraper instance with optional configuration overrides.
func NewYTSScraper(opts ...Option) *YTSScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultYTSBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &YTSScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *YTSScraper) Name() string {
	return "YTS"
}

// Search queries YTS using its JSON API with the given term and options.
func (s *YTSScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
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

	torrents, err := s.doSearch(ctx, cfg.BaseURL, query, cfg)
	if err != nil && cfg.BaseURL == DefaultYTSBaseURL {
		// Attempt fallback to secondary mirror
		fallbackTorrents, fallbackErr := s.doSearch(ctx, FallbackYTSBaseURL, query, cfg)
		if fallbackErr == nil {
			return fallbackTorrents, nil
		}
	}
	return torrents, err
}

// Trending queries YTS movies sorted by seeds descending to retrieve the highest-seeded releases.
func (s *YTSScraper) Trending(ctx context.Context, opts ...Option) ([]Torrent, error) {
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

	torrents, err := s.doTrending(ctx, cfg.BaseURL, cfg)
	if err != nil && cfg.BaseURL == DefaultYTSBaseURL {
		fallbackTorrents, fallbackErr := s.doTrending(ctx, FallbackYTSBaseURL, cfg)
		if fallbackErr == nil {
			return fallbackTorrents, nil
		}
	}
	return torrents, err
}

func (s *YTSScraper) doTrending(ctx context.Context, baseURL string, cfg Options) ([]Torrent, error) {
	limit := cfg.Limit
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("sort_by", "seeds")
	params.Set("order_by", "desc")
	if cfg.Page > 1 {
		params.Set("page", strconv.Itoa(cfg.Page))
	}

	endpoint := fmt.Sprintf("%s/api/v2/list_movies.json?%s", strings.TrimRight(baseURL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("yts: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yts: trending request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yts: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

func (s *YTSScraper) doSearch(ctx context.Context, baseURL, query string, cfg Options) ([]Torrent, error) {
	limit := cfg.Limit
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("query_term", query)
	if cfg.Page > 1 {
		params.Set("page", strconv.Itoa(cfg.Page))
	}

	endpoint := fmt.Sprintf("%s/api/v2/list_movies.json?%s", strings.TrimRight(baseURL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("yts: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yts: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yts: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

// ParseHTML parses raw JSON feed content from an io.Reader into standardized Torrents.
// The method is named ParseHTML to conform with the SourceScraper interface contract.
func (s *YTSScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("yts: read response body: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return []Torrent{}, nil
	}

	var resp ytsResponseEnvelope
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("yts: parse json: %w", err)
	}

	// If no movies found or "movies" key omitted
	if resp.Data.MovieCount == 0 || len(resp.Data.Movies) == 0 {
		return []Torrent{}, nil
	}

	var results []Torrent
	for _, movie := range resp.Data.Movies {
		title := strings.TrimSpace(movie.TitleLong)
		if title == "" {
			title = strings.TrimSpace(movie.Title)
		}
		if title == "" {
			continue
		}

		for _, tor := range movie.Torrents {
			hash := NormalizeInfoHash(tor.Hash)
			if hash == "" {
				continue
			}

			cat := mapYTSCategory(tor.Quality)
			var pubDate time.Time
			if tor.DateUploadedUnix > 0 {
				pubDate = time.Unix(tor.DateUploadedUnix, 0).UTC()
			}

			magnet := BuildMagnetURI(hash, title, PublicTrackers)
			sizeFormatted := formatBytes(tor.SizeBytes)

			results = append(results, Torrent{
				Title:         title,
				MagnetURI:     magnet,
				InfoHash:      hash,
				Size:          tor.SizeBytes,
				SizeFormatted: sizeFormatted,
				Seeds:         tor.Seeds,
				Leeches:       tor.Peers,
				Source:        "YTS",
				Category:      cat,
				PublishedDate: pubDate,
				PageURL:       movie.URL,
			})
		}
	}

	return results, nil
}

func mapYTSCategory(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "480p":
		return "Movies/SD"
	case "720p", "1080p":
		return "Movies/HD"
	case "2160p":
		return "Movies/UHD"
	case "3d":
		return "Movies/3D"
	default:
		return "Movies"
	}
}

// JSON data envelopes for YTS API
type ytsResponseEnvelope struct {
	Status        string  `json:"status"`
	StatusMessage string  `json:"status_message"`
	Data          ytsData `json:"data"`
}

type ytsData struct {
	MovieCount int        `json:"movie_count"`
	Limit      int        `json:"limit"`
	PageNumber int        `json:"page_number"`
	Movies     []ytsMovie `json:"movies"`
}

type ytsMovie struct {
	ID        int          `json:"id"`
	URL       string       `json:"url"`
	Title     string       `json:"title"`
	TitleLong string       `json:"title_long"`
	Year      int          `json:"year"`
	Torrents  []ytsTorrent `json:"torrents"`
}

type ytsTorrent struct {
	URL              string `json:"url"`
	Hash             string `json:"hash"`
	Quality          string `json:"quality"`
	Type             string `json:"type"`
	Seeds            int    `json:"seeds"`
	Peers            int    `json:"peers"`
	SizeBytes        int64  `json:"size_bytes"`
	DateUploadedUnix int64  `json:"date_uploaded_unix"`
}
