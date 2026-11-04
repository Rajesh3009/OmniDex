package scraper

import (
	"fmt"
	"time"
)

// Torrent represents standardized torrent metadata extracted across all sources.
type Torrent struct {
	Title         string    `json:"title"`          // Cleaned title of the torrent
	MagnetURI     string    `json:"magnet_uri"`     // Complete magnet URI (magnet:?xt=urn:btih:...)
	InfoHash      string    `json:"info_hash"`      // Normalized 40-char lowercase hex infohash
	Size          int64     `json:"size"`           // Size in bytes
	SizeFormatted string    `json:"size_formatted"` // Human readable string (e.g. "1.50 GB")
	Seeds         int       `json:"seeds"`          // Current active seeders
	Leeches       int       `json:"leeches"`        // Current active leechers / peers
	Source        string    `json:"source"`         // Source identifier (e.g. "YTS", "Nyaa.si")
	Category      string    `json:"category"`       // Torznab or source category (e.g. "Movies/HD")
	PublishedDate time.Time `json:"published_date"` // Upload date/time in UTC
	PageURL       string    `json:"page_url"`       // Canonical web URL of the torrent release
}

// FormattedSize returns a standardized human-readable string representation of Size.
// If SizeFormatted is already populated, it is returned directly.
// Otherwise, it computes a robust decimal/binary representation (e.g. "1.50 GB", "500 B").
func (t *Torrent) FormattedSize() string {
	if t == nil {
		return "0 B"
	}
	if t.SizeFormatted != "" {
		return t.SizeFormatted
	}
	if t.Size <= 0 {
		return "0 B"
	}
	const unit = 1024
	if t.Size < unit {
		return fmt.Sprintf("%d B", t.Size)
	}
	div, exp := int64(unit), 0
	for n := t.Size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(t.Size)/float64(div), units[exp])
}
