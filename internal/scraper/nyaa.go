package scraper

import (
	"bytes"
	"context"
	"encoding/xml"
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
	// DefaultNyaaBaseURL is the primary base URL for Nyaa.si.
	DefaultNyaaBaseURL = "https://nyaa.si"
)

// NyaaTrackers holds the dedicated Nyaa tracker and canonical public announce mirrors.
var NyaaTrackers = []string{
	"http://nyaa.tracker.wf:7777/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// NyaaScraper implements SourceScraper for Nyaa.si.
type NyaaScraper struct {
	baseURL string
	client  *http.Client
}

// NewNyaaScraper creates a new Nyaa.si scraper with optional configuration overrides.
func NewNyaaScraper(opts ...Option) *NyaaScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultNyaaBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &NyaaScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *NyaaScraper) Name() string {
	return "Nyaa.si"
}

// Search queries Nyaa.si using its RSS feed.
func (s *NyaaScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
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
		return nil, fmt.Errorf("nyaa: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nyaa: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nyaa: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

// ParseHTML parses raw RSS 2.0 XML or HTML table content from an io.Reader.
func (s *NyaaScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	return parseNyaaFeed(r, "Nyaa.si", s.baseURL, NyaaTrackers, mapNyaaCategory)
}

// mapNyaaCategory maps Nyaa category codes (e.g. "1_2") to Torznab-compatible categories.
func mapNyaaCategory(catID string) string {
	cat := strings.TrimSpace(catID)
	switch {
	case strings.HasPrefix(cat, "1_"):
		return "TV/Anime"
	case strings.HasPrefix(cat, "2_"):
		return "Audio"
	case strings.HasPrefix(cat, "3_"):
		return "Books"
	case strings.HasPrefix(cat, "4_"):
		return "TV/Foreign"
	case strings.HasPrefix(cat, "6_"):
		return "PC/Games"
	default:
		return "Other"
	}
}

// XML schema for Nyaa and Sukebei RSS feeds
type nyaaRSSEnvelope struct {
	XMLName xml.Name    `xml:"rss"`
	Channel nyaaChannel `xml:"channel"`
}

type nyaaChannel struct {
	Title string     `xml:"title"`
	Items []nyaaItem `xml:"item"`
}

type nyaaItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Guid        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Seeders     string `xml:"seeders"`
	Leechers    string `xml:"leechers"`
	InfoHash    string `xml:"infoHash"`
	CategoryId  string `xml:"categoryId"`
	Category    string `xml:"category"`
	Size        string `xml:"size"`
	Description string `xml:"description"`
}

// parseNyaaFeed parses Nyaa or Sukebei feeds (RSS XML with HTML table fallback).
func parseNyaaFeed(r io.Reader, sourceName, baseURL string, trackers []string, catMapper func(string) string) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s: read response body: %w", strings.ToLower(sourceName), err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return []Torrent{}, nil
	}

	// Try XML parsing first
	var rss nyaaRSSEnvelope
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	if err := decoder.Decode(&rss); err == nil && len(rss.Channel.Items) > 0 {
		return parseNyaaXMLItems(rss.Channel.Items, sourceName, baseURL, trackers, catMapper)
	}

	// If XML decoding didn't find items or failed, check for HTML table markup
	if strings.Contains(trimmed, "<table") || strings.Contains(trimmed, "torrent-list") || strings.Contains(trimmed, "<tr") {
		return parseNyaaHTMLTable(data, sourceName, baseURL, trackers, catMapper)
	}

	// If no items found, return empty slice without error
	return []Torrent{}, nil
}

