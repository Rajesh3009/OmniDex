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
	// DefaultOxTorrentBaseURL is the default base URL for OxTorrent.
	DefaultOxTorrentBaseURL = "https://ww1.oxtorrent.run"
)

// OxTorrentScraper implements SourceScraper for OxTorrent.
type OxTorrentScraper struct {
	baseURL string
	client  *http.Client
}

// NewOxTorrentScraper creates a new OxTorrent scraper with optional configuration overrides.
func NewOxTorrentScraper(opts ...Option) *OxTorrentScraper {
	cfg := DefaultOptions()
	cfg.BaseURL = DefaultOxTorrentBaseURL
	for _, opt := range opts {
		opt(&cfg)
	}
	return &OxTorrentScraper{
		baseURL: cfg.BaseURL,
		client:  cfg.HTTPClient,
	}
}

// Name returns the canonical name of the scraper.
func (s *OxTorrentScraper) Name() string {
	return "OxTorrent"
}

type oxListingItem struct {
	Title    string
	PageURL  string
	Size     string
	Seeds    int
	Leeches  int
	Category string
}

var (
	oxTableRowRegex  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	oxCatRegex       = regexp.MustCompile(`(?is)<td[^>]*class="[^"]*liste-accueil-type[^"]*"[^>]*title="([^"]*)"`)
	oxCatAltRegex    = regexp.MustCompile(`(?is)<i[^>]*class="[^"]*icon-[^"]*"[^>]*title="([^"]*)"`)
	oxLinkRegex      = regexp.MustCompile(`(?is)<td[^>]*class="[^"]*liste-accueil-nom[^"]*"[^>]*>.*?<a[^>]*href="(/detail/\d+[^"]*)"(?:[^>]*title="([^"]*)")?[^>]*>(.*?)</a>`)
	oxLinkAltRegex   = regexp.MustCompile(`(?is)<a[^>]*href="(/detail/\d+[^"]*)"(?:[^>]*title="([^"]*)")?[^>]*>(.*?)</a>`)
	oxSizeRegex      = regexp.MustCompile(`(?is)<td[^>]*class="[^"]*liste-accueil-taille[^"]*"[^>]*>(.*?)</td>`)
	oxSeedsRegex     = regexp.MustCompile(`(?is)<(?:td|span)[^>]*class="[^"]*sources[^"]*"[^>]*>.*?(\d+).*?</(?:td|span)>`)
	oxLeechesRegex   = regexp.MustCompile(`(?is)<(?:td|span)[^>]*class="[^"]*clients[^"]*"[^>]*>.*?(\d+).*?</(?:td|span)>`)

	oxDetailMagnetRegex = regexp.MustCompile(`(?i)href="([^"]*magnet:\?[^"]*)"`)
	oxDetailDateRegex   = regexp.MustCompile(`(?i)<span[^>]*class="icon_date"[^>]*>.*?(\d{1,2})/(\d{1,2})/(\d{4})`)
	oxDetailDateAltRegex = regexp.MustCompile(`(?i)(\d{1,2})/(\d{1,2})/(\d{4})`)
	oxDetailSizeFallback = regexp.MustCompile(`(?i)Poids\s+du\s+fichier:</td>\s*<td[^>]*><strong>([^<]*)</strong>`)
	oxDetailTrackerRegex = regexp.MustCompile(`(?i)[&?]tr=([^&"'>\s]+)`)
)

