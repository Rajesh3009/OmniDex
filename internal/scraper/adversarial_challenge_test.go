package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// 1. YTS Adversarial Tests
// -----------------------------------------------------------------------------

func TestAdversarial_YTS_EdgeCases(t *testing.T) {
	s := NewYTSScraper()

	t.Run("nil and empty input", func(t *testing.T) {
		res, err := s.ParseHTML(nil)
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty slice, got res=%v, err=%v", res, err)
		}

		res, err = s.ParseHTML(strings.NewReader("   \n\t  "))
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty slice on whitespace, got res=%v, err=%v", res, err)
		}
	})

	t.Run("movie_count 0 with missing movies array", func(t *testing.T) {
		jsonStr := `{"status":"ok","status_message":"Query was successful","data":{"movie_count":0,"limit":50,"page_number":1}}`
		res, err := s.ParseHTML(strings.NewReader(jsonStr))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 0 {
			t.Fatalf("expected 0 results, got %d", len(res))
		}
	})

	t.Run("uppercase infohash and multiple quality variants", func(t *testing.T) {
		jsonStr := `{
			"status": "ok",
			"data": {
				"movie_count": 1,
				"movies": [
					{
						"id": 42,
						"url": "https://yts.mx/movies/interstellar-2014",
						"title": "Interstellar",
						"title_long": "Interstellar (2014)",
						"torrents": [
							{"hash": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "quality": "480p", "size_bytes": 500000000, "seeds": 10, "peers": 2},
							{"hash": "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", "quality": "720p", "size_bytes": 1000000000, "seeds": 20, "peers": 5},
							{"hash": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", "quality": "1080p", "size_bytes": 2000000000, "seeds": 30, "peers": 8},
							{"hash": "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD", "quality": "2160p", "size_bytes": 15000000000, "seeds": 40, "peers": 12},
							{"hash": "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE", "quality": "3D", "size_bytes": 2500000000, "seeds": 5, "peers": 1},
							{"hash": "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", "quality": "CustomBitrate", "size_bytes": 3000000000, "seeds": 1, "peers": 0}
						]
					}
				]
			}
		}`

		res, err := s.ParseHTML(strings.NewReader(jsonStr))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 6 {
			t.Fatalf("expected 6 quality variants, got %d", len(res))
		}

		expectedCats := []string{
			"Movies/SD",
			"Movies/HD",
			"Movies/HD",
			"Movies/UHD",
			"Movies/3D",
			"Movies",
		}

		for i, tor := range res {
			// Verify lowercase hash
			if tor.InfoHash != strings.ToLower(tor.InfoHash) {
				t.Errorf("torrent %d: expected lowercase infohash, got %q", i, tor.InfoHash)
			}
			if len(tor.InfoHash) != 40 {
				t.Errorf("torrent %d: expected 40-char infohash, got len %d", i, len(tor.InfoHash))
			}
			if tor.Category != expectedCats[i] {
				t.Errorf("torrent %d: expected category %q, got %q", i, expectedCats[i], tor.Category)
			}
			if tor.Source != "YTS" {
				t.Errorf("torrent %d: expected Source 'YTS', got %q", i, tor.Source)
			}
			if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
				t.Errorf("torrent %d: invalid magnet uri %q", i, tor.MagnetURI)
			}
		}
	})

	t.Run("malformed json handling", func(t *testing.T) {
		res, err := s.ParseHTML(strings.NewReader(`{"status":"ok", invalid_json`))
		if err == nil {
			t.Fatalf("expected json error on malformed input, got res=%v", res)
		}
	})
}

// -----------------------------------------------------------------------------
// 2. Nyaa Adversarial Tests
// -----------------------------------------------------------------------------

