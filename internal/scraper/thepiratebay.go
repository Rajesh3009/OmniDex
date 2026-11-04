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
	// DefaultTPBAPIBaseURL is the official API endpoint for The Pirate Bay.
	DefaultTPBAPIBaseURL = "https://apibay.org"
	// DefaultTPBSiteURL is the public web interface for detail links.
	DefaultTPBSiteURL = "https://thepiratebay.org"
)

// ThePirateBayScraper implements SourceScraper for The Pirate Bay (via apibay API).
type ThePirateBayScraper struct {
	baseURL string
	siteURL string
	client  *http.Client
}

// NewThePirateBayScraper creates a new The Pirate Bay scraper with optional configuration overrides.
func NewThePirateBayScraper(opts ...Option) *ThePirateBayScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultTPBAPIBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &ThePirateBayScraper{
		baseURL: cfg.BaseURL,
		siteURL: DefaultTPBSiteURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *ThePirateBayScraper) Name() string {
	return "The Pirate Bay"
}

// Search queries apibay.org for torrents matching the query term.
func (s *ThePirateBayScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
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
	params.Set("q", query)
	params.Set("cat", "")

	endpoint := fmt.Sprintf("%s/q.php?%s", strings.TrimRight(cfg.BaseURL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("thepiratebay: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thepiratebay: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thepiratebay: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

// ParseHTML parses raw JSON array or HTML table content from an io.Reader.
func (s *ThePirateBayScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("thepiratebay: read response body: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return []Torrent{}, nil
	}

	// Try JSON parsing first (standard for apibay.org)
	if strings.HasPrefix(trimmed, "[") {
		var items []tpbJSONRow
		if err := json.Unmarshal(data, &items); err == nil {
			return s.parseJSONItems(items)
		}
	}

	// HTML fallback if response is HTML markup
	if strings.Contains(trimmed, "<table") || strings.Contains(trimmed, "searchResult") {
		return s.parseHTMLTable(data)
	}

	return []Torrent{}, nil
}

func (s *ThePirateBayScraper) parseJSONItems(items []tpbJSONRow) ([]Torrent, error) {
	var results []Torrent

	for _, row := range items {
		// apibay returns a placeholder row when there are 0 results:
		// [{"id":"0","name":"No results returned","info_hash":"0000000000000000000000000000000000000000"}]
		if row.ID == "0" || row.InfoHash == "0000000000000000000000000000000000000000" || row.Name == "No results returned" {
			continue
		}

		hash := NormalizeInfoHash(row.InfoHash)
		if hash == "" {
			continue
		}

		title := html.UnescapeString(strings.TrimSpace(row.Name))
		if title == "" {
			continue
		}

		// Parse numeric size
		sizeBytes, sizeFormatted, _ := ParseSize(row.Size)
		seeds := ParseIntDefensive(row.Seeders, 0)
		leeches := ParseIntDefensive(row.Leechers, 0)

		var pubDate time.Time
		if addedSec, err := strconv.ParseInt(strings.TrimSpace(row.Added), 10, 64); err == nil && addedSec > 0 {
			pubDate = time.Unix(addedSec, 0).UTC()
		}

		category := mapTPBCategory(row.Category)
		magnet := BuildMagnetURI(hash, title, PublicTrackers)
		pageURL := fmt.Sprintf("%s/description.php?id=%s", strings.TrimRight(s.siteURL, "/"), strings.TrimSpace(row.ID))

		results = append(results, Torrent{
			Title:         title,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        "The Pirate Bay",
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

var (
	tpbHTMLRowRegex    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tpbHTMLLinkRegex   = regexp.MustCompile(`(?is)<a\s+[^>]*href="(/description\.php\?id=\d+|/torrent/\d+/[^"]*)"[^>]*>(.*?)</a>`)
	tpbHTMLMagnetRegex = regexp.MustCompile(`(?i)href="([^"]*magnet:[^"]*)"`)
	tpbHTMLDescRegex   = regexp.MustCompile(`(?i)Uploaded\s+([^,]+),\s*Size\s+([^,]+),`)
	tpbHTMLCellRegex   = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
)

func (s *ThePirateBayScraper) parseHTMLTable(data []byte) ([]Torrent, error) {
	content := string(data)
	rows := tpbHTMLRowRegex.FindAllStringSubmatch(content, -1)
	if len(rows) == 0 {
		return []Torrent{}, nil
	}

	var results []Torrent
	for _, row := range rows {
		rowContent := row[1]
		linkMatch := tpbHTMLLinkRegex.FindStringSubmatch(rowContent)
		if len(linkMatch) < 3 {
			continue
		}

		title := html.UnescapeString(strings.TrimSpace(stripHTMLTags(linkMatch[2])))
		if title == "" {
			continue
		}

		pageURL := strings.TrimRight(s.siteURL, "/") + linkMatch[1]

		var magnet string
		if mMatch := tpbHTMLMagnetRegex.FindStringSubmatch(rowContent); len(mMatch) >= 2 {
			magnet = html.UnescapeString(mMatch[1])
		}
		hash := NormalizeInfoHash(magnet)
		if hash == "" {
			continue
		}

		var sizeBytes int64
		var sizeFormatted string
		var pubDate time.Time

		if descMatch := tpbHTMLDescRegex.FindStringSubmatch(rowContent); len(descMatch) >= 3 {
			pubDate = parseDateDefensive(descMatch[1])
			sizeBytes, sizeFormatted, _ = ParseSize(descMatch[2])
		}

		cells := tpbHTMLCellRegex.FindAllStringSubmatch(rowContent, -1)
		seeds := 0
		leeches := 0
		if len(cells) >= 3 {
			seeds = ParseIntDefensive(stripHTMLTags(cells[len(cells)-2][1]), 0)
			leeches = ParseIntDefensive(stripHTMLTags(cells[len(cells)-1][1]), 0)
		}

		if magnet == "" {
			magnet = BuildMagnetURI(hash, title, PublicTrackers)
		}

		results = append(results, Torrent{
			Title:         title,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        "The Pirate Bay",
			Category:      "Other",
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

func mapTPBCategory(cat string) string {
	code := ParseIntDefensive(cat, 0)
	switch {
	case code >= 100 && code < 200:
		switch code {
		case 102:
			return "Audio/Audiobook"
		case 104:
			return "Audio/Lossless"
		default:
			return "Audio"
		}
	case code >= 200 && code < 300:
		switch code {
		case 207:
			return "Movies/HD"
		case 208:
			return "TV/HD"
		case 211:
			return "Movies/UHD"
		case 212:
			return "TV/UHD"
		case 201, 202:
			return "Movies"
		case 205:
			return "TV"
		default:
			return "Movies"
		}
	case code >= 300 && code < 400:
		switch code {
		case 302:
			return "PC/Mac"
		case 305:
			return "PC/Mobile-iOS"
		case 306:
			return "PC/Mobile-Android"
		default:
			return "PC"
		}
	case code >= 400 && code < 500:
		switch code {
		case 401:
			return "PC/Games"
		case 402:
			return "Console/Xbox"
		case 403:
			return "Console/PS4"
		default:
			return "Games"
		}
	case code >= 500 && code < 600:
		switch code {
		case 503:
			return "XXX/ImageSet"
		default:
			return "XXX"
		}
	case code >= 600 && code < 700:
		switch code {
		case 601:
			return "Books/EBook"
		case 602:
			return "Books/Comics"
		default:
			return "Books/Other"
		}
	default:
		return "Other"
	}
}

type tpbJSONRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	InfoHash string `json:"info_hash"`
	Leechers string `json:"leechers"`
	Seeders  string `json:"seeders"`
	NumFiles string `json:"num_files"`
	Size     string `json:"size"`
	Username string `json:"username"`
	Added    string `json:"added"`
	Status   string `json:"status"`
	Category string `json:"category"`
}
