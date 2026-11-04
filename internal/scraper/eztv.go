package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultEZTVBaseURL is the default base URL for EZTV.
	DefaultEZTVBaseURL = "https://eztvx.to"
	// DefaultIMDbSuggestionURL is the suggestion endpoint used to resolve TV show titles to IMDb IDs.
	DefaultIMDbSuggestionURL = "https://v3.sg.media-imdb.com/suggestion/x"
)

// EZTVScraper implements SourceScraper for EZTV.
type EZTVScraper struct {
	baseURL string
	client  *http.Client
}

// NewEZTVScraper creates a new EZTV scraper with optional configuration overrides.
func NewEZTVScraper(opts ...Option) *EZTVScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultEZTVBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &EZTVScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *EZTVScraper) Name() string {
	return "EZTV"
}

var (
	// eztvReleaseTokenRegex cleans release tokens (e.g. S01E02, 720p, x264) for IMDb show lookups.
	eztvReleaseTokenRegex = regexp.MustCompile(`(?i)\b(s\d{1,2}(e\d{1,3})?|e\d{1,3}|\d{1,2}x\d{1,3}|\d{3,4}p|x264|h264|x265|hevc|web-dl|webrip|hdtv)\b`)
	eztvResolutionRegex   = regexp.MustCompile(`(?i)\b(480p|720p|1080p|2160p)\b`)
	eztvNumericRegex      = regexp.MustCompile(`^(?:tt)?(\d+)$`)
	eztvSlugRegex         = regexp.MustCompile(`[^a-z0-9]+`)
)

