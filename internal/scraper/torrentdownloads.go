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
	"strings"
	"time"
)

const (
	// DefaultTorrentDownloadsBaseURL is the default base URL for TorrentDownloads.
	DefaultTorrentDownloadsBaseURL = "https://www.torrentdownloads.pro"
)

// TorrentDownloadsScraper implements SourceScraper for TorrentDownloads.
type TorrentDownloadsScraper struct {
	baseURL string
	client  *http.Client
}

// NewTorrentDownloadsScraper creates a new TorrentDownloads scraper with optional configuration overrides.
func NewTorrentDownloadsScraper(opts ...Option) *TorrentDownloadsScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultTorrentDownloadsBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &TorrentDownloadsScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *TorrentDownloadsScraper) Name() string {
	return "TorrentDownloads"
}

// Search queries TorrentDownloads using its search RSS feed.
func (s *TorrentDownloadsScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
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
	params.Set("type", "search")
	params.Set("search", query)

	if cid := mapCategoryToTorrentDownloadsCID(cfg.Category); cid != "" {
		params.Set("cid", cid)
	}

	endpoint := fmt.Sprintf("%s/rss.xml?%s", strings.TrimRight(cfg.BaseURL, "/"), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("torrentdownloads: create request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrentdownloads: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrentdownloads: server returned status %d", resp.StatusCode)
	}

	return s.ParseHTML(resp.Body)
}

// ParseHTML parses raw RSS 2.0 XML or HTML content from an io.Reader.
func (s *TorrentDownloadsScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("torrentdownloads: read response body: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return []Torrent{}, nil
	}

	// Try XML parsing
	var rss tdRSSEnvelope
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	if err := decoder.Decode(&rss); err == nil && len(rss.Channel.Items) > 0 {
		return s.parseXMLItems(rss.Channel.Items)
	}

	// HTML fallback if XML decoding failed or returned no items
	if strings.Contains(trimmed, "<table") || strings.Contains(trimmed, "inner_container") || strings.Contains(trimmed, "<tr") {
		return s.parseHTMLTable(data)
	}

	return []Torrent{}, nil
}

func (s *TorrentDownloadsScraper) parseXMLItems(items []tdItem) ([]Torrent, error) {
	var results []Torrent

	for _, item := range items {
		title := html.UnescapeString(strings.TrimSpace(item.Title))
		if title == "" {
			continue
		}

		hash := NormalizeInfoHash(item.InfoHash)
		if hash == "" {
			hash = NormalizeInfoHash(item.Link)
		}

		sizeBytes, sizeFormatted, _ := ParseSize(item.Size)
		seeds := ParseIntDefensive(item.Seeders, 0)
		leeches := ParseIntDefensive(item.Leechers, 0)
		pubDate := parseDateDefensive(item.PubDate)
		category := mapTorrentDownloadsCategory(item.Category)

		pageURL := strings.TrimSpace(item.Link)
		if pageURL != "" && !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
			pageURL = strings.TrimRight(s.baseURL, "/") + "/" + strings.TrimLeft(pageURL, "/")
		}

		var magnet string
		if hash != "" {
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
			Source:        "TorrentDownloads",
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       pageURL,
		})
	}

	return results, nil
}

var (
	tdRowRegex    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tdCellRegex   = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	tdLinkRegex   = regexp.MustCompile(`(?is)<a\s+[^>]*href="(/torrent/\d+/[^"]+)"[^>]*>(.*?)</a>`)
	tdMagnetRegex = regexp.MustCompile(`(?i)href="([^"]*magnet:[^"]*)"`)
)

func (s *TorrentDownloadsScraper) parseHTMLTable(data []byte) ([]Torrent, error) {
	content := string(data)
	rows := tdRowRegex.FindAllStringSubmatch(content, -1)
	if len(rows) == 0 {
		return []Torrent{}, nil
	}

	var results []Torrent
	for _, row := range rows {
		cells := tdCellRegex.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 4 {
			continue
		}

		linkMatch := tdLinkRegex.FindStringSubmatch(cells[0][1])
		if len(linkMatch) < 3 {
			continue
		}

		pageURL := strings.TrimRight(s.baseURL, "/") + linkMatch[1]
		title := html.UnescapeString(strings.TrimSpace(stripHTMLTags(linkMatch[2])))
		if title == "" {
			continue
		}

		var magnet string
		if mMatch := tdMagnetRegex.FindStringSubmatch(row[1]); len(mMatch) >= 2 {
			magnet = html.UnescapeString(mMatch[1])
		}
		hash := NormalizeInfoHash(magnet)

		sizeBytes, sizeFormatted, _ := ParseSize(stripHTMLTags(cells[1][1]))
		seeds := ParseIntDefensive(stripHTMLTags(cells[2][1]), 0)
		leeches := ParseIntDefensive(stripHTMLTags(cells[3][1]), 0)

		if hash != "" && magnet == "" {
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
			Source:        "TorrentDownloads",
			Category:      "Other",
			PublishedDate: time.Time{},
			PageURL:       pageURL,
		})
	}

	return results, nil
}

func mapTorrentDownloadsCategory(rawCat string) string {
	cleaned := strings.TrimSpace(rawCat)
	switch strings.ToLower(cleaned) {
	case "anime":
		return "TV/Anime"
	case "books":
		return "Books"
	case "games":
		return "PC/Games"
	case "movies":
		return "Movies"
	case "music":
		return "Audio"
	case "software":
		return "PC"
	case "tv shows", "tv":
		return "TV"
	case "pics":
		return "Other/Misc"
	default:
		if cleaned == "" {
			return "Other"
		}
		return cleaned
	}
}

func mapCategoryToTorrentDownloadsCID(cat string) string {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "anime", "tv/anime":
		return "1"
	case "books":
		return "2"
	case "games", "pc/games":
		return "3"
	case "movies":
		return "4"
	case "music", "audio":
		return "5"
	case "software", "pc":
		return "7"
	case "tv", "tv shows":
		return "8"
	case "other":
		return "9"
	default:
		return ""
	}
}

// XML schema for TorrentDownloads RSS feeds
type tdRSSEnvelope struct {
	XMLName xml.Name  `xml:"rss"`
	Channel tdChannel `xml:"channel"`
}

type tdChannel struct {
	Items []tdItem `xml:"item"`
}

type tdItem struct {
	Title    string `xml:"title"`
	PubDate  string `xml:"pubDate"`
	Category string `xml:"category"`
	Link     string `xml:"link"`
	Size     string `xml:"size"`
	Seeders  string `xml:"seeders"`
	Leechers string `xml:"leechers"`
	InfoHash string `xml:"info_hash"`
}
