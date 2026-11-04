package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// 1. YTS Tests
// -----------------------------------------------------------------------------

func TestYTS_ParseHTML_Valid(t *testing.T) {
	jsonContent := `{
		"status": "ok",
		"data": {
			"movie_count": 2,
			"limit": 50,
			"page_number": 1,
			"movies": [
				{
					"id": 100,
					"url": "https://yts.mx/movies/inception-2010",
					"title": "Inception",
					"title_long": "Inception (2010)",
					"torrents": [
						{
							"hash": "0123456789ABCDEF0123456789ABCDEF01234567",
							"quality": "720p",
							"size_bytes": 874575299,
							"seeds": 12,
							"peers": 3,
							"date_uploaded_unix": 1700000000
						},
						{
							"hash": "FEDCBA9876543210FEDCBA9876543210FEDCBA98",
							"quality": "2160p",
							"size_bytes": 5000000000,
							"seeds": 45,
							"peers": 10,
							"date_uploaded_unix": 1700000500
						}
					]
				}
			]
		}
	}`

	scraper := NewYTSScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(jsonContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.Title != "Inception (2010)" {
		t.Errorf("expected title 'Inception (2010)', got %q", t1.Title)
	}
	if t1.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("expected lowercase hash, got %q", t1.InfoHash)
	}
	if t1.Category != "Movies/HD" {
		t.Errorf("expected category 'Movies/HD', got %q", t1.Category)
	}
	if t1.Size != 874575299 {
		t.Errorf("expected size 874575299, got %d", t1.Size)
	}
	if t1.Seeds != 12 || t1.Leeches != 3 {
		t.Errorf("expected seeds=12, leeches=3, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}
	if t1.Source != "YTS" {
		t.Errorf("expected source 'YTS', got %q", t1.Source)
	}
	if !strings.HasPrefix(t1.MagnetURI, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567") {
		t.Errorf("expected magnet to contain infohash, got %q", t1.MagnetURI)
	}
	if t1.PublishedDate.IsZero() || t1.PublishedDate.Unix() != 1700000000 {
		t.Errorf("expected published date 1700000000, got %v", t1.PublishedDate)
	}

	t2 := torrents[1]
	if t2.Category != "Movies/UHD" {
		t.Errorf("expected category 'Movies/UHD' for 2160p, got %q", t2.Category)
	}
}

func TestYTS_ParseHTML_EmptyResults(t *testing.T) {
	jsonContent := `{"status":"ok","data":{"movie_count":0}}`
	scraper := NewYTSScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(jsonContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(torrents) != 0 {
		t.Errorf("expected 0 torrents, got %d", len(torrents))
	}
}

func TestYTS_Search_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "list_movies.json") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("query_term") != "matrix" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","data":{"movie_count":1,"movies":[{"title":"The Matrix","url":"http://test","torrents":[{"hash":"1111222233334444555566667777888899990000","quality":"1080p","size_bytes":2000000000,"seeds":50,"peers":10,"date_uploaded_unix":1600000000}]}]}}`)
	}))
	defer server.Close()

	scraper := NewYTSScraper(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	results, err := scraper.Search(context.Background(), "matrix")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "The Matrix" {
		t.Errorf("expected title 'The Matrix', got %q", results[0].Title)
	}
}

// -----------------------------------------------------------------------------
// 2. Nyaa.si Tests
// -----------------------------------------------------------------------------