// Search queries OxTorrent search listing and fetches detail pages for magnet links with bounded concurrency.
func (s *OxTorrentScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	cfg := Options{
		BaseURL:    s.baseURL,
		HTTPClient: s.client,
		Timeout:    15 * time.Second,
		Limit:      15,
		Page:       1,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	searchURL := fmt.Sprintf("%s/recherche/%s", strings.TrimRight(cfg.BaseURL, "/"), url.PathEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oxtorrent: create search request: %w", err)
	}
	req.Header.Set("User-Agent", "OmniDex/1.3 (compatible; ScraperBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxtorrent: search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oxtorrent: server returned status %d", resp.StatusCode)
	}

	searchBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oxtorrent: read search response: %w", err)
	}

	listings := s.extractListings(string(searchBody), cfg.BaseURL)
	if len(listings) == 0 {
		return s.ParseHTML(strings.NewReader(string(searchBody)))
	}

	maxFetch := cfg.Limit
	if maxFetch <= 0 || maxFetch > len(listings) {
		maxFetch = len(listings)
	}
	if maxFetch > 15 {
		maxFetch = 15
	}
	toFetch := listings[:maxFetch]

	results := make([]Torrent, 0, len(toFetch))
	var mu sync.Mutex

	type job struct {
		item oxListingItem
	}
	jobs := make(chan job, len(toFetch))
	for _, it := range toFetch {
		jobs <- job{item: it}
	}
	close(jobs)

	concurrency := 3
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

				time.Sleep(100 * time.Millisecond)

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

				torrents, pErr := s.parseDetailPage(dResp.Body, j.item)
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

// ParseHTML parses raw OxTorrent search table rows or detail HTML from an io.Reader.
func (s *OxTorrentScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	if r == nil {
		return []Torrent{}, nil
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("oxtorrent: read input: %w", err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return []Torrent{}, nil
	}

	// Case 1: Detail page (contains magnet link)
	if oxDetailMagnetRegex.MatchString(content) {
		return s.parseDetailPage(strings.NewReader(content), oxListingItem{PageURL: s.baseURL})
	}

	// Case 2: Search listing table
	listings := s.extractListings(content, s.baseURL)
	var results []Torrent
	for _, it := range listings {
		sizeBytes, sizeFormatted, _ := ParseSize(it.Size)
		results = append(results, Torrent{
			Title:         it.Title,
			PageURL:       it.PageURL,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         it.Seeds,
			Leeches:       it.Leeches,
			Source:        "OxTorrent",
			Category:      mapOxTorrentCategory(it.Category),
		})
	}

	return results, nil
}

func (s *OxTorrentScraper) extractListings(content, baseURL string) []oxListingItem {
	var items []oxListingItem

	rows := oxTableRowRegex.FindAllStringSubmatch(content, -1)
	for _, row := range rows {
		rowContent := row[1]
		// Skip cover strip rows or table headers
		if !strings.Contains(rowContent, "/detail/") {
			continue
		}

		linkMatch := oxLinkRegex.FindStringSubmatch(rowContent)
		if len(linkMatch) < 4 {
			linkMatch = oxLinkAltRegex.FindStringSubmatch(rowContent)
			if len(linkMatch) < 4 {
				continue
			}
		}

		rawPath := strings.TrimSpace(linkMatch[1])
		pageURL := rawPath
		if !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
			pageURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(rawPath, "/")
		}

		title := strings.TrimSpace(linkMatch[2])
		if title == "" {
			title = stripHTMLTags(linkMatch[3])
		}
		title = html.UnescapeString(title)
		if title == "" {
			continue
		}

		// Category
		cat := ""
		if catMatch := oxCatRegex.FindStringSubmatch(rowContent); len(catMatch) >= 2 {
			cat = strings.TrimSpace(catMatch[1])
		} else if catMatch := oxCatAltRegex.FindStringSubmatch(rowContent); len(catMatch) >= 2 {
			cat = strings.TrimSpace(catMatch[1])
		}

		// Size
		sizeStr := ""
		if sizeMatch := oxSizeRegex.FindStringSubmatch(rowContent); len(sizeMatch) >= 2 {
			sizeStr = stripHTMLTags(sizeMatch[1])
		}

		// Seeds & Leeches
		seeds := 0
		if sMatch := oxSeedsRegex.FindStringSubmatch(rowContent); len(sMatch) >= 2 {
			seeds = ParseIntDefensive(sMatch[1], 0)
		}
		leeches := 0
		if lMatch := oxLeechesRegex.FindStringSubmatch(rowContent); len(lMatch) >= 2 {
			leeches = ParseIntDefensive(lMatch[1], 0)
		}

		items = append(items, oxListingItem{
			Title:    title,
			PageURL:  pageURL,
			Size:     sizeStr,
			Seeds:    seeds,
			Leeches:  leeches,
			Category: cat,
		})
	}

	return items
}

