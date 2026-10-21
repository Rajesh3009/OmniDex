package feed

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"local-indexer/internal/db"
)

type Generator struct {
	store   *db.Store
	baseURL string
}

func NewGenerator(store *db.Store, baseURL string) *Generator {
	return &Generator{
		store:   store,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// RSS 2.0 Structures
type RSS2Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type RSS2Item struct {
	Title       string         `xml:"title"`
	Description string         `xml:"description"`
	Link        string         `xml:"link"`
	GUID        string         `xml:"guid"`
	PubDate     string         `xml:"pubDate"`
	Category    string         `xml:"category,omitempty"`
	Enclosure   *RSS2Enclosure `xml:"enclosure,omitempty"`
}

type RSS2Channel struct {
	Title         string     `xml:"title"`
	Link          string     `xml:"link"`
	Description   string     `xml:"description"`
	Language      string     `xml:"language"`
	LastBuildDate string     `xml:"lastBuildDate"`
	Items         []RSS2Item `xml:"item"`
}

type RSS2Doc struct {
	XMLName xml.Name    `xml:"rss"`
	Version string      `xml:"version,attr"`
	Channel RSS2Channel `xml:"channel"`
}

// Atom Structures
type AtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type AtomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Updated   string     `xml:"updated"`
	Summary   string     `xml:"summary"`
	Link      []AtomLink `xml:"link"`
	Category  string     `xml:"category,attr,omitempty"`
}

type AtomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Xmlns   string      `xml:"xmlns,attr"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Link    []AtomLink  `xml:"link"`
	Entries []AtomEntry `xml:"entry"`
}

func (g *Generator) parseFilter(r *http.Request) db.SearchFilter {
	q := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")
	source := r.URL.Query().Get("source")
	resolution := r.URL.Query().Get("resolution")
	codec := r.URL.Query().Get("codec")
	quality := r.URL.Query().Get("quality")
	releaseGroup := r.URL.Query().Get("release_group")
	includeSuspicious := r.URL.Query().Get("include_suspicious") == "true" || r.URL.Query().Get("suspicious") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	return db.SearchFilter{
		Query:             q,
		Category:          category,
		Source:            source,
		Resolution:        resolution,
		Codec:             codec,
		Quality:           quality,
		ReleaseGroup:      releaseGroup,
		IncludeSuspicious: includeSuspicious,
		SortBy:            "created_desc",
		Limit:             limit,
		Offset:            0,
	}
}

// HandleRSS serves standard RSS 2.0 XML
func (g *Generator) HandleRSS(w http.ResponseWriter, r *http.Request) {
	filter := g.parseFilter(r)
	items, _, err := g.store.SearchWithFilter(filter)
	if err != nil {
		http.Error(w, "Feed error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	feedTitle := "OmniDex - Dynamic Torrent Feed"
	if filter.Category != "" && !strings.EqualFold(filter.Category, "all") {
		feedTitle += fmt.Sprintf(" [%s]", filter.Category)
	}
	if filter.Query != "" {
		feedTitle += fmt.Sprintf(" - \"%s\"", filter.Query)
	}

	doc := RSS2Doc{
		Version: "2.0",
		Channel: RSS2Channel{
			Title:         feedTitle,
			Link:          g.baseURL,
			Description:   "Real-time indexed torrents from OmniDex personal DHT crawler and Prowlarr sync",
			Language:      "en-us",
			LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
		},
	}

	for _, t := range items {
		magnet := t.MagnetURI
		if magnet == "" {
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.InfoHash, url.QueryEscape(t.Name))
		}

		desc := fmt.Sprintf("Category: %s | Size: %d bytes | Seeders: %d | InfoHash: %s", t.Category, t.Size, t.Seeders, t.InfoHash)
		if t.Resolution != "" {
			desc += " | Res: " + t.Resolution
		}
		if t.Codec != "" {
			desc += " | Codec: " + t.Codec
		}

		doc.Channel.Items = append(doc.Channel.Items, RSS2Item{
			Title:       t.Name,
			Description: desc,
			Link:        magnet,
			GUID:        t.InfoHash,
			PubDate:     t.CreatedTime.Format(time.RFC1123Z),
			Category:    t.Category,
			Enclosure: &RSS2Enclosure{
				URL:    magnet,
				Length: t.Size,
				Type:   "application/x-bittorrent",
			},
		})
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(doc)
}

// HandleAtom serves standard Atom XML (RFC 4287)
func (g *Generator) HandleAtom(w http.ResponseWriter, r *http.Request) {
	filter := g.parseFilter(r)
	items, _, err := g.store.SearchWithFilter(filter)
	if err != nil {
		http.Error(w, "Feed error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	feedTitle := "OmniDex - Dynamic Torrent Feed"
	if filter.Category != "" && !strings.EqualFold(filter.Category, "all") {
		feedTitle += fmt.Sprintf(" [%s]", filter.Category)
	}
	if filter.Query != "" {
		feedTitle += fmt.Sprintf(" - \"%s\"", filter.Query)
	}

	atomFeed := AtomFeed{
		Xmlns:   "http://www.w3.org/2005/Atom",
		ID:      g.baseURL + "/api/v1/feed/atom",
		Title:   feedTitle,
		Updated: time.Now().UTC().Format(time.RFC3339),
		Link: []AtomLink{
			{Href: g.baseURL + r.URL.RequestURI(), Rel: "self", Type: "application/atom+xml"},
			{Href: g.baseURL, Rel: "alternate", Type: "text/html"},
		},
	}

	for _, t := range items {
		magnet := t.MagnetURI
		if magnet == "" {
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.InfoHash, url.QueryEscape(t.Name))
		}

		summary := fmt.Sprintf("Category: %s | Size: %d bytes | Seeders: %d | InfoHash: %s", t.Category, t.Size, t.Seeders, t.InfoHash)
		if t.Resolution != "" {
			summary += " | Res: " + t.Resolution
		}

		entry := AtomEntry{
			ID:       "urn:btih:" + t.InfoHash,
			Title:    t.Name,
			Updated:  t.CreatedTime.Format(time.RFC3339),
			Summary:  summary,
			Category: t.Category,
			Link: []AtomLink{
				{Href: magnet, Rel: "enclosure", Type: "application/x-bittorrent"},
				{Href: fmt.Sprintf("%s/#%s", g.baseURL, t.InfoHash), Rel: "alternate", Type: "text/html"},
			},
		}
		atomFeed.Entries = append(atomFeed.Entries, entry)
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(atomFeed)
}