func TestNyaa_ParseHTML_XML(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="utf-8"?>
	<rss xmlns:nyaa="https://nyaa.si/xmlns/nyaa" version="2.0">
		<channel>
			<title>Nyaa</title>
			<item>
				<title>[Subs] Attack on Titan &amp; Shingeki &#39;Final&#39; - 01 (1080p)</title>
				<link>https://nyaa.si/download/12345.torrent</link>
				<guid isPermaLink="true">https://nyaa.si/view/12345</guid>
				<pubDate>Fri, 09 Oct 2026 23:24:24 -0000</pubDate>
				<nyaa:seeders>120</nyaa:seeders>
				<nyaa:leechers>15</nyaa:leechers>
				<nyaa:infoHash>AEC9B1182F520E27AF38955DCE4CEC9118AC115F</nyaa:infoHash>
				<nyaa:categoryId>1_2</nyaa:categoryId>
				<nyaa:size>1.5 GiB</nyaa:size>
			</item>
			<item>
				<title>Anime OST Album FLAC</title>
				<guid isPermaLink="true">https://nyaa.si/view/12346</guid>
				<pubDate>Sat, 10 Oct 2026 12:00:00 +0000</pubDate>
				<nyaa:seeders>45</nyaa:seeders>
				<nyaa:leechers>2</nyaa:leechers>
				<nyaa:infoHash>0123456789abcdef0123456789abcdef01234567</nyaa:infoHash>
				<nyaa:categoryId>2_1</nyaa:categoryId>
				<nyaa:size>700.5 MiB</nyaa:size>
			</item>
		</channel>
	</rss>`

	scraper := NewNyaaScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(xmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}

	t1 := torrents[0]
	expectedTitle := "[Subs] Attack on Titan & Shingeki 'Final' - 01 (1080p)"
	if t1.Title != expectedTitle {
		t.Errorf("expected title %q, got %q", expectedTitle, t1.Title)
	}
	if t1.InfoHash != "aec9b1182f520e27af38955dce4cec9118ac115f" {
		t.Errorf("expected lowercased infohash, got %q", t1.InfoHash)
	}
	if t1.Category != "TV/Anime" {
		t.Errorf("expected TV/Anime, got %q", t1.Category)
	}
	if t1.Seeds != 120 || t1.Leeches != 15 {
		t.Errorf("expected seeds=120, leeches=15, got %d, %d", t1.Seeds, t1.Leeches)
	}
	if !strings.Contains(t1.MagnetURI, "nyaa.tracker.wf") {
		t.Errorf("expected Nyaa tracker in magnet, got %q", t1.MagnetURI)
	}
	if t1.Size != 1610612736 {
		t.Errorf("expected size 1610612736 bytes (1.5 GiB), got %d", t1.Size)
	}

	t2 := torrents[1]
	if t2.Category != "Audio" {
		t.Errorf("expected Category Audio for 2_1, got %q", t2.Category)
	}
}

func TestNyaa_ParseHTML_HTMLTable(t *testing.T) {
	htmlContent := `<table class="table table-bordered table-hover table-striped torrent-list">
		<tbody>
			<tr class="default">
				<td><a href="/?c=1_2" title="Anime - English-translated"></a></td>
				<td><a href="/view/12345">Demon Slayer S04 1080p</a></td>
				<td><a href="magnet:?xt=urn:btih:aec9b1182f520e27af38955dce4cec9118ac115f&dn=Demon"></a></td>
				<td>2.1 GiB</td>
				<td data-timestamp="1696893864">2023-10-09</td>
				<td>55</td>
				<td>4</td>
			</tr>
		</tbody>
	</table>`

	scraper := NewNyaaScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent from HTML table, got %d", len(torrents))
	}
	if torrents[0].Title != "Demon Slayer S04 1080p" {
		t.Errorf("expected Demon Slayer title, got %q", torrents[0].Title)
	}
	if torrents[0].InfoHash != "aec9b1182f520e27af38955dce4cec9118ac115f" {
		t.Errorf("expected hash aec9..., got %q", torrents[0].InfoHash)
	}
}

// -----------------------------------------------------------------------------
// 3. Sukebei Tests
// -----------------------------------------------------------------------------

func TestSukebei_AdultCategoryMapping(t *testing.T) {
	xmlContent := `<?xml version="1.0" encoding="utf-8"?>
	<rss xmlns:nyaa="https://sukebei.nyaa.si/xmlns/nyaa" version="2.0">
		<channel>
			<item>
				<title>Adult Video 1</title>
				<guid>https://sukebei.nyaa.si/view/1</guid>
				<nyaa:infoHash>0123456789abcdef0123456789abcdef01234567</nyaa:infoHash>
				<nyaa:categoryId>1_1</nyaa:categoryId>
				<nyaa:size>1.1 GiB</nyaa:size>
			</item>
			<item>
				<title>Adult Doujinshi Gallery</title>
				<guid>https://sukebei.nyaa.si/view/2</guid>
				<nyaa:infoHash>1111222233334444555566667777888899990000</nyaa:infoHash>
				<nyaa:categoryId>1_2</nyaa:categoryId>
				<nyaa:size>500 MiB</nyaa:size>
			</item>
			<item>
				<title>Adult Game 3</title>
				<guid>https://sukebei.nyaa.si/view/3</guid>
				<nyaa:infoHash>2222333344445555666677778888999900001111</nyaa:infoHash>
				<nyaa:categoryId>1_3</nyaa:categoryId>
				<nyaa:size>2.0 GiB</nyaa:size>
			</item>
			<item>
				<title>Adult Unknown Category</title>
				<guid>https://sukebei.nyaa.si/view/4</guid>
				<nyaa:infoHash>3333444455556666777788889999000011112222</nyaa:infoHash>
				<nyaa:categoryId>9_9</nyaa:categoryId>
				<nyaa:size>100 MiB</nyaa:size>
			</item>
		</channel>
	</rss>`

	scraper := NewSukebeiScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(xmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 4 {
		t.Fatalf("expected 4 torrents, got %d", len(torrents))
	}

	// Verify categories
	if torrents[0].Category != "XXX" {
		t.Errorf("expected 1_1 -> XXX, got %q", torrents[0].Category)
	}
	if torrents[1].Category != "XXX/ImageSet" {
		t.Errorf("expected 1_2 -> XXX/ImageSet, got %q", torrents[1].Category)
	}
	if torrents[2].Category != "XXX/Other" {
		t.Errorf("expected 1_3 -> XXX/Other, got %q", torrents[2].Category)
	}
	if torrents[3].Category != "XXX" {
		t.Errorf("expected default fallback -> XXX (never non-adult), got %q", torrents[3].Category)
	}

	// Verify port 8888 tracker
	if !strings.Contains(torrents[0].MagnetURI, "sukebei.tracker.wf") {
		t.Errorf("expected port 8888 tracker in Sukebei magnet, got %q", torrents[0].MagnetURI)
	}
}

// -----------------------------------------------------------------------------
// 4. TorrentDownloads Tests
// -----------------------------------------------------------------------------

func TestTorrentDownloads_ParseHTML_SentinelAndRelativeLink(t *testing.T) {
	xmlContent := `<rss version='2.0'>
		<channel>
			<item>
				<title>Kill Jackie S01 2160p WEBRip HDR x265-KONTRAST</title>
				<pubDate>Sat, 10 Oct 2026 01:40:07 +0200</pubDate>
				<category><![CDATA[TV Shows]]></category>
				<link>/torrent/1707208289/Kill-Jackie-S01-2160p-WEBRip-HDR-x265-KONTRAST</link>
				<size>23379695704</size>
				<seeders>16777215</seeders>
				<leechers>16777215</leechers>
				<info_hash>23429A9AE77301514B02DCD1D6ADB17BB87F80C5</info_hash>
			</item>
			<item>
				<title>Some Ubuntu Linux Distro</title>
				<pubDate>Sat, 10 Oct 2026 01:40:07 +0200</pubDate>
				<category><![CDATA[Software]]></category>
				<link>/torrent/1707208290/ubuntu</link>
				<size>4500000000</size>
				<seeders>150</seeders>
				<leechers>30</leechers>
				<info_hash>ABCDEF0123456789ABCDEF0123456789ABCDEF01</info_hash>
			</item>
		</channel>
	</rss>`

	scraper := NewTorrentDownloadsScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(xmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.Seeds != 0 || t1.Leeches != 0 {
		t.Errorf("expected sentinel 16777215 to be mapped to 0, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}
	if t1.Size != 23379695704 {
		t.Errorf("expected size 23379695704, got %d", t1.Size)
	}
	if t1.InfoHash != "23429a9ae77301514b02dcd1d6adb17bb87f80c5" {
		t.Errorf("expected lowercase infohash, got %q", t1.InfoHash)
	}
	if t1.Category != "TV" {
		t.Errorf("expected Category TV, got %q", t1.Category)
	}
	if !strings.HasPrefix(t1.PageURL, DefaultTorrentDownloadsBaseURL) {
		t.Errorf("expected PageURL prepended with base URL, got %q", t1.PageURL)
	}

	t2 := torrents[1]
	if t2.Seeds != 150 || t2.Leeches != 30 {
		t.Errorf("expected normal seeds=150, leeches=30, got seeds=%d, leeches=%d", t2.Seeds, t2.Leeches)
	}
	if t2.Category != "PC" {
		t.Errorf("expected Category PC for Software, got %q", t2.Category)
	}
}

// -----------------------------------------------------------------------------
// 5. The Pirate Bay Tests
// -----------------------------------------------------------------------------

func TestThePirateBay_ParseHTML_FilterPlaceholderAndStrings(t *testing.T) {
	jsonContent := `[
		{
			"id": "1001",
			"name": "Fixture Movie 2024 1080p WEB-DL",
			"info_hash": "0123456789ABCDEF0123456789ABCDEF01234567",
			"seeders": "25",
			"leechers": "4",
			"size": "1621350154",
			"added": "1700000000",
			"category": "207"
		},
		{
			"id": "0",
			"name": "No results returned",
			"info_hash": "0000000000000000000000000000000000000000",
			"seeders": "0",
			"leechers": "0",
			"size": "0",
			"added": "0",
			"category": "0"
		}
	]`

	scraper := NewThePirateBayScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(jsonContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The placeholder item should be filtered out!
	if len(torrents) != 1 {
		t.Fatalf("expected exactly 1 torrent (placeholder filtered out), got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.Title != "Fixture Movie 2024 1080p WEB-DL" {
		t.Errorf("expected title, got %q", t1.Title)
	}
	if t1.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("expected lowercase hash, got %q", t1.InfoHash)
	}
	if t1.Size != 1621350154 {
		t.Errorf("expected size 1621350154, got %d", t1.Size)
	}
	if t1.Seeds != 25 || t1.Leeches != 4 {
		t.Errorf("expected seeds=25, leeches=4, got seeds=%d, leeches=%d", t1.Seeds, t1.Leeches)
	}
	if t1.Category != "Movies/HD" {
		t.Errorf("expected Category Movies/HD for 207, got %q", t1.Category)
	}
	if !strings.HasPrefix(t1.PageURL, "https://thepiratebay.org/description.php?id=1001") {
		t.Errorf("expected canonical description PageURL, got %q", t1.PageURL)
	}
}

// -----------------------------------------------------------------------------
// 6. AudioBookBay Tests
// -----------------------------------------------------------------------------

func TestAudioBookBay_ParseHTML_DetailPage(t *testing.T) {
	htmlContent := `
	<html>
	<head><title>Test Book</title></head>
	<body>
		<div class="postTitle"><h1>Chasing Mila - Charlotte Engel</h1></div>
		<div class="postContent">
			<p>Book description here...</p>
			<table>
				<tr><td>File Size:</td><td><span>686.36</span> MBs</td></tr>
				<tr><td>Info Hash:</td><td>23B1D853D891217222EF24852BF7FF5323E062E6</td></tr>
				<tr><td>Tracker:</td><td>udp://tracker.openbittorrent.com:80/announce</td></tr>
				<tr><td>Announce URL:</td><td>udp://tracker.opentrackr.org:1337/announce</td></tr>
			</table>
		</div>
	</body>
	</html>`

	scraper := NewAudioBookBayScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.Title != "Chasing Mila - Charlotte Engel" {
		t.Errorf("expected title 'Chasing Mila - Charlotte Engel', got %q", t1.Title)
	}
	if t1.InfoHash != "23b1d853d891217222ef24852bf7ff5323e062e6" {
		t.Errorf("expected lowercase hash, got %q", t1.InfoHash)
	}
	if t1.Category != "Audio/Audiobook" {
		t.Errorf("expected Category Audio/Audiobook, got %q", t1.Category)
	}
	if t1.Seeds != 0 || t1.Leeches != 0 {
		t.Errorf("expected seeds=0, leeches=0, got %d, %d", t1.Seeds, t1.Leeches)
	}
	// 686.36 MBs -> 686.36 * 1024 * 1024 = 719700623
	if t1.Size != 719700623 {
		t.Errorf("expected size 719700623, got %d", t1.Size)
	}
	if !strings.Contains(t1.MagnetURI, "tracker.opentrackr.org") {
		t.Errorf("expected extracted tracker in magnet, got %q", t1.MagnetURI)
	}
}

// -----------------------------------------------------------------------------
// 7. EZTV Tests
// -----------------------------------------------------------------------------

func TestEZTV_ParseHTML_JSONEnvelope(t *testing.T) {
	jsonContent := `{
		"imdb_id": "1000001",
		"torrents_count": 2,
		"limit": 100,
		"page": 1,
		"torrents": [
			{
				"id": 2001,
				"hash": "0123456789abcdef0123456789abcdef01234567",
				"title": "Fixture Show S02E01 720p WEB H264-FIX EZTV",
				"date_released_unix": 1700000000,
				"size_bytes": "1080141019",
				"seeds": 25,
				"peers": 4,
				"magnet_url": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Fixture"
			},
			{
				"id": 2002,
				"hash": "1111222233334444555566667777888899990000",
				"title": "Fixture Show S02E02 480p HDTV [eztv]",
				"date_released_unix": 1700000500,
				"size_bytes": 450000000,
				"seeds": "10",
				"peers": "2"
			}
		]
	}`

	scraper := NewEZTVScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(jsonContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.Title != "Fixture Show S02E01 720p WEB H264-FIX" {
		t.Errorf("expected stripped ' EZTV' title, got %q", t1.Title)
	}
	if t1.Category != "TV/HD" {
		t.Errorf("expected TV/HD category for 720p, got %q", t1.Category)
	}
	if t1.Size != 1080141019 {
		t.Errorf("expected size 1080141019, got %d", t1.Size)
	}
	if t1.Seeds != 25 || t1.Leeches != 4 {
		t.Errorf("expected seeds=25, leeches=4, got %d, %d", t1.Seeds, t1.Leeches)
	}

	t2 := torrents[1]
	if t2.Title != "Fixture Show S02E02 480p HDTV" {
		t.Errorf("expected stripped '[eztv]' title, got %q", t2.Title)
	}
	if t2.Category != "TV/SD" {
		t.Errorf("expected TV/SD category for 480p, got %q", t2.Category)
	}
	if t2.Seeds != 10 || t2.Leeches != 2 {
		t.Errorf("expected seeds=10, leeches=2 from string values, got %d, %d", t2.Seeds, t2.Leeches)
	}
}

// -----------------------------------------------------------------------------
// 8. OxTorrent Tests
// -----------------------------------------------------------------------------

func TestOxTorrent_ParseHTML_DetailPage(t *testing.T) {
	htmlContent := `
	<html>
	<head><title>The Woman in Cabin 10 - OxTorrent</title></head>
	<body>
		<div class="header-torrent">
			<h1>The.Woman.in.Cabin.10.2025.MULTi.2160p.WEB.H265-FW</h1>
		</div>
		<span class="icon_date"><i class="fa fa-calendar"></i> 26/10/2025</span>
		<table>
			<tr><td>Poids du fichier:</td><td><strong>8.42 GB</strong></td></tr>
			<tr><td>Catégorie:</td><td>Films</td></tr>
		</table>
		<div class="btn-download">
			<a href="magnet:?xt=urn:btih:F83EF8ECB9ECEBA1C55267264478EE4552A2A095&dn=The+Woman&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337">Télécharger</a>
		</div>
	</body>
	</html>`

	scraper := NewOxTorrentScraper()
	torrents, err := scraper.ParseHTML(strings.NewReader(htmlContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}

	t1 := torrents[0]
	if t1.InfoHash != "f83ef8ecb9eceba1c55267264478ee4552a2a095" {
		t.Errorf("expected lowercase hash, got %q", t1.InfoHash)
	}
	if t1.PublishedDate.IsZero() || t1.PublishedDate.Year() != 2025 || t1.PublishedDate.Month() != 10 || t1.PublishedDate.Day() != 26 {
		t.Errorf("expected published date 2025-10-26, got %v", t1.PublishedDate)
	}
	// 8.42 GB = 9040906158 bytes
	if t1.Size != 9040906158 {
		t.Errorf("expected size 9040906158, got %d", t1.Size)
	}
	if t1.Source != "OxTorrent" {
		t.Errorf("expected source 'OxTorrent', got %q", t1.Source)
	}
}

// -----------------------------------------------------------------------------
// 9. Registry Tests
// -----------------------------------------------------------------------------

func TestRegistry_DefaultAndOperations(t *testing.T) {
	reg := DefaultRegistry()
	all := reg.All()

	if len(all) != 8 {
		t.Fatalf("expected 8 scrapers in DefaultRegistry, got %d", len(all))
	}

	expectedNames := []string{
		"YTS",
		"Nyaa.si",
		"Sukebei",
		"TorrentDownloads",
		"The Pirate Bay",
		"AudioBookBay",
		"EZTV",
		"OxTorrent",
	}

	for _, name := range expectedNames {
		s, found := reg.Get(name)
		if !found || s == nil {
			t.Errorf("expected scraper %q to be found in DefaultRegistry", name)
		} else if !strings.EqualFold(s.Name(), name) {
			t.Errorf("expected scraper name %q, got %q", name, s.Name())
		}
	}

	// Case insensitive lookup
	s, found := reg.Get("yts")
	if !found || s.Name() != "YTS" {
		t.Errorf("expected case-insensitive lookup for 'yts' to succeed")
	}

	s2, found2 := reg.Get("the pirate bay")
	if !found2 || s2.Name() != "The Pirate Bay" {
		t.Errorf("expected case-insensitive lookup for 'the pirate bay' to succeed")
	}
}

// MockScraper for Registry testing
type mockTestScraper struct {
	name     string
	results  []Torrent
	err      error
	delay    time.Duration
}

func (m *mockTestScraper) Name() string { return m.name }
func (m *mockTestScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.results, m.err
}
func (m *mockTestScraper) ParseHTML(r io.Reader) ([]Torrent, error) { return m.results, m.err }

func TestRegistry_SearchAll_AggregationAndErrorTolerance(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&mockTestScraper{
		name: "GoodSource1",
		results: []Torrent{
			{Title: "Result 1", InfoHash: "1111111111111111111111111111111111111111", Source: "GoodSource1"},
		},
	})
	reg.Register(&mockTestScraper{
		name: "FailingSource",
		err:  fmt.Errorf("simulated network timeout"),
	})
	reg.Register(&mockTestScraper{
		name: "GoodSource2",
		results: []Torrent{
			{Title: "Result 2", InfoHash: "2222222222222222222222222222222222222222", Source: "GoodSource2"},
		},
	})

	results, err := reg.SearchAll(context.Background(), "test")
	if err != nil {
		t.Fatalf("SearchAll failed despite having good sources: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 aggregated results from good sources, got %d", len(results))
	}
}

func TestRegistry_SearchAll_ContextCancellation(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTestScraper{
		name:  "SlowSource",
		delay: 500 * time.Millisecond,
		results: []Torrent{
			{Title: "Slow Result", InfoHash: "3333333333333333333333333333333333333333"},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := reg.SearchAll(ctx, "test")
	if err == nil {
		t.Fatalf("expected context timeout error, got nil")
	}
}