func TestAdversarial_Nyaa_EdgeCases(t *testing.T) {
	s := NewNyaaScraper()

	t.Run("xml entities and mixed case infohash and binary units", func(t *testing.T) {
		xmlData := `<?xml version="1.0" encoding="utf-8"?>
		<rss xmlns:nyaa="https://nyaa.si/xmlns/nyaa" version="2.0">
			<channel>
				<title>Nyaa Feed</title>
				<item>
					<title>&lt;Bleach&gt; &amp; &quot;Thousand-Year Blood War&quot; &#39;Part 2&#39;</title>
					<guid isPermaLink="true">https://nyaa.si/view/99901</guid>
					<nyaa:seeders>350</nyaa:seeders>
					<nyaa:leechers>25</nyaa:leechers>
					<nyaa:infoHash>a1B2c3D4e5F6a1B2c3D4e5F6a1B2c3D4e5F6a1B2</nyaa:infoHash>
					<nyaa:categoryId>1_2</nyaa:categoryId>
					<nyaa:size>512 KiB</nyaa:size>
					<pubDate>Mon, 02 Nov 2026 15:04:05 +0000</pubDate>
				</item>
				<item>
					<title>Anime MiB Pack</title>
					<guid>https://nyaa.si/view/99902</guid>
					<nyaa:seeders>10</nyaa:seeders>
					<nyaa:leechers>1</nyaa:leechers>
					<nyaa:infoHash>b1B2c3D4e5F6a1B2c3D4e5F6a1B2c3D4e5F6a1B3</nyaa:infoHash>
					<nyaa:categoryId>2_1</nyaa:categoryId>
					<nyaa:size>700.5 MiB</nyaa:size>
				</item>
				<item>
					<title>Anime GiB Pack</title>
					<guid>https://nyaa.si/view/99903</guid>
					<nyaa:seeders>5</nyaa:seeders>
					<nyaa:leechers>0</nyaa:leechers>
					<nyaa:infoHash>c1B2c3D4e5F6a1B2c3D4e5F6a1B2c3D4e5F6a1B4</nyaa:infoHash>
					<nyaa:categoryId>3_1</nyaa:categoryId>
					<nyaa:size>1.5 GiB</nyaa:size>
				</item>
				<item>
					<title>Anime TiB Archive</title>
					<guid>https://nyaa.si/view/99904</guid>
					<nyaa:seeders>2</nyaa:seeders>
					<nyaa:leechers>0</nyaa:leechers>
					<nyaa:infoHash>d1B2c3D4e5F6a1B2c3D4e5F6a1B2c3D4e5F6a1B5</nyaa:infoHash>
					<nyaa:categoryId>6_1</nyaa:categoryId>
					<nyaa:size>2.0 TiB</nyaa:size>
				</item>
			</channel>
		</rss>`

		res, err := s.ParseHTML(strings.NewReader(xmlData))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res) != 4 {
			t.Fatalf("expected 4 results, got %d", len(res))
		}

		// Item 0: XML entities unescaped
		expectedTitle := `<Bleach> & "Thousand-Year Blood War" 'Part 2'`
		if res[0].Title != expectedTitle {
			t.Errorf("expected title %q, got %q", expectedTitle, res[0].Title)
		}
		if res[0].InfoHash != "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
			t.Errorf("expected lowercase infohash, got %q", res[0].InfoHash)
		}
		// 512 KiB = 512 * 1024 = 524288
		if res[0].Size != 524288 {
			t.Errorf("expected 524288 bytes for 512 KiB, got %d", res[0].Size)
		}
		if res[0].Category != "TV/Anime" {
			t.Errorf("expected TV/Anime, got %q", res[0].Category)
		}

		// Item 1: 700.5 MiB = 734527488
		if res[1].Size != 734527488 {
			t.Errorf("expected 734527488 bytes for 700.5 MiB, got %d", res[1].Size)
		}
		if res[1].Category != "Audio" {
			t.Errorf("expected Audio for 2_1, got %q", res[1].Category)
		}

		// Item 2: 1.5 GiB = 1610612736
		if res[2].Size != 1610612736 {
			t.Errorf("expected 1610612736 bytes for 1.5 GiB, got %d", res[2].Size)
		}
		if res[2].Category != "Books" {
			t.Errorf("expected Books for 3_1, got %q", res[2].Category)
		}

		// Item 3: 2.0 TiB = 2199023255552
		if res[3].Size != 2199023255552 {
			t.Errorf("expected 2199023255552 bytes for 2.0 TiB, got %d", res[3].Size)
		}
		if res[3].Category != "PC/Games" {
			t.Errorf("expected PC/Games for 6_1, got %q", res[3].Category)
		}
	})
}

// -----------------------------------------------------------------------------
// 3. Sukebei Adversarial Tests: Adult Taxonomy Preservation
// -----------------------------------------------------------------------------

