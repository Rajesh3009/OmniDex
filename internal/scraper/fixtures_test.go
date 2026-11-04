package scraper

import (
	"bytes"
	"context"
	"embed"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/*
var testdataFS embed.FS

// loadFixture reads a fixture file from the embedded testdata directory.
func loadFixture(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("failed to read fixture %q: %v", filename, err)
	}
	return data
}

// -----------------------------------------------------------------------------
// 1. YTS Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_YTS_ParseHTML(t *testing.T) {
	s := NewYTSScraper()

	t.Run("valid multi-movie fixture with multiple qualities", func(t *testing.T) {
		data := loadFixture(t, "yts.json")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing yts.json: %v", err)
		}

		if len(results) != 5 {
			t.Fatalf("expected 5 torrents across 2 movies, got %d", len(results))
		}

		// Torrent 0: 720p of Movie 1
		t0 := results[0]
		if t0.Title != "Fixture Movie (2024)" {
			t.Errorf("expected title 'Fixture Movie (2024)', got %q", t0.Title)
		}
		if t0.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
			t.Errorf("expected lowercase infohash, got %q", t0.InfoHash)
		}
		if t0.Category != "Movies/HD" {
			t.Errorf("expected category 'Movies/HD', got %q", t0.Category)
		}
		if t0.Size != 874575299 {
			t.Errorf("expected size 874575299, got %d", t0.Size)
		}
		if t0.Seeds != 12 || t0.Leeches != 3 {
			t.Errorf("expected seeds=12, leeches=3, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
		}
		if t0.Source != "YTS" {
			t.Errorf("expected source 'YTS', got %q", t0.Source)
		}
		if t0.PageURL != "https://yts.mx/movies/fixture-movie-2024" {
			t.Errorf("expected pageURL 'https://yts.mx/movies/fixture-movie-2024', got %q", t0.PageURL)
		}
		if t0.PublishedDate.Unix() != 1700000000 {
			t.Errorf("expected published date 1700000000, got %v", t0.PublishedDate)
		}
		if !strings.HasPrefix(t0.MagnetURI, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567") {
			t.Errorf("expected magnet to contain infohash, got %q", t0.MagnetURI)
		}

		// Torrent 1: 1080p of Movie 1
		t1 := results[1]
		if t1.Category != "Movies/HD" {
			t.Errorf("expected category 'Movies/HD' for 1080p, got %q", t1.Category)
		}
		if t1.Size != 1621350154 {
			t.Errorf("expected size 1621350154, got %d", t1.Size)
		}

		// Torrent 2: 2160p of Movie 1
		t2 := results[2]
		if t2.Category != "Movies/UHD" {
			t.Errorf("expected category 'Movies/UHD' for 2160p, got %q", t2.Category)
		}
		if t2.Seeds != 40 || t2.Leeches != 10 {
			t.Errorf("expected seeds=40, leeches=10, got seeds=%d, leeches=%d", t2.Seeds, t2.Leeches)
		}

		// Torrent 3: 480p of Movie 2
		t3 := results[3]
		if t3.Title != "Second Feature Film (2023)" {
			t.Errorf("expected title 'Second Feature Film (2023)', got %q", t3.Title)
		}
		if t3.Category != "Movies/SD" {
			t.Errorf("expected category 'Movies/SD' for 480p, got %q", t3.Category)
		}

		// Torrent 4: 3D of Movie 2
		t4 := results[4]
		if t4.Category != "Movies/3D" {
			t.Errorf("expected category 'Movies/3D' for 3D, got %q", t4.Category)
		}
	})

	t.Run("empty search fixture movie_count 0", func(t *testing.T) {
		data := loadFixture(t, "yts_empty.json")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing yts_empty.json: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 torrents for empty fixture, got %d", len(results))
		}
	})
}

// -----------------------------------------------------------------------------
// 2. Nyaa.si Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_Nyaa_ParseHTML(t *testing.T) {
	s := NewNyaaScraper()
	data := loadFixture(t, "nyaa.xml")

	results, err := s.ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error parsing nyaa.xml: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 items from nyaa.xml, got %d", len(results))
	}

	// Item 0: Anime (1_2 -> TV/Anime) with entity unescaping
	t0 := results[0]
	if t0.Title != "Show & Tell - 01 (1080p) ['Sub']" {
		t.Errorf("expected unescaped title \"Show & Tell - 01 (1080p) ['Sub']\", got %q", t0.Title)
	}
	if t0.InfoHash != "aec9b1182f520e27af38955dce4cec9118ac115f" {
		t.Errorf("expected infohash 'aec9b1182f520e27af38955dce4cec9118ac115f', got %q", t0.InfoHash)
	}
	if t0.Category != "TV/Anime" {
		t.Errorf("expected category 'TV/Anime', got %q", t0.Category)
	}
	if t0.Size != 1610612736 { // 1.5 GiB
		t.Errorf("expected size 1610612736 bytes (1.5 GiB), got %d", t0.Size)
	}
	if t0.Seeds != 12 || t0.Leeches != 3 {
		t.Errorf("expected seeds=12, leeches=3, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
	}
	if t0.Source != "Nyaa.si" {
		t.Errorf("expected source 'Nyaa.si', got %q", t0.Source)
	}
	if t0.PageURL != "https://nyaa.si/view/100001" {
		t.Errorf("expected pageURL 'https://nyaa.si/view/100001', got %q", t0.PageURL)
	}
	if t0.PublishedDate.IsZero() || t0.PublishedDate.Year() != 2026 {
		t.Errorf("expected published date in 2026, got %v", t0.PublishedDate)
	}
	if !strings.Contains(t0.MagnetURI, "nyaa.tracker.wf") || !strings.Contains(t0.MagnetURI, "7777") {
		t.Errorf("expected Nyaa tracker in magnet URI, got %q", t0.MagnetURI)
	}

	// Item 1: Audio (2_1 -> Audio) with uppercase hash and entity
	t1 := results[1]
	if t1.Title != "Café & 日本語 Original Soundtrack" {
		t.Errorf("expected unescaped title 'Café & 日本語 Original Soundtrack', got %q", t1.Title)
	}
	if t1.InfoHash != "b0c1d2e3f405162738495a6b7c8d9e0f1a2b3c4d" {
		t.Errorf("expected normalized lowercase infohash, got %q", t1.InfoHash)
	}
	if t1.Category != "Audio" {
		t.Errorf("expected category 'Audio', got %q", t1.Category)
	}
	if t1.Size != 734527488 { // 700.5 MiB
		t.Errorf("expected size 734527488 bytes (700.5 MiB), got %d", t1.Size)
	}

	// Item 2: Books (3_1 -> Books)
	t2 := results[2]
	if t2.Category != "Books" {
		t.Errorf("expected category 'Books', got %q", t2.Category)
	}
	if t2.Seeds != 15 || t2.Leeches != 1 {
		t.Errorf("expected seeds=15, leeches=1, got seeds=%d, leeches=%d", t2.Seeds, t2.Leeches)
	}
}

// -----------------------------------------------------------------------------
// 3. Sukebei Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_Sukebei_ParseHTML(t *testing.T) {
	s := NewSukebeiScraper()
	data := loadFixture(t, "sukebei.xml")

	results, err := s.ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error parsing sukebei.xml: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 items from sukebei.xml, got %d", len(results))
	}

	// Item 0: Category 2_2 -> XXX, port 8888 tracker
	t0 := results[0]
	if t0.Title != "Fixture Clip 01 & Visuals" {
		t.Errorf("expected title 'Fixture Clip 01 & Visuals', got %q", t0.Title)
	}
	if t0.Category != "XXX" {
		t.Errorf("expected category 'XXX' for 2_2, got %q", t0.Category)
	}
	if t0.Size != 1181116006 { // 1.1 GiB
		t.Errorf("expected size 1181116006 bytes, got %d", t0.Size)
	}
	if t0.Seeds != 1 || t0.Leeches != 0 {
		t.Errorf("expected seeds=1, leeches=0, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
	}
	if t0.Source != "Sukebei" {
		t.Errorf("expected source 'Sukebei', got %q", t0.Source)
	}
	if !strings.Contains(t0.MagnetURI, "sukebei.tracker.wf") || !strings.Contains(t0.MagnetURI, "8888") {
		t.Errorf("expected Sukebei port 8888 tracker in magnet, got %q", t0.MagnetURI)
	}

	// Item 1: Category 1_2 -> XXX/ImageSet
	t1 := results[1]
	if t1.Category != "XXX/ImageSet" {
		t.Errorf("expected category 'XXX/ImageSet' for 1_2, got %q", t1.Category)
	}
	if t1.Seeds != 9 || t1.Leeches != 2 {
		t.Errorf("expected seeds=9, leeches=2, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}

	// Item 2: Category 1_3 -> XXX/Other
	t2 := results[2]
	if t2.Category != "XXX/Other" {
		t.Errorf("expected category 'XXX/Other' for 1_3, got %q", t2.Category)
	}
}

// -----------------------------------------------------------------------------
// 4. TorrentDownloads Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_TorrentDownloads_ParseHTML(t *testing.T) {
	s := NewTorrentDownloadsScraper()
	data := loadFixture(t, "torrentdownloads.xml")

	results, err := s.ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error parsing torrentdownloads.xml: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 items from torrentdownloads.xml, got %d", len(results))
	}

	// Item 0: Standard TV show item with timezone offset pubDate & relative URL
	t0 := results[0]
	if t0.Title != "Kill Jackie S01 2160p WEBRip HDR x265-KONTRAST" {
		t.Errorf("expected title 'Kill Jackie S01 2160p WEBRip HDR x265-KONTRAST', got %q", t0.Title)
	}
	if t0.InfoHash != "23429a9ae77301514b02dcd1d6adb17bb87f80c5" {
		t.Errorf("expected lowercase infohash, got %q", t0.InfoHash)
	}
	if t0.Category != "TV" {
		t.Errorf("expected category 'TV', got %q", t0.Category)
	}
	if t0.Size != 23379695704 {
		t.Errorf("expected size 23379695704 bytes, got %d", t0.Size)
	}
	if t0.Seeds != 6 || t0.Leeches != 34 {
		t.Errorf("expected seeds=6, leeches=34, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
	}
	if t0.Source != "TorrentDownloads" {
		t.Errorf("expected source 'TorrentDownloads', got %q", t0.Source)
	}
	if t0.PageURL != "https://www.torrentdownloads.pro/torrent/1654321/kill-jackie-s01-2160p" {
		t.Errorf("expected full URL with base prefix, got %q", t0.PageURL)
	}
	if t0.PublishedDate.IsZero() || t0.PublishedDate.Location() != time.UTC {
		t.Errorf("expected UTC parsed date, got %v", t0.PublishedDate)
	}

	// Item 1: Sentinel swarm 16777215 mapped to 0 seeds/leeches
	t1 := results[1]
	if t1.Seeds != 0 || t1.Leeches != 0 {
		t.Errorf("expected sentinel 16777215 to map to 0 seeds and 0 leeches, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}
	if t1.Category != "Movies" {
		t.Errorf("expected category 'Movies', got %q", t1.Category)
	}
	if t1.Size != 1073741824 {
		t.Errorf("expected size 1073741824 bytes, got %d", t1.Size)
	}
}

// -----------------------------------------------------------------------------
// 5. The Pirate Bay Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_ThePirateBay_ParseHTML(t *testing.T) {
	s := NewThePirateBayScraper()
	data := loadFixture(t, "thepiratebay.json")

	results, err := s.ParseHTML(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error parsing thepiratebay.json: %v", err)
	}

	// 3 rows in JSON, but row 3 is 40-zero placeholder row (id 0) and must be discarded!
	if len(results) != 2 {
		t.Fatalf("expected 2 valid items (excluding 40-zero dummy row), got %d", len(results))
	}

	// Item 0: Movie (207 -> Movies/HD) with entity unescaping
	t0 := results[0]
	if t0.Title != "Fixture Movie 2024 1080p WEB-DL & Extras" {
		t.Errorf("expected unescaped title 'Fixture Movie 2024 1080p WEB-DL & Extras', got %q", t0.Title)
	}
	if t0.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("expected lowercase infohash, got %q", t0.InfoHash)
	}
	if t0.Category != "Movies/HD" {
		t.Errorf("expected category 'Movies/HD' for 207, got %q", t0.Category)
	}
	if t0.Size != 1621350154 {
		t.Errorf("expected size 1621350154, got %d", t0.Size)
	}
	if t0.Seeds != 25 || t0.Leeches != 4 {
		t.Errorf("expected seeds=25, leeches=4, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
	}
	if t0.Source != "The Pirate Bay" {
		t.Errorf("expected source 'The Pirate Bay', got %q", t0.Source)
	}
	if t0.PageURL != "https://thepiratebay.org/description.php?id=1001" {
		t.Errorf("expected description URL, got %q", t0.PageURL)
	}
	if t0.PublishedDate.Unix() != 1700000000 {
		t.Errorf("expected published date 1700000000, got %v", t0.PublishedDate)
	}

	// Item 1: TV Show (208 -> TV/HD)
	t1 := results[1]
	if t1.Category != "TV/HD" {
		t.Errorf("expected category 'TV/HD' for 208, got %q", t1.Category)
	}
	if t1.Seeds != 80 || t1.Leeches != 15 {
		t.Errorf("expected seeds=80, leeches=15, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}
	if t1.Size != 4500000000 {
		t.Errorf("expected size 4500000000, got %d", t1.Size)
	}
}

// -----------------------------------------------------------------------------
// 6. AudioBookBay Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_AudioBookBay_ParseHTML(t *testing.T) {
	s := NewAudioBookBayScraper()

	t.Run("search listing html fixture", func(t *testing.T) {
		data := loadFixture(t, "audiobookbay_search.html")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing audiobookbay_search.html: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 listing items, got %d", len(results))
		}

		t0 := results[0]
		if t0.Title != "Chasing Mila - Charlotte Engel" {
			t.Errorf("expected title 'Chasing Mila - Charlotte Engel', got %q", t0.Title)
		}
		if t0.PageURL != "https://audiobookbay.lu/audio-books/chasing-mila-charlotte-engel/" {
			t.Errorf("expected full URL with base prefix, got %q", t0.PageURL)
		}
		if t0.Category != "Audio/Audiobook" {
			t.Errorf("expected category 'Audio/Audiobook', got %q", t0.Category)
		}
		if t0.Size <= 0 {
			t.Errorf("expected positive parsed size, got %d", t0.Size)
		}
		if t0.Source != "AudioBookBay" {
			t.Errorf("expected source 'AudioBookBay', got %q", t0.Source)
		}

		t1 := results[1]
		if t1.Title != "The Quiet Detective - Arthur Conan" {
			t.Errorf("expected title 'The Quiet Detective - Arthur Conan', got %q", t1.Title)
		}
		if t1.Size <= 0 {
			t.Errorf("expected positive parsed size, got %d", t1.Size)
		}
	})

	t.Run("book detail html fixture", func(t *testing.T) {
		data := loadFixture(t, "audiobookbay_detail.html")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing audiobookbay_detail.html: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 item from detail page, got %d", len(results))
		}

		t0 := results[0]
		if t0.Title != "Chasing Mila - Charlotte Engel" {
			t.Errorf("expected title 'Chasing Mila - Charlotte Engel', got %q", t0.Title)
		}
		if t0.InfoHash != "23b1d853d891217222ef24852bf7ff5323e062e6" {
			t.Errorf("expected infohash '23b1d853d891217222ef24852bf7ff5323e062e6', got %q", t0.InfoHash)
		}
		if t0.Category != "Audio/Audiobook" {
			t.Errorf("expected category 'Audio/Audiobook', got %q", t0.Category)
		}
		if t0.Size <= 0 {
			t.Errorf("expected positive size, got %d", t0.Size)
		}
		if !strings.Contains(t0.MagnetURI, "23b1d853d891217222ef24852bf7ff5323e062e6") {
			t.Errorf("expected magnet to contain infohash, got %q", t0.MagnetURI)
		}
		if !strings.Contains(t0.MagnetURI, "tracker.openbittorrent.com") {
			t.Errorf("expected page announce URL in magnet, got %q", t0.MagnetURI)
		}
	})
}

// -----------------------------------------------------------------------------
// 7. EZTV Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_EZTV_ParseHTML(t *testing.T) {
	s := NewEZTVScraper()

	t.Run("api json envelope fixture", func(t *testing.T) {
		data := loadFixture(t, "eztv.json")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing eztv.json: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 torrents from eztv.json, got %d", len(results))
		}

		// Item 0: 720p with " EZTV" suffix stripped
		t0 := results[0]
		if t0.Title != "Fixture Show S02E01 720p WEB H264-FIX" {
			t.Errorf("expected stripped title 'Fixture Show S02E01 720p WEB H264-FIX', got %q", t0.Title)
		}
		if t0.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
			t.Errorf("expected lowercase infohash, got %q", t0.InfoHash)
		}
		if t0.Category != "TV/HD" {
			t.Errorf("expected category 'TV/HD', got %q", t0.Category)
		}
		if t0.Size != 1080141019 {
			t.Errorf("expected size 1080141019, got %d", t0.Size)
		}
		if t0.Seeds != 25 || t0.Leeches != 4 {
			t.Errorf("expected seeds=25, leeches=4, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
		}
		if t0.Source != "EZTV" {
			t.Errorf("expected source 'EZTV', got %q", t0.Source)
		}
		if t0.PageURL != "https://eztvx.to/ep/2001/fixture-show-s02e01-720p-web-h264-fix" {
			t.Errorf("expected pageURL 'https://eztvx.to/ep/2001/fixture-show-s02e01-720p-web-h264-fix', got %q", t0.PageURL)
		}
		if t0.PublishedDate.Unix() != 1700000000 {
			t.Errorf("expected published date 1700000000, got %v", t0.PublishedDate)
		}

		// Item 1: 1080p with "[eztv]" suffix stripped
		t1 := results[1]
		if t1.Title != "Fixture Show S02E01 1080p WEB H265-FIX" {
			t.Errorf("expected stripped title 'Fixture Show S02E01 1080p WEB H265-FIX', got %q", t1.Title)
		}
		if t1.InfoHash != "fedcba9876543210fedcba9876543210fedcba98" {
			t.Errorf("expected lowercase infohash, got %q", t1.InfoHash)
		}
		if t1.Category != "TV/HD" {
			t.Errorf("expected category 'TV/HD', got %q", t1.Category)
		}
		if t1.Size != 2147483648 {
			t.Errorf("expected size 2147483648, got %d", t1.Size)
		}
		if t1.Seeds != 42 || t1.Leeches != 7 {
			t.Errorf("expected seeds=42, leeches=7, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
		}
	})

	t.Run("search html table fixture", func(t *testing.T) {
		data := loadFixture(t, "eztv_search.html")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing eztv_search.html: %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 items from eztv_search.html table, got %d", len(results))
		}

		t0 := results[0]
		if t0.Title != "Fixture Show S02E01 720p WEB H264-FIX" {
			t.Errorf("expected stripped title, got %q", t0.Title)
		}
		if t0.Seeds != 25 {
			t.Errorf("expected seeds=25, got %d", t0.Seeds)
		}
		if t0.Category != "TV/HD" {
			t.Errorf("expected category 'TV/HD', got %q", t0.Category)
		}
	})
}

// -----------------------------------------------------------------------------
// 8. OxTorrent Fixture Tests
// -----------------------------------------------------------------------------

func TestFixtures_OxTorrent_ParseHTML(t *testing.T) {
	s := NewOxTorrentScraper()

	t.Run("search listing table fixture", func(t *testing.T) {
		data := loadFixture(t, "oxtorrent_search.html")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing oxtorrent_search.html: %v", err)
		}

		// Non-detail rows or cover strips must be skipped; only valid table rows extracted
		if len(results) != 2 {
			t.Fatalf("expected 2 table items, got %d", len(results))
		}

		t0 := results[0]
		if t0.Title != "The.Woman.in.Cabin.10.2025.MULTi.2160p.WEB.H265-FW" {
			t.Errorf("expected title 'The.Woman.in.Cabin.10.2025.MULTi.2160p.WEB.H265-FW', got %q", t0.Title)
		}
		if t0.PageURL != "https://ww1.oxtorrent.run/detail/3001-the-woman-in-cabin-10-2025-multi-2160p" {
			t.Errorf("expected full URL, got %q", t0.PageURL)
		}
		if t0.Category != "Movies" {
			t.Errorf("expected category 'Movies' for Films, got %q", t0.Category)
		}
		if t0.Size != 9040906158 { // 8.42 GB
			t.Errorf("expected size 9040906158 bytes (8.42 GB), got %d", t0.Size)
		}
		if t0.Seeds != 16 || t0.Leeches != 5 {
			t.Errorf("expected seeds=16, leeches=5, got seeds=%d, leeches=%d", t0.Seeds, t0.Leeches)
		}
		if t0.Source != "OxTorrent" {
			t.Errorf("expected source 'OxTorrent', got %q", t0.Source)
		}

		t1 := results[1]
		if t1.Title != "French.Drama.S01.FRENCH.720p.WEB" {
			t.Errorf("expected title 'French.Drama.S01.FRENCH.720p.WEB', got %q", t1.Title)
		}
		if t1.Category != "TV" {
			t.Errorf("expected category 'TV' for Séries, got %q", t1.Category)
		}
		if t1.Seeds != 30 || t1.Leeches != 8 {
			t.Errorf("expected seeds=30, leeches=8, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
		}
	})

	t.Run("detail page fixture with French date and magnet link", func(t *testing.T) {
		data := loadFixture(t, "oxtorrent_detail.html")
		results, err := s.ParseHTML(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error parsing oxtorrent_detail.html: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 item from detail page, got %d", len(results))
		}

		t0 := results[0]
		if !strings.HasPrefix(t0.Title, "The.Woman.in.Cabin.10.2025.MULTi.2160p.WEB.H265") {
			t.Errorf("expected title prefix 'The.Woman.in.Cabin.10.2025.MULTi.2160p.WEB.H265', got %q", t0.Title)
		}
		if t0.InfoHash != "f83ef8ecb9eceba1c55267264478ee4552a2a095" {
			t.Errorf("expected infohash 'f83ef8ecb9eceba1c55267264478ee4552a2a095', got %q", t0.InfoHash)
		}
		if t0.Size != 9040906158 {
			t.Errorf("expected size 9040906158 bytes (8.42 GB), got %d", t0.Size)
		}
		if t0.PublishedDate.IsZero() || t0.PublishedDate.Day() != 26 || t0.PublishedDate.Month() != 10 || t0.PublishedDate.Year() != 2025 {
			t.Errorf("expected French date 26/10/2025 parsed to UTC, got %v", t0.PublishedDate)
		}
		if !strings.Contains(t0.MagnetURI, "F83EF8ECB9ECEBA1C55267264478EE4552A2A095") &&
			!strings.Contains(t0.MagnetURI, "f83ef8ecb9eceba1c55267264478ee4552a2a095") {
			t.Errorf("expected magnet URI with infohash, got %q", t0.MagnetURI)
		}
	})
}

// -----------------------------------------------------------------------------
// 9. Negative / Malformed & Missing Fields Tests (Zero Panics)
// -----------------------------------------------------------------------------

func TestFixtures_MalformedAndMissingFields(t *testing.T) {
	scrapers := []struct {
		name    string
		scraper SourceScraper
	}{
		{"YTS", NewYTSScraper()},
		{"Nyaa.si", NewNyaaScraper()},
		{"Sukebei", NewSukebeiScraper()},
		{"TorrentDownloads", NewTorrentDownloadsScraper()},
		{"The Pirate Bay", NewThePirateBayScraper()},
		{"AudioBookBay", NewAudioBookBayScraper()},
		{"EZTV", NewEZTVScraper()},
		{"OxTorrent", NewOxTorrentScraper()},
	}

	corruptInputs := []struct {
		name    string
		content string
	}{
		{"empty string", ""},
		{"only whitespace", "    \r\n\t  "},
		{"corrupt json", `{"broken": [1, 2,`},
		{"corrupt xml", `<?xml version="1.0"?><rss><channel><item><title>unclosed`},
		{"corrupt html", `<html><body><div><span>unclosed tags<table><tr><td>`},
		{"non-numeric sizes and seeds", `<item><size>not-a-number</size><seeders>NaN</seeders></item>`},
		{"arbitrary binary garbage", string([]byte{0x00, 0xFF, 0xFE, 0x12, 0x34, 0x89, 0xAB})},
		{"unexpected json structure", `{"random_key": {"nested": [true, null, "unexpected"]}}`},
		{"empty json array", `[]`},
		{"empty json object", `{}`},
	}

	for _, entry := range scrapers {
		t.Run(entry.name, func(t *testing.T) {
			// 1. Nil reader check
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on nil reader: %v", entry.name, r)
				}
			}()
			resNil, errNil := entry.scraper.ParseHTML(nil)
			if errNil != nil && len(resNil) > 0 {
				t.Errorf("%s returned results on nil reader: %v", entry.name, resNil)
			}

			// 2. Corrupt inputs check
			for _, ci := range corruptInputs {
				t.Run(ci.name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked on %s: %v", entry.name, ci.name, r)
						}
					}()

					res, err := entry.scraper.ParseHTML(strings.NewReader(ci.content))
					// Either an error is returned or a graceful empty slice; must never crash or panic!
					_ = err
					_ = res
				})
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 10. Offline HTTP Querying via httptest.Server
// -----------------------------------------------------------------------------

func TestFixtures_OfflineHTTPSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. YTS Search Test
	t.Run("YTS Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "list_movies.json") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "yts.json"))
		}))
		defer ts.Close()

		scraper := NewYTSScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "inception")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 5 {
			t.Fatalf("expected 5 results from mock YTS server, got %d", len(results))
		}
	})

	// 2. Nyaa Search Test
	t.Run("Nyaa Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "nyaa.xml"))
		}))
		defer ts.Close()

		scraper := NewNyaaScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "show")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results from mock Nyaa server, got %d", len(results))
		}
	})

	// 3. Sukebei Search Test
	t.Run("Sukebei Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "sukebei.xml"))
		}))
		defer ts.Close()

		scraper := NewSukebeiScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "clip")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results from mock Sukebei server, got %d", len(results))
		}
	})

	// 4. TorrentDownloads Search Test
	t.Run("TorrentDownloads Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "torrentdownloads.xml"))
		}))
		defer ts.Close()

		scraper := NewTorrentDownloadsScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "jackie")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results from mock TorrentDownloads server, got %d", len(results))
		}
	})

	// 5. The Pirate Bay Search Test
	t.Run("The Pirate Bay Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "thepiratebay.json"))
		}))
		defer ts.Close()

		scraper := NewThePirateBayScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "movie")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results from mock TPB server, got %d", len(results))
		}
	})

	// 6. AudioBookBay Search Test (2-tier search + detail page)
	t.Run("AudioBookBay Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if strings.Contains(r.URL.Path, "audio-books") {
				w.Write(loadFixture(t, "audiobookbay_detail.html"))
				return
			}
			w.Write(loadFixture(t, "audiobookbay_search.html"))
		}))
		defer ts.Close()

		scraper := NewAudioBookBayScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "mila")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) == 0 {
			t.Fatalf("expected results from mock AudioBookBay 2-tier search, got 0")
		}
		if results[0].InfoHash == "" {
			t.Errorf("expected extracted infohash from detail fetch, got empty string")
		}
	})

	// 7. EZTV Search Test
	t.Run("EZTV Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "eztv.json"))
		}))
		defer ts.Close()

		scraper := NewEZTVScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		// Search with numeric IMDb ID to query API directly on mock server
		results, err := scraper.Search(ctx, "tt0944947")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results from mock EZTV server, got %d", len(results))
		}
	})

	// 8. OxTorrent Search Test (2-tier search + detail page)
	t.Run("OxTorrent Search", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if strings.Contains(r.URL.Path, "detail") {
				w.Write(loadFixture(t, "oxtorrent_detail.html"))
				return
			}
			w.Write(loadFixture(t, "oxtorrent_search.html"))
		}))
		defer ts.Close()

		scraper := NewOxTorrentScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
		results, err := scraper.Search(ctx, "woman")
		if err != nil {
			t.Fatalf("unexpected search error: %v", err)
		}
		if len(results) == 0 {
			t.Fatalf("expected results from mock OxTorrent 2-tier search, got 0")
		}
		if results[0].InfoHash != "f83ef8ecb9eceba1c55267264478ee4552a2a095" {
			t.Errorf("expected infohash from detail page, got %q", results[0].InfoHash)
		}
	})
}

// -----------------------------------------------------------------------------
// 11. Offline Registry SearchAll Integration Test
// -----------------------------------------------------------------------------

func TestFixtures_OfflineRegistrySearchAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "list_movies.json"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "yts.json"))
		case strings.Contains(r.URL.Path, "nyaa"):
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "nyaa.xml"))
		case strings.Contains(r.URL.Path, "sukebei"):
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "sukebei.xml"))
		case strings.Contains(r.URL.Path, "torrentdownloads"):
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write(loadFixture(t, "torrentdownloads.xml"))
		case strings.Contains(r.URL.Path, "q.php"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "thepiratebay.json"))
		case strings.Contains(r.URL.Path, "audio-books"):
			w.Header().Set("Content-Type", "text/html")
			w.Write(loadFixture(t, "audiobookbay_detail.html"))
		case strings.Contains(r.URL.Path, "detail"):
			w.Header().Set("Content-Type", "text/html")
			w.Write(loadFixture(t, "oxtorrent_detail.html"))
		case strings.Contains(r.URL.Path, "api/get-torrents"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(loadFixture(t, "eztv.json"))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Write(loadFixture(t, "oxtorrent_search.html"))
		}
	}))
	defer ts.Close()

	reg := NewRegistry()
	reg.Register(NewYTSScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client())))
	reg.Register(NewNyaaScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client())))
	reg.Register(NewThePirateBayScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client())))

	allResults, err := reg.SearchAll(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected SearchAll error: %v", err)
	}

	if len(allResults) == 0 {
		t.Fatalf("expected aggregated results from registered mock scrapers, got 0")
	}

	// Verify sources are present
	sourcesFound := make(map[string]bool)
	for _, tor := range allResults {
		sourcesFound[tor.Source] = true
	}

	if !sourcesFound["YTS"] || !sourcesFound["Nyaa.si"] || !sourcesFound["The Pirate Bay"] {
		t.Errorf("expected results from YTS, Nyaa.si, and The Pirate Bay; got sources: %v", sourcesFound)
	}
}
