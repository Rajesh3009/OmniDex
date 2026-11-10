package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockTrendingScraper struct {
	name     string
	torrents []Torrent
	err      error
}

func (m *mockTrendingScraper) Name() string {
	return m.name
}

func (m *mockTrendingScraper) Search(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	return m.torrents, m.err
}

func (m *mockTrendingScraper) ParseHTML(r io.Reader) ([]Torrent, error) {
	return m.torrents, m.err
}

func (m *mockTrendingScraper) Trending(ctx context.Context, opts ...Option) ([]Torrent, error) {
	return m.torrents, m.err
}

func TestTrendingScraper_ThePirateBay(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Apibay JSON format
		_, _ = w.Write([]byte(`[
			{"id":"101","name":"Big.Buck.Bunny.1080p","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":"850000000","seeders":"500","leechers":"45","category":"200"},
			{"id":"102","name":"Elephant.Dream.720p","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":"450000000","seeders":"150","leechers":"15","category":"200"}
		]`))
	}))
	defer ts.Close()

	scraper := NewThePirateBayScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	torrents, err := scraper.Trending(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from TPB Trending: %v", err)
	}
	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}
	if torrents[0].Seeds != 500 {
		t.Errorf("expected 500 seeds, got %d", torrents[0].Seeds)
	}
	if torrents[0].InfoHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("expected infohash a..., got %s", torrents[0].InfoHash)
	}
}

func TestTrendingScraper_YTS(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sort_by") != "seeds" {
			t.Errorf("expected sort_by=seeds, got %s", r.URL.Query().Get("sort_by"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"data": {
				"movie_count": 1,
				"movies": [
					{
						"id": 1,
						"title": "Popular Film (2024)",
						"year": 2024,
						"torrents": [
							{
								"hash": "cccccccccccccccccccccccccccccccccccccccc",
								"quality": "1080p",
								"type": "bluray",
								"seeds": 1200,
								"peers": 80,
								"size_bytes": 2147483648
							}
						]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	scraper := NewYTSScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	torrents, err := scraper.Trending(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from YTS Trending: %v", err)
	}
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}
	if torrents[0].Seeds != 1200 {
		t.Errorf("expected 1200 seeds, got %d", torrents[0].Seeds)
	}
}

func TestTrendingScraper_EZTV(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"torrents": [
				{
					"id": 55,
					"hash": "dddddddddddddddddddddddddddddddddddddddd",
					"filename": "Popular.Show.S01E01.720p.HDTV",
					"size_bytes": "524288000",
					"seeds": 350,
					"peers": 40
				}
			]
		}`))
	}))
	defer ts.Close()

	scraper := NewEZTVScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	torrents, err := scraper.Trending(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from EZTV Trending: %v", err)
	}
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}
	if torrents[0].Seeds != 350 {
		t.Errorf("expected 350 seeds, got %d", torrents[0].Seeds)
	}
}

func TestTrendingScraper_Nyaa(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") != "seeders" {
			t.Errorf("expected s=seeders in query params")
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		xml := `<?xml version="1.0" encoding="utf-8"?>
		<rss xmlns:nyaa="https://nyaa.si/xmlns/nyaa" version="2.0">
			<channel>
				<title>Nyaa</title>
				<item>
					<title>[SubGroup] Anime Episode 01 [1080p]</title>
					<link>https://nyaa.si/download/100.torrent</link>
					<guid isPermaLink="true">https://nyaa.si/view/100</guid>
					<nyaa:infoHash>eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee</nyaa:infoHash>
					<nyaa:size>1.4 GiB</nyaa:size>
					<nyaa:seeders>800</nyaa:seeders>
					<nyaa:leechers>50</nyaa:leechers>
					<nyaa:category>1_2</nyaa:category>
				</item>
			</channel>
		</rss>`
		fmt.Fprint(w, xml)
	}))
	defer ts.Close()

	scraper := NewNyaaScraper(WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	torrents, err := scraper.Trending(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from Nyaa Trending: %v", err)
	}
	if len(torrents) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(torrents))
	}
	if torrents[0].Seeds != 800 {
		t.Errorf("expected 800 seeds, got %d", torrents[0].Seeds)
	}
}

func TestRegistry_TrendingScrapers(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTrendingScraper{
		name: "ActiveSource",
		torrents: []Torrent{
			{Title: "Active 1", InfoHash: "1111111111111111111111111111111111111111", Seeds: 100},
		},
	})
	reg.Register(&mockTrendingScraper{
		name: "ExcludedSource",
		torrents: []Torrent{
			{Title: "Excluded 1", InfoHash: "2222222222222222222222222222222222222222", Seeds: 200},
		},
	})

	filter := map[string]bool{
		"activesource": true,
	}

	res, err := reg.TrendingScrapers(context.Background(), filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 torrent from active source, got %d", len(res))
	}
	if res[0].Title != "Active 1" {
		t.Errorf("expected Title 'Active 1', got '%s'", res[0].Title)
	}
}