func parseNyaaXMLItems(items []nyaaItem, sourceName, baseURL string, trackers []string, catMapper func(string) string) ([]Torrent, error) {
	var results []Torrent

	for _, item := range items {
		rawTitle := html.UnescapeString(strings.TrimSpace(item.Title))
		if rawTitle == "" {
			continue
		}

		hash := NormalizeInfoHash(item.InfoHash)
		if hash == "" {
			// If not in infoHash element, attempt extraction from link or guid
			hash = NormalizeInfoHash(item.Link)
		}
		if hash == "" {
			hash = NormalizeInfoHash(item.Guid)
		}

		sizeBytes, sizeFormatted, _ := ParseSize(item.Size)
		seeds := ParseIntDefensive(item.Seeders, 0)
		leeches := ParseIntDefensive(item.Leechers, 0)
		pubDate := parseDateDefensive(item.PubDate)

		catID := item.CategoryId
		if catID == "" {
			catID = item.Category
		}
		category := catMapper(catID)

		pageURL := strings.TrimSpace(item.Guid)
		if pageURL == "" {
			pageURL = strings.TrimSpace(item.Link)
		}
		if pageURL != "" && !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
			pageURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(pageURL, "/")
		}

		var magnet string
		if hash != "" {
			magnet = BuildMagnetURI(hash, rawTitle, trackers)
		}

		results = append(results, Torrent{
			Title:         rawTitle,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        sourceName,
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

var (
	nyaaRowRegex    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	nyaaCellRegex   = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	nyaaMagnetRegex = regexp.MustCompile(`(?i)href="([^"]*magnet:[^"]*)"`)
	nyaaCatRegex    = regexp.MustCompile(`(?i)(?:/|\?c=)(\d+_\d+)`)
	nyaaLinkRegex   = regexp.MustCompile(`(?is)<a\s+[^>]*href="(/view/\d+)"[^>]*>(.*?)</a>`)
	nyaaDateRegex   = regexp.MustCompile(`(?i)data-timestamp="(\d+)"`)
	tagStripRegex   = regexp.MustCompile(`(?s)<[^>]+>`)
)

func parseNyaaHTMLTable(data []byte, sourceName, baseURL string, trackers []string, catMapper func(string) string) ([]Torrent, error) {
	content := string(data)
	rows := nyaaRowRegex.FindAllStringSubmatch(content, -1)
	if len(rows) == 0 {
		return []Torrent{}, nil
	}

	var results []Torrent
	for _, row := range rows {
		rowContent := row[1]
		cells := nyaaCellRegex.FindAllStringSubmatch(rowContent, -1)
		if len(cells) < 5 {
			continue
		}

		// Category extraction
		catCode := ""
		if catMatch := nyaaCatRegex.FindStringSubmatch(cells[0][1]); len(catMatch) >= 2 {
			catCode = catMatch[1]
		}
		category := catMapper(catCode)

		// Title & PageURL extraction from column 2
		titleCol := cells[1][1]
		linkMatch := nyaaLinkRegex.FindStringSubmatch(titleCol)
		var title, pageURL string
		if len(linkMatch) >= 3 {
			pageURL = strings.TrimRight(baseURL, "/") + linkMatch[1]
			title = html.UnescapeString(strings.TrimSpace(stripHTMLTags(linkMatch[2])))
		} else {
			title = html.UnescapeString(strings.TrimSpace(stripHTMLTags(titleCol)))
		}
		if title == "" {
			continue
		}

		// Magnet extraction from column 3
		linksCol := cells[2][1]
		var magnet string
		if mMatch := nyaaMagnetRegex.FindStringSubmatch(linksCol); len(mMatch) >= 2 {
			magnet = html.UnescapeString(mMatch[1])
		}
		hash := NormalizeInfoHash(magnet)

		// Size extraction from column 4
		sizeText := stripHTMLTags(cells[3][1])
		sizeBytes, sizeFormatted, _ := ParseSize(sizeText)

		// Date extraction from column 5
		dateCol := cells[4][1]
		var pubDate time.Time
		if dMatch := nyaaDateRegex.FindStringSubmatch(dateCol); len(dMatch) >= 2 {
			sec, _ := strconv.ParseInt(dMatch[1], 10, 64)
			if sec > 0 {
				pubDate = time.Unix(sec, 0).UTC()
			}
		}
		if pubDate.IsZero() {
			pubDate = parseDateDefensive(stripHTMLTags(dateCol))
		}

		// Seeds from column 6
		seeds := 0
		if len(cells) >= 6 {
			seeds = ParseIntDefensive(stripHTMLTags(cells[5][1]), 0)
		}

		// Leeches from column 7
		leeches := 0
		if len(cells) >= 7 {
			leeches = ParseIntDefensive(stripHTMLTags(cells[6][1]), 0)
		}

		if hash != "" && magnet == "" {
			magnet = BuildMagnetURI(hash, title, trackers)
		}

		results = append(results, Torrent{
			Title:         title,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         seeds,
			Leeches:       leeches,
			Source:        sourceName,
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

func stripHTMLTags(s string) string {
	return strings.TrimSpace(tagStripRegex.ReplaceAllString(s, " "))
}

// parseDateDefensive attempts parsing multiple timestamp formats into UTC time.
func parseDateDefensive(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}

	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 MST",
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"02/01/2006 15:04:05",
		"02/01/2006",
		"2 Jan 2006",
		"02 Jan 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, trimmed); err == nil {
			return t.UTC()
		}
	}

	// Try integer unix timestamp
	if sec, err := strconv.ParseInt(trimmed, 10, 64); err == nil && sec > 0 {
		return time.Unix(sec, 0).UTC()
	}

	return time.Time{}
}
