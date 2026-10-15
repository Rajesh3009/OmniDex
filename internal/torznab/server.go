package torznab

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"local-indexer/internal/db"
)

type Server struct {
	store   *db.Store
	apiKey  string
	baseURL string
}

func NewServer(store *db.Store, apiKey, baseURL string) *Server {
	return &Server{
		store:   store,
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// XML Data Structures for Torznab Specification

type TorznabAttr struct {
	XMLName xml.Name `xml:"torznab:attr"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type Item struct {
	Title       string        `xml:"title"`
	GUID        string        `xml:"guid"`
	Comments    string        `xml:"comments,omitempty"`
	PubDate     string        `xml:"pubDate"`
	Size        int64         `xml:"size"`
	Link        string        `xml:"link"`
	Category    string        `xml:"category"`
	Enclosure   Enclosure     `xml:"enclosure"`
	TorznabAttr []TorznabAttr `xml:"torznab:attr"`
}

type Channel struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	Items       []Item `xml:"item"`
}

type RSSFeed struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Torznab string   `xml:"xmlns:torznab,attr"`
	Channel Channel  `xml:"channel"`
}

type CapsServer struct {
	Version string `xml:"version,attr"`
	Title   string `xml:"title,attr"`
}

type CapsLimits struct {
	Max     int `xml:"max,attr"`
	Default int `xml:"default,attr"`
}

type CapsSearching struct {
	Search      CapsSearchType `xml:"search"`
	TvSearch    CapsSearchType `xml:"tv-search"`
	MovieSearch CapsSearchType `xml:"movie-search"`
	MusicSearch CapsSearchType `xml:"music-search"`
	BookSearch  CapsSearchType `xml:"book-search"`
}

type CapsSearchType struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type CapsCategory struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

type CapsRoot struct {
	XMLName    xml.Name       `xml:"caps"`
	Server     CapsServer     `xml:"server"`
	Limits     CapsLimits     `xml:"limits"`
	Searching  CapsSearching  `xml:"searching"`
	Categories []CapsCategory `xml:"categories>category"`
}

func (s *Server) HandleTorznab(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Verify API key if configured
	if s.apiKey != "" {
		providedKey := query.Get("apikey")
		if providedKey == "" {
			providedKey = r.Header.Get("X-Api-Key")
		}
		if providedKey != s.apiKey {
			http.Error(w, "Unauthorized: Invalid apikey", http.StatusUnauthorized)
			return
		}
	}

	tType := query.Get("t")
	if tType == "" {
		tType = "caps"
	}

	switch tType {
	case "caps":
		s.handleCaps(w, r)
	case "search", "tvsearch", "movie", "music", "book":
		s.handleSearch(w, r)
	default:
		s.handleError(w, 203, fmt.Sprintf("Function not available: %s", tType))
	}
}

func (s *Server) handleCaps(w http.ResponseWriter, r *http.Request) {
	caps := CapsRoot{
		Server: CapsServer{
			Version: "1.0",
			Title:   "Local Torrent Indexer & Hybrid Prowlarr Bridge",
		},
		Limits: CapsLimits{
			Max:     100,
			Default: 50,
		},
		Searching: CapsSearching{
			Search: CapsSearchType{
				Available:       "yes",
				SupportedParams: "q",
			},
			TvSearch: CapsSearchType{
				Available:       "yes",
				SupportedParams: "q,season,ep",
			},
			MovieSearch: CapsSearchType{
				Available:       "yes",
				SupportedParams: "q,imdbid",
			},
			MusicSearch: CapsSearchType{
				Available:       "yes",
				SupportedParams: "q",
			},
			BookSearch: CapsSearchType{
				Available:       "yes",
				SupportedParams: "q",
			},
		},
		Categories: []CapsCategory{
			{ID: 2000, Name: "Movies"},
			{ID: 5000, Name: "TV"},
			{ID: 3000, Name: "Audio"},
			{ID: 4000, Name: "PC"},
			{ID: 7000, Name: "Books"},
			{ID: 8000, Name: "Other"},
		},
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(caps)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	qParams := r.URL.Query()
	qText := qParams.Get("q")
	limit, _ := strconv.Atoi(qParams.Get("limit"))
	offset, _ := strconv.Atoi(qParams.Get("offset"))

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	torrents, _, err := s.store.Search(qText, limit, offset)
	if err != nil {
		s.handleError(w, 900, "Internal search error: "+err.Error())
		return
	}

	var items []Item
	for _, t := range torrents {
		catID := mapCategoryToTorznab(t.Category)
		magnet := t.MagnetURI
		if magnet == "" {
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.InfoHash, t.Name)
		}

		item := Item{
			Title:    t.Name,
			GUID:     t.InfoHash,
			Comments: fmt.Sprintf("%s/torrent/%s", s.baseURL, t.InfoHash),
			PubDate:  t.CreatedTime.Format(time.RFC1123Z),
			Size:     t.Size,
			Link:     magnet,
			Category: strconv.Itoa(catID),
			Enclosure: Enclosure{
				URL:    magnet,
				Length: t.Size,
				Type:   "application/x-bittorrent",
			},
			TorznabAttr: []TorznabAttr{
				{Name: "infohash", Value: t.InfoHash},
				{Name: "size", Value: strconv.FormatInt(t.Size, 10)},
				{Name: "seeders", Value: strconv.Itoa(t.Seeders)},
				{Name: "peers", Value: strconv.Itoa(t.Seeders + t.Leechers)},
				{Name: "category", Value: strconv.Itoa(catID)},
				{Name: "magneturl", Value: magnet},
			},
		}
		items = append(items, item)
	}

	feed := RSSFeed{
		Version: "2.0",
		Torznab: "http://torznab.com/schemas/2015/feed",
		Channel: Channel{
			Title:       "Local Torrent Indexer Torznab Feed",
			Description: "Torznab compatible feed for Prowlarr/Radarr/Sonarr",
			Link:        s.baseURL,
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(feed)
}

func (s *Server) handleError(w http.ResponseWriter, code int, description string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<error code="%d" description="%s"/>`, code, description)))
}

func mapCategoryToTorznab(cat string) int {
	switch strings.ToLower(cat) {
	case "movies", "movie":
		return 2000
	case "tv", "series":
		return 5000
	case "audio", "music":
		return 3000
	case "pc", "software":
		return 4000
	case "books", "book":
		return 7000
	default:
		return 8000
	}
}
