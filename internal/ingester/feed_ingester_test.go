package ingester

import (
	"encoding/json"
	"encoding/xml"
	"testing"
)

func TestParseRSSItem(t *testing.T) {
	xmlData := `
	<item>
		<title>Ubuntu 24.04 LTS Desktop amd64</title>
		<link>magnet:?xt=urn:btih:d35ec4bc7a04dd2664988bb11c1d74268e0d47ad&amp;dn=ubuntu-24.04-desktop-amd64.iso</link>
		<pubDate>Mon, 26 Oct 2026 12:00:00 +0000</pubDate>
		<enclosure url="magnet:?xt=urn:btih:d35ec4bc7a04dd2664988bb11c1d74268e0d47ad" length="6115295232" type="application/x-bittorrent" />
		<attr name="infohash" value="d35ec4bc7a04dd2664988bb11c1d74268e0d47ad" />
		<attr name="seeders" value="142" />
		<attr name="peers" value="23" />
	</item>`

	var it rssItem
	if err := xml.Unmarshal([]byte(xmlData), &it); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	ing := &Ingester{}
	rec := ing.parseRSSItemToRecord(it, "Test Feed")
	if rec == nil {
		t.Fatalf("expected non-nil record")
	}

	if rec.InfoHash != "d35ec4bc7a04dd2664988bb11c1d74268e0d47ad" {
		t.Errorf("expected infohash d35ec4bc7a04dd2664988bb11c1d74268e0d47ad, got %s", rec.InfoHash)
	}
	if rec.Seeders != 142 {
		t.Errorf("expected 142 seeders, got %d", rec.Seeders)
	}
	if rec.Leechers != 23 {
		t.Errorf("expected 23 leechers, got %d", rec.Leechers)
	}
	if rec.Category != "Software" {
		t.Errorf("expected category Software, got %s", rec.Category)
	}
	if rec.Source != "test-feed" {
		t.Errorf("expected source test-feed, got %s", rec.Source)
	}
}

func TestGuessCategory(t *testing.T) {
	tests := []struct {
		title    string
		cat      string
		expected string
	}{
		{"Cyberpunk 2077 v2.1 Repack-DODI", "Games", "Software"},
		{"Blade Runner 2049 2017 1080p BluRay x264", "Movies", "Video"},
		{"Pink Floyd - The Dark Side of the Moon FLAC", "Music", "Audio"},
		{"Clean Code A Handbook of Agile Software Craftsmanship PDF", "E-Books", "Books"},
		{"Brazzers Exxtra - Hot Summer 1080p", "Adult", "XXX"},
	}

	for _, tc := range tests {
		got := guessCategory(tc.title, tc.cat)
		if got != tc.expected {
			t.Errorf("guessCategory(%q, %q) = %s, expected %s", tc.title, tc.cat, got, tc.expected)
		}
	}
}

func TestIngestEZTVJSON(t *testing.T) {
	sampleJSON := []byte(`{
		"torrents_count": 1,
		"limit": 10,
		"page": 1,
		"torrents": [
			{
				"id": 12345,
				"hash": "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
				"filename": "Breaking.Bad.S01E01.720p.BluRay.x264.mkv",
				"title": "Breaking Bad S01E01 720p BluRay x264",
				"magnet_url": "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd&dn=Breaking+Bad",
				"seeds": 85,
				"peers": 12,
				"date_released_unix": 1700000000,
				"size_bytes": "1048576000"
			}
		]
	}`)

	// Using a nil store just to test JSON unmarshaling and record parsing without db insertion errors
	var resp eztvResponse
	if err := json.Unmarshal(sampleJSON, &resp); err != nil {
		t.Fatalf("Unmarshal EZTV JSON failed: %v", err)
	}

	if len(resp.Torrents) != 1 {
		t.Fatalf("expected 1 torrent in EZTV response, got %d", len(resp.Torrents))
	}

	torrent := resp.Torrents[0]
	if torrent.Hash != "abcdefabcdefabcdefabcdefabcdefabcdefabcd" {
		t.Errorf("expected hash abcdefabcdefabcdefabcdefabcdefabcdefabcd, got %s", torrent.Hash)
	}

	size, err := torrent.SizeBytes.Int64()
	if err != nil || size != 1048576000 {
		t.Errorf("expected size 1048576000, got %d (err: %v)", size, err)
	}
}

