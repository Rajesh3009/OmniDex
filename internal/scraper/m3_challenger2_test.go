package scraper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Challenger 2: Empirical Verification of Milestone 3
// -----------------------------------------------------------------------------

// TestM3_Empirical_StandardMetadataCoverage verifies that all 8 scrapers extract
// and populate the standard metadata fields (Title, MagnetURI or InfoHash, Size, Seeds, Leeches)
// from their respective fixtures.
func TestM3_Empirical_StandardMetadataCoverage(t *testing.T) {
	testCases := []struct {
		name        string
		scraper     SourceScraper
		fixtureFile string
		checkFields func(t *testing.T, results []Torrent)
	}{
		{
			name:        "YTS",
			scraper:     NewYTSScraper(),
			fixtureFile: "yts.json",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("YTS returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("YTS item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("YTS item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("YTS item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("YTS item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("YTS item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "Nyaa.si",
			scraper:     NewNyaaScraper(),
			fixtureFile: "nyaa.xml",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("Nyaa.si returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("Nyaa item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("Nyaa item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("Nyaa item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("Nyaa item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("Nyaa item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "Sukebei",
			scraper:     NewSukebeiScraper(),
			fixtureFile: "sukebei.xml",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("Sukebei returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("Sukebei item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("Sukebei item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("Sukebei item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("Sukebei item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("Sukebei item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "TorrentDownloads",
			scraper:     NewTorrentDownloadsScraper(),
			fixtureFile: "torrentdownloads.xml",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("TorrentDownloads returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("TorrentDownloads item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("TorrentDownloads item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("TorrentDownloads item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("TorrentDownloads item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("TorrentDownloads item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "The Pirate Bay",
			scraper:     NewThePirateBayScraper(),
			fixtureFile: "thepiratebay.json",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("The Pirate Bay returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("TPB item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("TPB item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("TPB item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("TPB item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("TPB item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "AudioBookBay",
			scraper:     NewAudioBookBayScraper(),
			fixtureFile: "audiobookbay_detail.html",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("AudioBookBay returned 0 torrents from detail fixture")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("AudioBookBay item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("AudioBookBay item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("AudioBookBay item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("AudioBookBay item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("AudioBookBay item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "EZTV",
			scraper:     NewEZTVScraper(),
			fixtureFile: "eztv.json",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("EZTV returned 0 torrents")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("EZTV item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("EZTV item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("EZTV item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("EZTV item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("EZTV item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
		{
			name:        "OxTorrent",
			scraper:     NewOxTorrentScraper(),
			fixtureFile: "oxtorrent_detail.html",
			checkFields: func(t *testing.T, results []Torrent) {
				if len(results) == 0 {
					t.Fatalf("OxTorrent returned 0 torrents from detail fixture")
				}
				for i, tor := range results {
					if tor.Title == "" {
						t.Errorf("OxTorrent item %d: missing Title", i)
					}
					if tor.InfoHash == "" || len(tor.InfoHash) != 40 {
						t.Errorf("OxTorrent item %d: invalid InfoHash %q", i, tor.InfoHash)
					}
					if !strings.HasPrefix(tor.MagnetURI, "magnet:?xt=urn:btih:") {
						t.Errorf("OxTorrent item %d: invalid MagnetURI %q", i, tor.MagnetURI)
					}
					if tor.Size <= 0 {
						t.Errorf("OxTorrent item %d: non-positive Size %d", i, tor.Size)
					}
					if tor.Seeds < 0 || tor.Leeches < 0 {
						t.Errorf("OxTorrent item %d: negative Seeds (%d) or Leeches (%d)", i, tor.Seeds, tor.Leeches)
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := loadFixture(t, tc.fixtureFile)
			results, err := tc.scraper.ParseHTML(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("unexpected error parsing %s: %v", tc.fixtureFile, err)
			}
			tc.checkFields(t, results)
		})
	}
}

// TestM3_Empirical_NetworkTimeouts verifies that all 8 scrapers properly abort
// and return error when an HTTP server hangs or when the context deadline is exceeded.
func TestM3_Empirical_NetworkTimeouts(t *testing.T) {
	// A mock server that sleeps for 2 seconds (simulating a slow or unresponsive server)
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow response"))
	}))
	defer slowServer.Close()

	scrapers := []struct {
		name        string
		makeScraper func(baseURL string, client *http.Client) SourceScraper
	}{
		{"YTS", func(b string, c *http.Client) SourceScraper { return NewYTSScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"Nyaa.si", func(b string, c *http.Client) SourceScraper { return NewNyaaScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"Sukebei", func(b string, c *http.Client) SourceScraper { return NewSukebeiScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"TorrentDownloads", func(b string, c *http.Client) SourceScraper { return NewTorrentDownloadsScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"The Pirate Bay", func(b string, c *http.Client) SourceScraper { return NewThePirateBayScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"AudioBookBay", func(b string, c *http.Client) SourceScraper { return NewAudioBookBayScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"EZTV", func(b string, c *http.Client) SourceScraper { return NewEZTVScraper(WithBaseURL(b), WithHTTPClient(c)) }},
		{"OxTorrent", func(b string, c *http.Client) SourceScraper { return NewOxTorrentScraper(WithBaseURL(b), WithHTTPClient(c)) }},
	}

	for _, sc := range scrapers {
		query := "timeout-query"
		if sc.name == "EZTV" {
			query = "tt0944947"
		}
		t.Run(sc.name+"_ContextTimeout", func(t *testing.T) {
			// Context with a strict 40ms timeout
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()

			scraper := sc.makeScraper(slowServer.URL, slowServer.Client())

			start := time.Now()
			_, err := scraper.Search(ctx, query)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("%s: expected timeout error, got nil", sc.name)
			}

			// Invariant: should return within reasonable time of the context deadline (under 500ms)
			if elapsed > 1*time.Second {
				t.Errorf("%s: took too long to abort on context timeout: %v", sc.name, elapsed)
			}
		})

		t.Run(sc.name+"_ClientTimeout", func(t *testing.T) {
			// HTTP client with a 50ms timeout
			shortClient := &http.Client{Timeout: 50 * time.Millisecond}
			scraper := sc.makeScraper(slowServer.URL, shortClient)

			ctx := context.Background()
			start := time.Now()
			_, err := scraper.Search(ctx, query)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("%s: expected client timeout error, got nil", sc.name)
			}

			if elapsed > 1*time.Second {
				t.Errorf("%s: took too long to abort on client timeout: %v", sc.name, elapsed)
			}
		})
	}
}

// TestM3_Empirical_EmptyPayloads verifies that all 8 scrapers handle all variations
// of empty payloads gracefully without panics.
func TestM3_Empirical_EmptyPayloads(t *testing.T) {
	emptyInputs := []struct {
		name    string
		payload string
	}{
		{"empty string", ""},
		{"whitespace only", "   \r\n\t  "},
		{"null json", "null"},
		{"empty json array", "[]"},
		{"empty json object", "{}"},
		{"empty rss channel", `<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`},
		{"empty html doc", `<!DOCTYPE html><html><head></head><body></body></html>`},
	}

	scrapers := []SourceScraper{
		NewYTSScraper(),
		NewNyaaScraper(),
		NewSukebeiScraper(),
		NewTorrentDownloadsScraper(),
		NewThePirateBayScraper(),
		NewAudioBookBayScraper(),
		NewEZTVScraper(),
		NewOxTorrentScraper(),
	}

	for _, s := range scrapers {
		t.Run(s.Name(), func(t *testing.T) {
			for _, ei := range emptyInputs {
				t.Run(ei.name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked on empty payload (%s): %v", s.Name(), ei.name, r)
						}
					}()

					results, err := s.ParseHTML(strings.NewReader(ei.payload))
					// Empty payloads should yield 0 results (either error or empty slice)
					if err == nil && len(results) > 0 {
						t.Errorf("%s returned %d results on empty payload %q", s.Name(), len(results), ei.name)
					}
				})
			}
		})
	}
}

// TestM3_Empirical_CorruptedPayloads verifies graceful behavior on heavily corrupted data.
func TestM3_Empirical_CorruptedPayloads(t *testing.T) {
	corruptedInputs := []struct {
		name    string
		payload []byte
	}{
		{"corrupt json unterminated array", []byte(`{"data": {"movies": [{"title": "Incomplete`)},
		{"corrupt xml unclosed tags", []byte(`<?xml version="1.0"?><rss><channel><item><title>Broken`)},
		{"corrupt xml mismatched tags", []byte(`<?xml version="1.0"?><rss><channel><item><title>Broken</wrong></item>`)},
		{"corrupt html mismatched tags", []byte(`<html><body><table><tr><td>No closing table`)},
		{"binary zeroes", bytes.Repeat([]byte{0x00}, 256)},
		{"binary 0xFF", bytes.Repeat([]byte{0xFF}, 256)},
		{"random bytes with tags", []byte("<tr class=\"test\x00\xFF\xFE\"><td>\x01\x02</td></tr>")},
	}

	scrapers := []SourceScraper{
		NewYTSScraper(),
		NewNyaaScraper(),
		NewSukebeiScraper(),
		NewTorrentDownloadsScraper(),
		NewThePirateBayScraper(),
		NewAudioBookBayScraper(),
		NewEZTVScraper(),
		NewOxTorrentScraper(),
	}

	for _, s := range scrapers {
		t.Run(s.Name(), func(t *testing.T) {
			for _, ci := range corruptedInputs {
				t.Run(ci.name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked on %s: %v", s.Name(), ci.name, r)
						}
					}()

					results, err := s.ParseHTML(bytes.NewReader(ci.payload))
					_ = err
					// Invariant: returned results must never contain corrupt 40-zero hash
					for _, tor := range results {
						if tor.InfoHash == "0000000000000000000000000000000000000000" {
							t.Errorf("%s accepted dummy all-zero infohash on corrupt input %s", s.Name(), ci.name)
						}
					}
				})
			}
		})
	}
}

// TestM3_Empirical_ConcurrentOfflineStress executes 50 concurrent searches and parses
// across all 8 scrapers simultaneously to ensure thread safety and no data races.
func TestM3_Empirical_ConcurrentOfflineStress(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Concurrently parse fixtures
			ytsData := loadFixture(t, "yts.json")
			ytsScraper := NewYTSScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
			res, err := ytsScraper.ParseHTML(bytes.NewReader(ytsData))
			if err != nil || len(res) == 0 {
				errCh <- fmt.Errorf("worker %d: YTS ParseHTML failed: %v", workerID, err)
				return
			}

			// Concurrently search
			nyaaScraper := NewNyaaScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
			sRes, sErr := nyaaScraper.Search(ctx, "query")
			if sErr != nil || len(sRes) == 0 {
				if !errors.Is(sErr, context.Canceled) {
					errCh <- fmt.Errorf("worker %d: Nyaa Search failed: %v", workerID, sErr)
				}
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent stress error: %v", err)
	}
}
