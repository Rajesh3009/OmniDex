package scraper

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Milestone 3 Empirical Challenger Test Suite
// -----------------------------------------------------------------------------

var allFixtureNames = []string{
	"yts.json",
	"yts_empty.json",
	"nyaa.xml",
	"sukebei.xml",
	"torrentdownloads.xml",
	"thepiratebay.json",
	"audiobookbay_search.html",
	"audiobookbay_detail.html",
	"eztv.json",
	"eztv_search.html",
	"oxtorrent_search.html",
	"oxtorrent_detail.html",
}

func getAllScrapers() []SourceScraper {
	return []SourceScraper{
		NewYTSScraper(),
		NewNyaaScraper(),
		NewSukebeiScraper(),
		NewTorrentDownloadsScraper(),
		NewThePirateBayScraper(),
		NewAudioBookBayScraper(),
		NewEZTVScraper(),
		NewOxTorrentScraper(),
	}
}

// TestM3_Empirical_OfflineIsolation proves that tests and scrapers do not reach out
// to external networks.
func TestM3_Empirical_OfflineIsolation(t *testing.T) {
	// Verify that if any scraper Search() is given an invalid base URL, it fails cleanly
	// without hanging or silently trying default live endpoints.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	deadClient := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, fmt.Errorf("isolated: network egress blocked to %s", addr)
			},
		},
	}

	for _, s := range getAllScrapers() {
		t.Run(s.Name()+"_EgressBlocked", func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked under blocked network: %v", s.Name(), r)
				}
			}()

			var testScraper SourceScraper
			switch s.Name() {
			case "YTS":
				testScraper = NewYTSScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "Nyaa.si":
				testScraper = NewNyaaScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "Sukebei":
				testScraper = NewSukebeiScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "TorrentDownloads":
				testScraper = NewTorrentDownloadsScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "The Pirate Bay":
				testScraper = NewThePirateBayScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "AudioBookBay":
				testScraper = NewAudioBookBayScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "EZTV":
				testScraper = NewEZTVScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			case "OxTorrent":
				testScraper = NewOxTorrentScraper(WithBaseURL("http://blocked.invalid"), WithHTTPClient(deadClient))
			}

			query := "test-query"
			if s.Name() == "EZTV" {
				// EZTV with IMDb ID bypasses title suggestion lookup and hits API base URL
				query = "tt1234567"
			}

			_, err := testScraper.Search(ctx, query)
			if err == nil {
				t.Errorf("%s: expected search to fail when network egress is blocked, got nil error", s.Name())
			}
		})
	}
}

// TestM3_Empirical_FixtureTruncationMutations tests truncated variants of all 12 fixtures.
func TestM3_Empirical_FixtureTruncationMutations(t *testing.T) {
	scrapers := getAllScrapers()

	for _, fixtureName := range allFixtureNames {
		raw, err := testdataFS.ReadFile("testdata/" + fixtureName)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", fixtureName, err)
		}

		truncFractions := []float64{0.0, 0.01, 0.05, 0.10, 0.25, 0.50, 0.75, 0.90, 0.99}

		for _, s := range scrapers {
			t.Run(fmt.Sprintf("%s_on_%s_truncated", s.Name(), fixtureName), func(t *testing.T) {
				for _, frac := range truncFractions {
					cutoff := int(float64(len(raw)) * frac)
					if cutoff > len(raw) {
						cutoff = len(raw)
					}
					mutated := raw[:cutoff]

					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("%s panicked on fixture %s truncated at fraction %.2f (%d/%d bytes): %v",
									s.Name(), fixtureName, frac, cutoff, len(raw), r)
							}
						}()

						res, err := s.ParseHTML(bytes.NewReader(mutated))
						_ = err
						// Invariant: returned torrents must never contain all-zero infohash
						for _, tor := range res {
							if tor.InfoHash == "0000000000000000000000000000000000000000" {
								t.Errorf("%s emitted all-zero infohash on truncated fixture %s", s.Name(), fixtureName)
							}
						}
					}()
				}

				// Also test N-1 byte (last byte dropped)
				if len(raw) > 1 {
					mutated := raw[:len(raw)-1]
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked on %s with last byte stripped: %v", s.Name(), fixtureName, r)
						}
					}()
					_, _ = s.ParseHTML(bytes.NewReader(mutated))
				}
			})
		}
	}
}