func TestAdversarial_Sukebei_AdultTaxonomyStrictPreservation(t *testing.T) {
	s := NewSukebeiScraper()

	categoriesToTest := []struct {
		code     string
		expected string
	}{
		{"1_0", "XXX"},
		{"1_1", "XXX"},
		{"1_2", "XXX/ImageSet"},
		{"1_3", "XXX/Other"},
		{"1_4", "XXX/ImageSet"},
		{"1_5", "XXX/ImageSet"},
		{"2_0", "XXX"},
		{"2_1", "XXX/ImageSet"},
		{"2_2", "XXX"},
		// Unknown or adversarial inputs must NEVER fall back to non-adult categories!
		{"", "XXX"},
		{"unknown", "XXX"},
		{"9_9", "XXX"},
		{"3_1", "XXX"}, // on Nyaa 3_1 is Books, on Sukebei MUST be XXX!
		{"4_1", "XXX"}, // on Nyaa 4_1 is TV, on Sukebei MUST be XXX!
		{"6_1", "XXX"}, // on Nyaa 6_1 is Games, on Sukebei MUST be XXX!
	}

	for _, tc := range categoriesToTest {
		xmlData := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
		<rss xmlns:nyaa="https://sukebei.nyaa.si/xmlns/nyaa" version="2.0">
			<channel>
				<item>
					<title>Test Item For %s</title>
					<guid>https://sukebei.nyaa.si/view/100</guid>
					<nyaa:infoHash>1111111111111111111111111111111111111111</nyaa:infoHash>
					<nyaa:categoryId>%s</nyaa:categoryId>
					<nyaa:size>1 GiB</nyaa:size>
				</item>
			</channel>
		</rss>`, tc.code, tc.code)

		torrents, err := s.ParseHTML(strings.NewReader(xmlData))
		if err != nil {
			t.Fatalf("unexpected error parsing category %q: %v", tc.code, err)
		}
		if len(torrents) != 1 {
			t.Fatalf("expected 1 torrent for category %q, got %d", tc.code, len(torrents))
		}
		if torrents[0].Category != tc.expected {
			t.Errorf("category %q: expected adult category %q, got %q (LEAK TO NON-ADULT)", tc.code, tc.expected, torrents[0].Category)
		}
		// Verify port 8888 tracker
		if !strings.Contains(torrents[0].MagnetURI, "sukebei.tracker.wf%3A8888") && !strings.Contains(torrents[0].MagnetURI, "sukebei.tracker.wf:8888") {
			t.Errorf("expected Sukebei port 8888 tracker, got magnet: %q", torrents[0].MagnetURI)
		}
	}
}

// -----------------------------------------------------------------------------
// 4. TorrentDownloads Adversarial Tests: Sentinel 16777215 & Timezones
// -----------------------------------------------------------------------------

func TestAdversarial_TorrentDownloads_SentinelAndTimezones(t *testing.T) {
	s := NewTorrentDownloadsScraper()

	xmlData := `<?xml version="1.0" encoding="utf-8"?>
	<rss version="2.0">
		<channel>
			<item>
				<title>Linux Distro With Swarm Sentinel</title>
				<pubDate>Thu, 02 Nov 2026 14:30:00 +0200</pubDate>
				<category><![CDATA[Software]]></category>
				<link>/torrent/54321/linux-iso</link>
				<size>1621350154</size>
				<seeders>16777215</seeders>
				<leechers>16777215</leechers>
				<info_hash>4A36B093DA3664CD015A4C2EBEBCE1733BE430B9</info_hash>
			</item>
			<item>
				<title>Ubuntu Distro With Formatted Sentinel</title>
				<pubDate>Wed, 1 Nov 2026 10:15:30 -0700</pubDate>
				<category>Software</category>
				<link>/torrent/54322/ubuntu-iso</link>
				<size>2500000000</size>
				<seeders> 16,777,215 </seeders>
				<leechers>+16777215</leechers>
				<info_hash>1A36B093DA3664CD015A4C2EBEBCE1733BE430B1</info_hash>
			</item>
			<item>
				<title>Movie With Known Swarm</title>
				<pubDate>Mon, 02 Nov 2026 12:00:00 UTC</pubDate>
				<category>Movies</category>
				<link>/torrent/54323/movie</link>
				<size>8500000000</size>
				<seeders>450</seeders>
				<leechers>80</leechers>
				<info_hash>2A36B093DA3664CD015A4C2EBEBCE1733BE430B2</info_hash>
			</item>
		</channel>
	</rss>`

	res, err := s.ParseHTML(strings.NewReader(xmlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 items, got %d", len(res))
	}

	// Item 0: Sentinel 16777215 mapped to 0
	if res[0].Seeds != 0 || res[0].Leeches != 0 {
		t.Errorf("expected sentinel seeds=0, leeches=0, got seeds=%d, leeches=%d", res[0].Seeds, res[0].Leeches)
	}
	if res[0].PublishedDate.IsZero() {
		t.Errorf("expected pubDate to parse +0200, got zero time")
	}
	// Thu, 02 Nov 2026 14:30:00 +0200 in UTC is 12:30:00
	if res[0].PublishedDate.Hour() != 12 || res[0].PublishedDate.Minute() != 30 {
		t.Errorf("expected UTC hour=12 min=30, got %v", res[0].PublishedDate)
	}
	if !strings.HasPrefix(res[0].PageURL, DefaultTorrentDownloadsBaseURL) {
		t.Errorf("expected relative link to prepend baseURL, got %q", res[0].PageURL)
	}

	// Item 1: Formatted sentinel with commas and plus
	if res[1].Seeds != 0 || res[1].Leeches != 0 {
		t.Errorf("expected formatted sentinel seeds=0, leeches=0, got seeds=%d, leeches=%d", res[1].Seeds, res[1].Leeches)
	}
	// Wed, 1 Nov 2026 10:15:30 -0700 in UTC is 17:15:30
	if res[1].PublishedDate.Hour() != 17 || res[1].PublishedDate.Minute() != 15 {
		t.Errorf("expected UTC hour=17 min=15, got %v", res[1].PublishedDate)
	}

	// Item 2: Normal swarm
	if res[2].Seeds != 450 || res[2].Leeches != 80 {
		t.Errorf("expected normal seeds=450, leeches=80, got seeds=%d, leeches=%d", res[2].Seeds, res[2].Leeches)
	}
}

// -----------------------------------------------------------------------------
// 5. The Pirate Bay Adversarial Tests: Placeholder Row & String Conversion
// -----------------------------------------------------------------------------

func TestAdversarial_ThePirateBay_FilterPlaceholderAndStrings(t *testing.T) {
	s := NewThePirateBayScraper()

	// apibay returns a 40-zero placeholder row on empty results or alongside records
	jsonInput := `[
		{"id":"0","name":"No results returned","info_hash":"0000000000000000000000000000000000000000","leechers":"0","seeders":"0","num_files":"0","size":"0","username":"","added":"0","status":"member","category":"0"},
		{"id":"12345","name":"Real Release 2026 &amp; Extras","info_hash":"E1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2","leechers":"14","seeders":"88","num_files":"2","size":"2147483648","username":"VIP","added":"1730000000","status":"vip","category":"208"},
		{"id":"99999","name":"Another Zero Placeholder","info_hash":"0000000000000000000000000000000000000000","leechers":"0","seeders":"0","num_files":"0","size":"0","username":"","added":"0","status":"member","category":"0"}
	]`

	torrents, err := s.ParseHTML(strings.NewReader(jsonInput))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(torrents) != 1 {
		t.Fatalf("expected exactly 1 torrent (2 placeholders filtered), got %d", len(torrents))
	}

	tor := torrents[0]
	if tor.Title != "Real Release 2026 & Extras" {
		t.Errorf("expected HTML entity unescaped title, got %q", tor.Title)
	}
	if tor.InfoHash != "e1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Errorf("expected lowercase infohash, got %q", tor.InfoHash)
	}
	if tor.Seeds != 88 || tor.Leeches != 14 {
		t.Errorf("expected seeds=88, leeches=14, got %d, %d", tor.Seeds, tor.Leeches)
	}
	if tor.Size != 2147483648 {
		t.Errorf("expected size 2147483648, got %d", tor.Size)
	}
	if tor.Category != "TV/HD" {
		t.Errorf("expected Category TV/HD for 208, got %q", tor.Category)
	}
}

// -----------------------------------------------------------------------------
// 6. AudioBookBay and OxTorrent Bounded Concurrency Tests
// -----------------------------------------------------------------------------

func TestAdversarial_AudioBookBay_BoundedConcurrency(t *testing.T) {
	var activeWorkers int32
	var maxActiveWorkers int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&activeWorkers, 1)
		for {
			oldMax := atomic.LoadInt32(&maxActiveWorkers)
			if current <= oldMax || atomic.CompareAndSwapInt32(&maxActiveWorkers, oldMax, current) {
				break
			}
		}
		defer atomic.AddInt32(&activeWorkers, -1)

		if strings.Contains(r.URL.RawQuery, "s=tolkien") {
			// Search listing returning 4 books
			htmlContent := `<html><body>
				<div class="postTitle"><h2><a href="/detail/book1">The Hobbit</a></h2></div>
				<div class="postContent"><p>Posted: 12 Jan 2026</p>File Size: <span>500</span> MBs</div>
				<div class="postTitle"><h2><a href="/detail/book2">Fellowship of the Ring</a></h2></div>
				<div class="postContent"><p>Posted: 13 Jan 2026</p>File Size: <span>800</span> MBs</div>
				<div class="postTitle"><h2><a href="/detail/book3">The Two Towers</a></h2></div>
				<div class="postContent"><p>Posted: 14 Jan 2026</p>File Size: <span>750</span> MBs</div>
				<div class="postTitle"><h2><a href="/detail/book4">Return of the King</a></h2></div>
				<div class="postContent"><p>Posted: 15 Jan 2026</p>File Size: <span>900</span> MBs</div>
			</body></html>`
			fmt.Fprint(w, htmlContent)
			return
		}

		// Detail page
		time.Sleep(30 * time.Millisecond) // simulate fetch work
		hash := "1111111111111111111111111111111111111111"
		if strings.Contains(r.URL.Path, "book2") {
			hash = "2222222222222222222222222222222222222222"
		}
		detailHTML := fmt.Sprintf(`<html><body>
			<div class="postTitle"><h1>Tolkien Book</h1></div>
			<table>
				<tr><td>File Size:</td><td><span>600</span> MBs</td></tr>
				<tr><td>Info Hash:</td><td>%s</td></tr>
				<tr><td>Tracker:</td><td>udp://tracker.openbittorrent.com:80/announce</td></tr>
			</table>
		</body></html>`, hash)
		fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()

	scraper := NewAudioBookBayScraper(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	results, err := scraper.Search(context.Background(), "tolkien", WithLimit(4))
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	maxObserved := atomic.LoadInt32(&maxActiveWorkers)
	// AudioBookBay scraper must bound worker concurrency to 2
	if maxObserved > 2 {
		t.Errorf("bounded concurrency violation: max concurrent detail requests was %d, expected <= 2", maxObserved)
	}
}

func TestAdversarial_OxTorrent_BoundedConcurrencyAndDate(t *testing.T) {
	var activeWorkers int32
	var maxActiveWorkers int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&activeWorkers, 1)
		for {
			oldMax := atomic.LoadInt32(&maxActiveWorkers)
			if current <= oldMax || atomic.CompareAndSwapInt32(&maxActiveWorkers, oldMax, current) {
				break
			}
		}
		defer atomic.AddInt32(&activeWorkers, -1)

		if strings.Contains(r.URL.Path, "/recherche/") {
			// Search listing with 4 table rows
			htmlContent := `<table><tbody>
				<tr>
					<td class="liste-accueil-type" title="Films"></td>
					<td class="liste-accueil-nom"><a href="/detail/101" title="Film 101">Film 101 French</a></td>
					<td class="liste-accueil-taille">1.5 Go</td>
					<td class="sources">40</td>
					<td class="clients">5</td>
				</tr>
				<tr>
					<td class="liste-accueil-type" title="Séries"></td>
					<td class="liste-accueil-nom"><a href="/detail/102" title="Serie 102">Serie 102 French</a></td>
					<td class="liste-accueil-taille">700 Mo</td>
					<td class="sources">20</td>
					<td class="clients">2</td>
				</tr>
			</tbody></table>`
			fmt.Fprint(w, htmlContent)
			return
		}

		time.Sleep(30 * time.Millisecond)
		detailHTML := `<html><body>
			<span class="icon_date"><i class="fa fa-calendar"></i> 05/11/2026</span>
			<table><tr><td>Poids du fichier:</td><td><strong>1.5 Go</strong></td></tr></table>
			<div class="btn-download"><a href="magnet:?xt=urn:btih:3333333333333333333333333333333333333333&dn=Film">Download</a></div>
		</body></html>`
		fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()

	scraper := NewOxTorrentScraper(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	results, err := scraper.Search(context.Background(), "film", WithLimit(2))
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	maxObserved := atomic.LoadInt32(&maxActiveWorkers)
	// OxTorrent scraper must bound worker concurrency to 3
	if maxObserved > 3 {
		t.Errorf("bounded concurrency violation: max concurrent detail requests was %d, expected <= 3", maxObserved)
	}

	// Verify French date parsed (05/11/2026 -> 2026-11-05)
	if results[0].PublishedDate.IsZero() || results[0].PublishedDate.Year() != 2026 || results[0].PublishedDate.Month() != 11 || results[0].PublishedDate.Day() != 5 {
		t.Errorf("expected published date 2026-11-05, got %v", results[0].PublishedDate)
	}
}

// -----------------------------------------------------------------------------
// 7. EZTV Adversarial Tests: Query Cleaning, Suffix Removal & Resolution
// -----------------------------------------------------------------------------

func TestAdversarial_EZTV_CleaningAndResolution(t *testing.T) {
	s := NewEZTVScraper()

	t.Run("suffix removal and resolution mapping", func(t *testing.T) {
		jsonInput := `{
			"imdb_id": "0903747",
			"torrents_count": 4,
			"torrents": [
				{
					"id": 1,
					"hash": "4444444444444444444444444444444444444444",
					"title": "Better Call Saul S06E01 480p HDTV x264 EZTV",
					"size_bytes": 350000000,
					"seeds": 15,
					"peers": 3
				},
				{
					"id": 2,
					"hash": "5555555555555555555555555555555555555555",
					"title": "Better Call Saul S06E01 720p WEB x265 [eztv]",
					"size_bytes": 850000000,
					"seeds": "35",
					"peers": "8"
				},
				{
					"id": 3,
					"hash": "6666666666666666666666666666666666666666",
					"title": "Better Call Saul S06E01 1080p AMZN WEBRip DDP5 1 EZTV",
					"size_bytes": "1.8 GB",
					"seeds": 50,
					"peers": 12
				},
				{
					"id": 4,
					"hash": "7777777777777777777777777777777777777777",
					"title": "Better Call Saul S06E01 2160p UHD HDR [eztv]",
					"size_bytes": 5000000000,
					"seeds": 25,
					"peers": 4
				}
			]
		}`

		torrents, err := s.ParseHTML(strings.NewReader(jsonInput))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(torrents) != 4 {
			t.Fatalf("expected 4 torrents, got %d", len(torrents))
		}

		// 1. 480p -> TV/SD, stripped " EZTV"
		if torrents[0].Title != "Better Call Saul S06E01 480p HDTV x264" {
			t.Errorf("expected stripped ' EZTV', got %q", torrents[0].Title)
		}
		if torrents[0].Category != "TV/SD" {
			t.Errorf("expected TV/SD for 480p, got %q", torrents[0].Category)
		}

		// 2. 720p -> TV/HD, stripped "[eztv]", string seeds/peers
		if torrents[1].Title != "Better Call Saul S06E01 720p WEB x265" {
			t.Errorf("expected stripped '[eztv]', got %q", torrents[1].Title)
		}
		if torrents[1].Category != "TV/HD" {
			t.Errorf("expected TV/HD for 720p, got %q", torrents[1].Category)
		}
		if torrents[1].Seeds != 35 || torrents[1].Leeches != 8 {
			t.Errorf("expected seeds=35, leeches=8, got %d, %d", torrents[1].Seeds, torrents[1].Leeches)
		}

		// 3. 1080p -> TV/HD, size in human string "1.8 GB"
		if torrents[2].Category != "TV/HD" {
			t.Errorf("expected TV/HD for 1080p, got %q", torrents[2].Category)
		}
		if torrents[2].Size != 1932735283 {
			t.Errorf("expected size 1932735283 for 1.8 GB, got %d", torrents[2].Size)
		}

		// 4. 2160p -> TV/UHD
		if torrents[3].Category != "TV/UHD" {
			t.Errorf("expected TV/UHD for 2160p, got %q", torrents[3].Category)
		}
	})

	t.Run("numeric imdb id search without imdb suggestion api", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "get-torrents") {
				if r.URL.Query().Get("imdb_id") != "0903747" {
					http.Error(w, "bad imdb_id", http.StatusBadRequest)
					return
				}
				fmt.Fprint(w, `{"torrents_count":1,"torrents":[{"id":10,"hash":"8888888888888888888888888888888888888888","title":"Breaking Bad S01E01 720p EZTV","size_bytes":1000000000}]}`)
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		scraper := NewEZTVScraper(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
		res, err := scraper.Search(context.Background(), "tt0903747")
		if err != nil {
			t.Fatalf("Search with tt0903747 failed: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 result, got %d", len(res))
		}
		if res[0].Title != "Breaking Bad S01E01 720p" {
			t.Errorf("expected title without EZTV suffix, got %q", res[0].Title)
		}
	})
}

// -----------------------------------------------------------------------------
// 8. Registry Adversarial Tests: Concurrency, Cancellation & Partial Failures
// -----------------------------------------------------------------------------

func TestAdversarial_Registry_StressAndResilience(t *testing.T) {
	t.Run("concurrent registry searches and mutation", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(&mockTestScraper{
			name: "FastScraper1",
			results: []Torrent{
				{Title: "Fast 1", InfoHash: "1111111111111111111111111111111111111111", Source: "Fast1"},
			},
		})
		reg.Register(&mockTestScraper{
			name: "FastScraper2",
			results: []Torrent{
				{Title: "Fast 2", InfoHash: "2222222222222222222222222222222222222222", Source: "Fast2"},
			},
		})

		var wg sync.WaitGroup
		// 50 concurrent searchers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				torrents, err := reg.SearchAll(context.Background(), "query")
				if err != nil {
					t.Errorf("SearchAll failed in concurrent routine: %v", err)
				}
				if len(torrents) < 2 {
					t.Errorf("expected at least 2 torrents, got %d", len(torrents))
				}
			}()
		}

		// 10 concurrent readers/writers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				sName := fmt.Sprintf("DynamicScraper%d", idx)
				reg.Register(&mockTestScraper{
					name: sName,
					results: []Torrent{
						{Title: "Dyn", InfoHash: "3333333333333333333333333333333333333333", Source: sName},
					},
				})
				_ = reg.All()
				_, _ = reg.Get(sName)
			}(i)
		}

		wg.Wait()
	})

	t.Run("context cancellation during search", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(&mockTestScraper{
			name:  "HangingScraper",
			delay: 2 * time.Second,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := reg.SearchAll(ctx, "query")
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("expected context error, got nil")
		}
		if elapsed > 500*time.Millisecond {
			t.Errorf("SearchAll did not cancel promptly, took %v", elapsed)
		}
	})

	t.Run("partial failure returns available results", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(&mockTestScraper{
			name: "Failing1",
			err:  fmt.Errorf("connection refused"),
		})
		reg.Register(&mockTestScraper{
			name: "Failing2",
			err:  fmt.Errorf("503 Service Unavailable"),
		})
		reg.Register(&mockTestScraper{
			name: "Working1",
			results: []Torrent{
				{Title: "Found Torrent", InfoHash: "4444444444444444444444444444444444444444", Source: "Working1"},
			},
		})

		res, err := reg.SearchAll(context.Background(), "query")
		if err != nil {
			t.Fatalf("SearchAll returned error despite 1 scraper succeeding: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 result from working scraper, got %d", len(res))
		}
		if res[0].Title != "Found Torrent" {
			t.Errorf("expected 'Found Torrent', got %q", res[0].Title)
		}
	})

	t.Run("all scrapers failing returns aggregate error", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(&mockTestScraper{
			name: "Failing1",
			err:  fmt.Errorf("error 1"),
		})
		reg.Register(&mockTestScraper{
			name: "Failing2",
			err:  fmt.Errorf("error 2"),
		})

		res, err := reg.SearchAll(context.Background(), "query")
		if err == nil {
			t.Fatalf("expected error when all scrapers fail, got res=%v", res)
		}
		if !strings.Contains(err.Error(), "all scrapers failed") {
			t.Errorf("expected aggregate error message, got %q", err.Error())
		}
	})
}
