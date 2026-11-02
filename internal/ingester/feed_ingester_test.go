package ingester

import (
	"bytes"
	"encoding/csv"
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

func TestIngestTPBJSON(t *testing.T) {
	sampleTPB := []byte(`[
		{
			"id": "84919139",
			"info_hash": "157452D0FC95D354035EFC736AB74AD0230B4B6F",
			"category": "208",
			"name": "Tracker.2024.S04E05.1080p.x265-ELiTE",
			"status": "vip",
			"num_files": "6",
			"size": "899604077",
			"seeders": "42",
			"leechers": "7",
			"added": "1793603871"
		}
	]`)

	var items []tpbItem
	if err := json.Unmarshal(sampleTPB, &items); err != nil {
		t.Fatalf("Unmarshal TPB JSON failed: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	it := items[0]
	if it.InfoHash != "157452D0FC95D354035EFC736AB74AD0230B4B6F" {
		t.Errorf("unexpected infohash: %s", it.InfoHash)
	}
	if toInt64(it.Seeders) != 42 {
		t.Errorf("expected 42 seeders, got %d", toInt64(it.Seeders))
	}
	if toInt64(it.Leechers) != 7 {
		t.Errorf("expected 7 leechers, got %d", toInt64(it.Leechers))
	}
	if toInt64(it.Size) != 899604077 {
		t.Errorf("expected size 899604077, got %d", toInt64(it.Size))
	}

	cat := mapTPBCategory(int(toInt64(it.Category)), it.Name)
	if cat != "Video" {
		t.Errorf("expected Category Video for cat 208, got %s", cat)
	}
}

func TestIngestTorrentsCSVFormat(t *testing.T) {
	csvData := []byte("infohash,name,size_bytes,created_unix,seeders,leechers,completed,scraped_date,published\n" +
		"7ede2d8b2fb9bba3ef44993303a9ed9b2eb3666a,\"Adobe Illustrator 2024 v33.3 + Crack (macOS)\",1824730597,1723704841,87020,57886,70,1792941163,1697467411\n" +
		"00001f7eddd01e6f2d9d8c28695f3770bdcddfee,\"[SubsPlease] Lv1 Maou to One Room Yuusha - 06 (1080p)\",420876353,1693929977,15,2,8,1792926928,1693929982\n")

	ing := &Ingester{}
	// Test header parsing logic
	reader := csv.NewReader(bytes.NewReader(csvData))
	header, err := reader.Read()
	if err != nil {
		t.Fatalf("failed to read CSV header: %v", err)
	}

	colMap := make(map[string]int)
	for i, col := range header {
		colMap[col] = i
	}

	if _, ok := colMap["infohash"]; !ok {
		t.Errorf("missing infohash in colMap")
	}
	if _, ok := colMap["name"]; !ok {
		t.Errorf("missing name in colMap")
	}

	r1, err := reader.Read()
	if err != nil {
		t.Fatalf("failed to read record 1: %v", err)
	}

	if r1[colMap["infohash"]] != "7ede2d8b2fb9bba3ef44993303a9ed9b2eb3666a" {
		t.Errorf("unexpected infohash: %s", r1[colMap["infohash"]])
	}
	if r1[colMap["name"]] != "Adobe Illustrator 2024 v33.3 + Crack (macOS)" {
		t.Errorf("unexpected name: %s", r1[colMap["name"]])
	}

	cat := guessCategory(r1[colMap["name"]], "")
	if cat != "Software" {
		t.Errorf("expected Software category, got %s", cat)
	}
	_ = ing
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"220.84MB", 231567523},
		{"220.84 MB", 231567523},
		{"277.1 MiB", 290560409},
		{"1.5 GB", 1610612736},
		{"2 GiB", 2147483648},
		{"500 KB", 512000},
		{"512 KiB", 524288},
		{"1024 B", 1024},
		{"1.2 TB", 1319413953331},
	}

	for _, tc := range tests {
		got := parseByteSize(tc.input)
		// Allow small margin of delta due to float representation
		delta := got - tc.expected
		if delta < 0 {
			delta = -delta
		}
		if delta > 1000 {
			t.Errorf("parseByteSize(%q) = %d, expected ~%d", tc.input, got, tc.expected)
		}
	}
}

func TestParseRSSItem_TokyoTosho(t *testing.T) {
	// Tokyo Toshokan puts size and base32 magnet link inside <description>
	xmlData := `
	<item>
		<title>[nam..] レミエール (Zenless Zone Zero).zip</title>
		<link>https://www.tokyotosho.info/details.php?id=12345</link>
		<description>Comment: None&lt;br /&gt;Size: 220.84MB&lt;br /&gt;Submitter: Anonymous&lt;br /&gt;&lt;a href="magnet:?xt=urn:btih:3V33QQRKPTJVEPJ6XZV3FCEVHV6NW5HM&amp;dn=test"&gt;magnet&lt;/a&gt;</description>
	</item>`

	var it rssItem
	if err := xml.Unmarshal([]byte(xmlData), &it); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	ing := &Ingester{}
	rec := ing.parseRSSItemToRecord(it, "tokyotosho")
	if rec == nil {
		t.Fatalf("expected non-nil record")
	}

	if rec.Size <= 0 {
		t.Errorf("expected size > 0 from TokyoTosho description, got %d", rec.Size)
	}
	// 220.84 MB is ~231,567,523 bytes
	if rec.Size < 231000000 || rec.Size > 232000000 {
		t.Errorf("unexpected size for 220.84MB: %d", rec.Size)
	}

	// Infohash should be converted from Base32 (3V33QQRKPTJVEPJ6XZV3FCEVHV6NW5HM) to hex
	if len(rec.InfoHash) != 40 {
		t.Errorf("expected 40-char hex infohash, got %d chars: %s", len(rec.InfoHash), rec.InfoHash)
	}
}

func TestParseRSSItem_Knaben(t *testing.T) {
	// Knaben format: magnet link in <link>, size in <description>
	xmlData := `
	<item>
		<title>Resident.Evil.2026.2160p.WEB-DL [277.1 MB]</title>
		<link>magnet:?xt=urn:btih:3529d0309b900ec69b5fe4a29167e9354e880d4e&amp;dn=Resident.Evil</link>
		<description>Size: 277.1 MB&lt;br /&gt;Category: Movies</description>
	</item>`

	var it rssItem
	if err := xml.Unmarshal([]byte(xmlData), &it); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	ing := &Ingester{}
	rec := ing.parseRSSItemToRecord(it, "knaben")
	if rec == nil {
		t.Fatalf("expected non-nil record")
	}

	if rec.InfoHash != "3529d0309b900ec69b5fe4a29167e9354e880d4e" {
		t.Errorf("unexpected infohash: %s", rec.InfoHash)
	}

	if rec.Size <= 0 {
		t.Errorf("expected size > 0 from Knaben description, got %d", rec.Size)
	}
	if rec.Size < 290000000 || rec.Size > 291000000 {
		t.Errorf("unexpected size for 277.1 MB: %d", rec.Size)
	}
}

func TestParseRSSItem_Nyaa(t *testing.T) {
	// Nyaa format: <nyaa:size>277.1 MiB</nyaa:size>, <nyaa:seeders>45</nyaa:seeders>
	xmlData := `
	<item>
		<title>[SubsPlease] Frieren - 28 (1080p) [1.4 GiB]</title>
		<link>magnet:?xt=urn:btih:111122223333444455556666777788889999aaaa&amp;dn=Frieren</link>
		<size>1.4 GiB</size>
		<seeders>45</seeders>
		<leechers>5</leechers>
	</item>`

	var it rssItem
	if err := xml.Unmarshal([]byte(xmlData), &it); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	ing := &Ingester{}
	rec := ing.parseRSSItemToRecord(it, "nyaa")
	if rec == nil {
		t.Fatalf("expected non-nil record")
	}

	if rec.Seeders != 45 {
		t.Errorf("expected 45 seeders, got %d", rec.Seeders)
	}
	if rec.Leechers != 5 {
		t.Errorf("expected 5 leechers, got %d", rec.Leechers)
	}
	if rec.Size < 1500000000 || rec.Size > 1510000000 {
		t.Errorf("unexpected size for 1.4 GiB: %d", rec.Size)
	}
}