// TestM3_Empirical_FixtureBitFlipFuzzing introduces random byte mutations into fixtures.
func TestM3_Empirical_FixtureBitFlipFuzzing(t *testing.T) {
	scrapers := getAllScrapers()
	rng := rand.New(rand.NewSource(42))

	for _, fixtureName := range allFixtureNames {
		raw, err := testdataFS.ReadFile("testdata/" + fixtureName)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", fixtureName, err)
		}

		for _, s := range scrapers {
			t.Run(fmt.Sprintf("%s_on_%s_fuzz", s.Name(), fixtureName), func(t *testing.T) {
				// Perform 25 randomized mutations per fixture
				for trial := 0; trial < 25; trial++ {
					mutated := make([]byte, len(raw))
					copy(mutated, raw)

					numCorruptions := rng.Intn(10) + 1
					for i := 0; i < numCorruptions && len(mutated) > 0; i++ {
						idx := rng.Intn(len(mutated))
						switch rng.Intn(5) {
						case 0:
							mutated[idx] = 0x00 // Null byte
						case 1:
							mutated[idx] = 0xFF // High bit byte
						case 2:
							mutated[idx] = '<'
						case 3:
							mutated[idx] = '"'
						case 4:
							mutated[idx] = '\n'
						}
					}

					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("%s panicked during bit-flip trial %d on %s: %v",
									s.Name(), trial, fixtureName, r)
							}
						}()

						res, _ := s.ParseHTML(bytes.NewReader(mutated))
						for _, tor := range res {
							if tor.InfoHash == "0000000000000000000000000000000000000000" {
								t.Errorf("%s emitted all-zero infohash during bit-flip on %s", s.Name(), fixtureName)
							}
						}
					}()
				}
			})
		}
	}
}

// TestM3_Empirical_CorruptedInfoHashMutations verifies that scrapers never accept or emit
// corrupted, non-hex, or all-zero infohashes when present in mutated fixtures.
func TestM3_Empirical_CorruptedInfoHashMutations(t *testing.T) {
	// InfoHash regex to replace in fixtures
	hashRegex := regexp.MustCompile(`(?i)[0-9a-f]{40}`)

	adversarialHashes := []string{
		"0000000000000000000000000000000000000000",                                    // All-zeros
		"ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",                                    // Non-hex
		"0123456789abcdef0123456789abcdef0123456",                                     // 39 chars (too short)
		"0123456789abcdef0123456789abcdef012345678",                                   // 41 chars (too long)
		"<script>alert(1)</script>00000000000000",                                     // XSS injection
		"''''''''''''''''''''''''''''''''''''''''",                                    // SQL quotes
	}

	for _, fixtureName := range []string{"yts.json", "nyaa.xml", "torrentdownloads.xml", "thepiratebay.json", "audiobookbay_detail.html", "eztv.json", "oxtorrent_detail.html"} {
		raw, err := testdataFS.ReadFile("testdata/" + fixtureName)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", fixtureName, err)
		}

		for _, badHash := range adversarialHashes {
			mutated := hashRegex.ReplaceAll(raw, []byte(badHash))

			for _, s := range getAllScrapers() {
				t.Run(fmt.Sprintf("%s_%s_hash_%s", s.Name(), fixtureName, badHash[:8]), func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked on fixture %s with hash %s: %v", s.Name(), fixtureName, badHash, r)
						}
					}()

					res, err := s.ParseHTML(bytes.NewReader(mutated))
					_ = err

					for _, tor := range res {
						if tor.InfoHash == badHash && badHash != "0000000000000000000000000000000000000000" {
							t.Errorf("%s accepted invalid infohash %q in %s", s.Name(), badHash, fixtureName)
						}
						if tor.InfoHash == "0000000000000000000000000000000000000000" {
							t.Errorf("%s accepted 40-zero infohash in %s", s.Name(), fixtureName)
						}
					}
				})
			}
		}
	}
}

// TestM3_Empirical_HTTPServerFaults tests scrapers against abrupt HTTP connection drops,
// 500 internal server errors, and 1MB binary garbage bodies.
func TestM3_Empirical_HTTPServerFaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Server returning HTTP 500
	ts500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts500.Close()

	// 2. Server returning 1MB of binary garbage
	tsGarbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		garbage := make([]byte, 1024*1024)
		for i := range garbage {
			garbage[i] = byte(i % 256)
		}
		w.Write(garbage)
	}))
	defer tsGarbage.Close()

	// 3. Server abruptly dropping TCP connection
	tsDrop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		// Write partial HTTP header then close connection immediately
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\nPartial"))
		conn.Close()
	}))
	defer tsDrop.Close()

	faultServers := []struct {
		name   string
		server *httptest.Server
	}{
		{"HTTP_500", ts500},
		{"Binary_Garbage_1MB", tsGarbage},
		{"Abrupt_TCP_Drop", tsDrop},
	}

	for _, fs := range faultServers {
		for _, s := range getAllScrapers() {
			t.Run(fmt.Sprintf("%s_under_%s", s.Name(), fs.name), func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s panicked under %s: %v", s.Name(), fs.name, r)
					}
				}()

				var scraper SourceScraper
				switch s.Name() {
				case "YTS":
					scraper = NewYTSScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "Nyaa.si":
					scraper = NewNyaaScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "Sukebei":
					scraper = NewSukebeiScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "TorrentDownloads":
					scraper = NewTorrentDownloadsScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "The Pirate Bay":
					scraper = NewThePirateBayScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "AudioBookBay":
					scraper = NewAudioBookBayScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "EZTV":
					scraper = NewEZTVScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				case "OxTorrent":
					scraper = NewOxTorrentScraper(WithBaseURL(fs.server.URL), WithHTTPClient(fs.server.Client()))
				}

				res, err := scraper.Search(ctx, "stress-query")
				// Must either return error or empty slice, never panic
				_ = res
				_ = err
			})
		}
	}
}