// Search queries EZTV by first looking up the TV series IMDb ID, then fetching the torrent listing.
func (s *EZTVScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	cfg := Options{
		BaseURL:    s.baseURL,
		HTTPClient: s.client,
		Timeout:    15 * time.Second,
		Limit:      100,
		Page:       1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	cleanedQuery := strings.TrimSpace(query)
	if cleanedQuery == "" {
		return []Torrent{}, nil
	}

	// Step 1: Check if query is already an IMDb ID (e.g. "tt1000001" or "1000001")
	imdbID := ""
	if numMatch := eztvNumericRegex.FindStringSubmatch(cleanedQuery); len(numMatch) >= 2 {
		imdbID = numMatch[1]
	} else {
		// Clean release tokens from query to isolate show title
		showTitle := strings.TrimSpace(eztvReleaseTokenRegex.ReplaceAllString(cleanedQuery, " "))
		if showTitle == "" {
			showTitle = cleanedQuery
		}

		// Resolve show title via IMDb suggestion API
		imdbIDs := s.lookupIMDbIDs(ctx, cfg.HTTPClient, showTitle)
		if len(imdbIDs) > 0 {
			imdbID = imdbIDs[0]
		}
	}

	if imdbID == "" {
		// If IMDb resolution yielded nothing, return empty slice
		return []Torrent{}, nil
	}

	// Step 2: Query EZTV Torrents API with numeric IMDb ID
	limit := cfg.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	page := cfg.Page
	if page < 1 {
		page = 1
	}

	apiURL := fmt.Sprintf("%s/api/get-torrents?imdb_id=%s&limit=%d&page=%d",
		strings.TrimRight(cfg.BaseURL, "/"), url.QueryEscape(imdbID), limit, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("eztv: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eztv: api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eztv: server returned status %d", resp.StatusCode)
	}

	torrents, err := s.ParseHTML(resp.Body)
	if err != nil {
		return nil, err
	}

	// Step 3: Post-filter results if original query had specific episode or quality tokens
	return s.filterResults(torrents, cleanedQuery), nil
}

// lookupIMDbIDs queries the IMDb suggestion API and returns candidate numeric IMDb IDs.
func (s *EZTVScraper) lookupIMDbIDs(ctx context.Context, client *http.Client, showTitle string) []string {
	encoded := url.QueryEscape(strings.ToLower(showTitle))
	lookupURL := fmt.Sprintf("%s/%s.json", DefaultIMDbSuggestionURL, encoded)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var envelope struct {
		D []struct {
			ID  string `json:"id"`
			QID string `json:"qid"`
			L   string `json:"l"`
		} `json:"d"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil
	}

	var results []string
	// Prioritize TV series
	for _, item := range envelope.D {
		if strings.HasPrefix(item.ID, "tt") {
			digits := strings.TrimPrefix(item.ID, "tt")
			if item.QID == "tvSeries" || item.QID == "tvMiniSeries" {
				results = append(results, digits)
			}
		}
	}

	// Fallback to any matching IMDb item if no explicit tvSeries tag
	if len(results) == 0 {
		for _, item := range envelope.D {
			if strings.HasPrefix(item.ID, "tt") {
				results = append(results, strings.TrimPrefix(item.ID, "tt"))
			}
		}
	}

	return results
}

func (s *EZTVScraper) filterResults(torrents []Torrent, originalQuery string) []Torrent {
	tokens := strings.Fields(strings.ToLower(originalQuery))
	if len(tokens) <= 1 {
		return torrents
	}

	// Check if any specific tokens like "s01e02" or "1080p" are present
	var specificTokens []string
	for _, tok := range tokens {
		if eztvReleaseTokenRegex.MatchString(tok) {
			specificTokens = append(specificTokens, tok)
		}
	}

	if len(specificTokens) == 0 {
		return torrents
	}

	var filtered []Torrent
	for _, tor := range torrents {
		titleLower := strings.ToLower(tor.Title)
		matchAll := true
		for _, tok := range specificTokens {
			if !strings.Contains(titleLower, tok) {
				matchAll = false
				break
			}
		}
		if matchAll {
			filtered = append(filtered, tor)
		}
	}

	if len(filtered) > 0 {
		return filtered
	}
	return torrents
}

// ParseHTML parses raw EZTV API JSON or HTML table content from an io.Reader.
func (s *EZTVScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("eztv: read response body: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return []Torrent{}, nil
	}

	// Try JSON API parsing first
	if strings.HasPrefix(trimmed, "{") {
		var envelope eztvResponseEnvelope
		if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Torrents) > 0 {
			return s.parseJSONTorrents(envelope.Torrents)
		}
	}

	// HTML table parsing fallback
	if strings.Contains(trimmed, "<table") || strings.Contains(trimmed, "forum_header_border") || strings.Contains(trimmed, "<tr") {
		return s.parseHTMLTable(data)
	}

	return []Torrent{}, nil
}

func (s *EZTVScraper) parseJSONTorrents(items []eztvTorrentItem) ([]Torrent, error) {
	var results []Torrent

	for _, item := range items {
		rawTitle := strings.TrimSpace(item.Title)
		if rawTitle == "" {
			rawTitle = strings.TrimSpace(item.Filename)
		}
		if rawTitle == "" {
			continue
		}

		// Clean site suffix " EZTV"
		cleanTitle := strings.TrimSpace(strings.TrimSuffix(rawTitle, " EZTV"))
		cleanTitle = strings.TrimSpace(strings.TrimSuffix(cleanTitle, "[eztv]"))
		cleanTitle = html.UnescapeString(cleanTitle)

		hash := NormalizeInfoHash(item.Hash)
		if hash == "" {
			hash = NormalizeInfoHash(item.MagnetURL)
		}
		if hash == "" {
			continue
		}

		sizeBytes := parseFlexibleInt64(item.SizeBytes)
		sizeFormatted := formatBytes(sizeBytes)
		seeds := parseFlexibleInt(item.Seeds)
		leeches := parseFlexibleInt(item.Peers)

		var pubDate time.Time
		if item.DateReleasedUnix > 0 {
			pubDate = time.Unix(item.DateReleasedUnix, 0).UTC()
		}

		category := mapEZTVCategory(cleanTitle)

		magnet := item.MagnetURL
		if magnet == "" || !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
			magnet = BuildMagnetURI(hash, cleanTitle, PublicTrackers)
		}

		pageURL := strings.TrimSpace(item.EpisodeURL)
		if pageURL == "" {
			slug := slugifyTitle(cleanTitle)
			pageURL = fmt.Sprintf("%s/ep/%d/%s", strings.TrimRight(s.baseURL, "/"), item.ID, slug)
		}

		results = append(results, Torrent{
			Title:         cleanTitle,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        "EZTV",
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

var (
	eztvHTMLRowRegex    = regexp.MustCompile(`(?is)<tr[^>]*class="forum_header_border"[^>]*>(.*?)</tr>`)
	eztvHTMLLinkRegex   = regexp.MustCompile(`(?is)<a\s+[^>]*href="(/ep/\d+/[^"]*)"[^>]*>(.*?)</a>`)
	eztvHTMLMagnetRegex = regexp.MustCompile(`(?i)href="([^"]*magnet:[^"]*)"`)
	eztvHTMLCellRegex   = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
)