func (s *OxTorrentScraper) parseDetailPage(r io.Reader, item oxListingItem) ([]Torrent, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var magnet string
	if mMatch := oxDetailMagnetRegex.FindStringSubmatch(content); len(mMatch) >= 2 {
		magnet = html.UnescapeString(mMatch[1])
	}
	hash := NormalizeInfoHash(magnet)
	if hash == "" {
		return nil, fmt.Errorf("oxtorrent: magnet link or infohash not found")
	}

	title := item.Title
	if title == "" {
		// Extract from title tag or page header
		titleTagRegex := regexp.MustCompile(`(?is)<title>(.*?)(?:-|\|).*?</title>`)
		if tm := titleTagRegex.FindStringSubmatch(content); len(tm) >= 2 {
			title = html.UnescapeString(strings.TrimSpace(stripHTMLTags(tm[1])))
		}
	}
	if title == "" {
		title = "OxTorrent " + hash[:8]
	}

	sizeBytes, sizeFormatted, _ := ParseSize(item.Size)
	if sizeBytes <= 0 {
		if sizeMatch := oxDetailSizeFallback.FindStringSubmatch(content); len(sizeMatch) >= 2 {
			sizeBytes, sizeFormatted, _ = ParseSize(sizeMatch[1])
		}
	}

	var pubDate time.Time
	if dateMatch := oxDetailDateRegex.FindStringSubmatch(content); len(dateMatch) >= 4 {
		day, month, year := dateMatch[1], dateMatch[2], dateMatch[3]
		dateStr := fmt.Sprintf("%02s/%02s/%s", day, month, year)
		pubDate = parseFrenchDate(dateStr)
	} else if dateMatch := oxDetailDateAltRegex.FindStringSubmatch(content); len(dateMatch) >= 4 {
		day, month, year := dateMatch[1], dateMatch[2], dateMatch[3]
		dateStr := fmt.Sprintf("%02s/%02s/%s", day, month, year)
		pubDate = parseFrenchDate(dateStr)
	}

	category := mapOxTorrentCategory(item.Category)

	return []Torrent{
		{
			Title:         title,
			MagnetURI:     magnet,
			InfoHash:      hash,
			Size:          sizeBytes,
			SizeFormatted: sizeFormatted,
			Seeds:         item.Seeds,
			Leeches:       item.Leeches,
			Source:        "OxTorrent",
			Category:      category,
			PublishedDate: pubDate,
			PageURL:       item.PageURL,
		},
	}, nil
}

func parseFrenchDate(d string) time.Time {
	t, err := time.Parse("02/01/2006", d)
	if err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func mapOxTorrentCategory(cat string) string {
	cleaned := strings.ToLower(strings.TrimSpace(cat))
	switch {
	case strings.Contains(cleaned, "film"):
		return "Movies"
	case strings.Contains(cleaned, "série") || strings.Contains(cleaned, "serie"):
		return "TV"
	case strings.Contains(cleaned, "anime"):
		return "TV/Anime"
	case strings.Contains(cleaned, "sport"):
		return "TV/Sport"
	case strings.Contains(cleaned, "musique"):
		return "Audio"
	case strings.Contains(cleaned, "ebook"):
		return "Books/EBook"
	case strings.Contains(cleaned, "livre"):
		return "Books"
	case strings.Contains(cleaned, "logiciel"):
		return "PC"
	case strings.Contains(cleaned, "jeux-pc") || strings.Contains(cleaned, "jeu-pc"):
		return "PC/Games"
	case strings.Contains(cleaned, "jeux-console") || strings.Contains(cleaned, "console"):
		return "Console"
	default:
		return "Other"
	}
}