func (s *EZTVScraper) parseHTMLTable(data []byte) ([]Torrent, error) {
	content := string(data)
	rows := eztvHTMLRowRegex.FindAllStringSubmatch(content, -1)
	if len(rows) == 0 {
		return []Torrent{}, nil
	}

	var results []Torrent
	for _, row := range rows {
		rowContent := row[1]
		linkMatch := eztvHTMLLinkRegex.FindStringSubmatch(rowContent)
		if len(linkMatch) < 3 {
			continue
		}

		rawTitle := html.UnescapeString(strings.TrimSpace(stripHTMLTags(linkMatch[2])))
		cleanTitle := strings.TrimSpace(strings.TrimSuffix(rawTitle, " EZTV"))
		cleanTitle = strings.TrimSpace(strings.TrimSuffix(cleanTitle, "[eztv]"))
		if cleanTitle == "" {
			continue
		}

		pageURL := strings.TrimRight(s.baseURL, "/") + linkMatch[1]

		var magnet string
		if mMatch := eztvHTMLMagnetRegex.FindStringSubmatch(rowContent); len(mMatch) >= 2 {
			magnet = html.UnescapeString(mMatch[1])
		}
		hash := NormalizeInfoHash(magnet)
		if hash == "" {
			continue
		}

		cells := eztvHTMLCellRegex.FindAllStringSubmatch(rowContent, -1)
		var sizeBytes int64
		var sizeFormatted string
		seeds := 0
		leeches := 0

		if len(cells) >= 6 {
			sizeBytes, sizeFormatted, _ = ParseSize(stripHTMLTags(cells[3][1]))
			seeds = ParseIntDefensive(stripHTMLTags(cells[5][1]), 0)
		}

		category := mapEZTVCategory(cleanTitle)
		if magnet == "" {
			magnet = BuildMagnetURI(hash, cleanTitle, PublicTrackers)
		}

		results = append(results, Torrent{
			Title:         cleanTitle,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        "EZTV",
			Category:      category,
			PublishedDate: time.Time{},
			PageURL:       pageURL,
		})
	}

	return results, nil
}

func mapEZTVCategory(title string) string {
	resMatch := eztvResolutionRegex.FindStringSubmatch(title)
	if len(resMatch) >= 2 {
		switch strings.ToLower(resMatch[1]) {
		case "480p":
			return "TV/SD"
		case "720p", "1080p":
			return "TV/HD"
		case "2160p":
			return "TV/UHD"
		}
	}
	return "TV"
}

func slugifyTitle(title string) string {
	lower := strings.ToLower(title)
	cleaned := eztvSlugRegex.ReplaceAllString(lower, "-")
	return strings.Trim(cleaned, "-")
}

func parseFlexibleInt64(val any) int64 {
	switch v := val.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		bytes, _, err := ParseSize(v)
		if err == nil {
			return bytes
		}
		parsed, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed
	default:
		return 0
	}
}

func parseFlexibleInt(val any) int {
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		return ParseIntDefensive(v, 0)
	default:
		return 0
	}
}

// JSON data structures for EZTV API
type eztvResponseEnvelope struct {
	IMDbID        string           `json:"imdb_id"`
	TorrentsCount int              `json:"torrents_count"`
	Limit         int              `json:"limit"`
	Page          int              `json:"page"`
	Torrents      []eztvTorrentItem `json:"torrents"`
}

type eztvTorrentItem struct {
	ID               int    `json:"id"`
	Hash             string `json:"hash"`
	Filename         string `json:"filename"`
	Title            string `json:"title"`
	DateReleasedUnix int64  `json:"date_released_unix"`
	SizeBytes        any    `json:"size_bytes"` // string or numeric
	Seeds            any    `json:"seeds"`      // string or numeric
	Peers            any    `json:"peers"`      // string or numeric
	MagnetURL        string `json:"magnet_url"`
	EpisodeURL       string `json:"episode_url"`
}
