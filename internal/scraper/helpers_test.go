package scraper

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestParseSize_ValidUnits(t *testing.T) {
	tests := []struct {
		input             string
		expectedBytes     int64
		expectedFormatted string
	}{
		// Bytes
		{"100 B", 100, "100 B"},
		{"5000 bytes", 5000, "5000 B"},
		{"0 Bytes", 0, "0 B"},
		{"12 o", 12, "12 B"},
		{"1 octet", 1, "1 B"},
		{"42 octets", 42, "42 B"},

		// Binary units (1024^n)
		{"1.5 GiB", 1610612736, "1.50 GiB"},
		{"700.5 MiB", 734527488, "700.50 MiB"},
		{"512 KiB", 524288, "512.00 KiB"},
		{"2 TiB", 2199023255552, "2.00 TiB"},

		// Decimal / standard units
		{"100 KB", 102400, "100.00 KB"},
		{"50 MB", 52428800, "50.00 MB"},
		{"8.42 GB", 9040906158, "8.42 GB"},
		{"1 TB", 1099511627776, "1.00 TB"},

		// French units
		{"500 Ko", 512000, "500.00 KB"},
		{"750 Mo", 786432000, "750.00 MB"},
		{"2.5 Go", 2684354560, "2.50 GB"},
		{"1 To", 1099511627776, "1.00 TB"},

		// Units with trailing 's'
		{"686.36 MBs", 719700623, "686.36 MB"},
		{"10 KBs", 10240, "10.00 KB"},
		{"4.2 GBs", 4509715661, "4.20 GB"},
		{"1.1 TBs", 1209462790554, "1.10 TB"},

		// Commas and extra spaces
		{"1,024.50 MB", 1074266112, "1024.50 MB"},
		{"   700.5   MiB  ", 734527488, "700.50 MiB"},
		{"2,337,969 B", 2337969, "2337969 B"},

		// Pure integer byte strings
		{"23379695704", 23379695704, "21.77 GB"},
		{"0", 0, "0 B"},
		{"874575299", 874575299, "834.06 MB"},
		{"1621350154", 1621350154, "1.51 GB"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			bytesVal, formatted, err := ParseSize(tc.input)
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tc.input, err)
			}
			if bytesVal != tc.expectedBytes {
				t.Errorf("bytes mismatch for %q: expected %d, got %d", tc.input, tc.expectedBytes, bytesVal)
			}
			if formatted != tc.expectedFormatted {
				t.Errorf("formatted mismatch for %q: expected %q, got %q", tc.input, tc.expectedFormatted, formatted)
			}
		})
	}
}

func TestParseSize_ErrorsAndEdgeCases(t *testing.T) {
	invalidInputs := []string{
		"",
		"   ",
		"unknown",
		"-5 MB",
		"-1024",
		"abc",
		"NaN GiB",
		"Inf MiB",
		"!@#$%^",
		"100 FooBar",
	}

	for _, input := range invalidInputs {
		t.Run("invalid_"+input, func(t *testing.T) {
			bytesVal, formatted, err := ParseSize(input)
			if err == nil {
				t.Errorf("expected error for invalid input %q, but got bytes=%d formatted=%q", input, bytesVal, formatted)
			}
			if bytesVal != 0 || formatted != "" {
				t.Errorf("expected 0 and empty string on error, got %d and %q", bytesVal, formatted)
			}
		})
	}
}

func TestParseIntDefensive(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultVal int
		expected   int
	}{
		{"standard positive", "42", 0, 42},
		{"zero", "0", 10, 0},
		{"with spaces", "  123  ", 0, 123},
		{"with commas", "1,234,567", 0, 1234567},
		{"with plus sign", "+99", 0, 99},
		{"with plus and spaces", "  +50  ", 0, 50},
		{"TorrentDownloads sentinel 16777215", "16777215", 99, 0},
		{"TorrentDownloads sentinel with spaces", " 16777215 ", 99, 0},
		{"empty string", "", 5, 5},
		{"spaces only", "   ", 10, 10},
		{"dash placeholder", "-", 0, 0},
		{"N/A placeholder", "N/A", 0, 0},
		{"none placeholder", "none", 0, 0},
		{"unknown placeholder", "unknown", 42, 42},
		{"negative number", "-15", 0, -15},
		{"garbage string", "!@#abc", 7, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseIntDefensive(tc.input, tc.defaultVal)
			if got != tc.expected {
				t.Errorf("ParseIntDefensive(%q, %d) = %d; want %d", tc.input, tc.defaultVal, got, tc.expected)
			}
		})
	}
}

func TestNormalizeInfoHash(t *testing.T) {
	// Generate valid 20-byte test hash
	rawBytes := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67}
	expectedHex := hex.EncodeToString(rawBytes) // "0123456789abcdef0123456789abcdef01234567"
	base32Encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawBytes)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase 40 hex", expectedHex, expectedHex},
		{"uppercase 40 hex", strings.ToUpper(expectedHex), expectedHex},
		{"mixed case 40 hex", "0123456789AbCdEf0123456789aBcDeF01234567", expectedHex},
		{"with spaces", "  " + expectedHex + "  ", expectedHex},
		{"base32 uppercase", base32Encoded, expectedHex},
		{"base32 lowercase", strings.ToLower(base32Encoded), expectedHex},
		{"magnet link with 40 hex", "magnet:?xt=urn:btih:" + expectedHex + "&dn=Test+Torrent", expectedHex},
		{"magnet link uppercase hex", "magnet:?xt=urn:btih:" + strings.ToUpper(expectedHex) + "&dn=Test", expectedHex},
		{"magnet link with base32", "magnet:?xt=urn:btih:" + base32Encoded + "&dn=Test", expectedHex},
		{"magnet link url-encoded", "magnet:?xt=urn%3Abtih%3A" + expectedHex + "&dn=Test", expectedHex},
		{"all zeros 40 hex rejected", "0000000000000000000000000000000000000000", ""},
		{"all zeros base32 rejected", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ""},
		{"empty string", "", ""},
		{"spaces only", "   ", ""},
		{"too short hex", "0123456789abcdef", ""},
		{"too long hex", expectedHex + "00", ""},
		{"invalid characters", "0123456789abcdef0123456789abcdef0123456g", ""},
		{"malformed magnet", "magnet:?foo=bar", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeInfoHash(tc.input)
			if got != tc.expected {
				t.Errorf("NormalizeInfoHash(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestBuildMagnetURI(t *testing.T) {
	testHash := "0123456789abcdef0123456789abcdef01234567"
	testName := "The Big Buck Bunny (2008) 1080p"

	t.Run("default public trackers", func(t *testing.T) {
		uri := BuildMagnetURI(testHash, testName, nil)
		if !strings.HasPrefix(uri, "magnet:?xt=urn:btih:"+testHash) {
			t.Fatalf("expected prefix with hash, got: %s", uri)
		}
		if !strings.Contains(uri, "&dn="+url.QueryEscape(testName)) {
			t.Errorf("expected uri to contain encoded dn, got: %s", uri)
		}
		for _, tr := range PublicTrackers {
			if !strings.Contains(uri, "&tr="+url.QueryEscape(tr)) {
				t.Errorf("expected tracker %s in uri, got: %s", tr, uri)
			}
		}
	})

	t.Run("custom trackers and deduplication", func(t *testing.T) {
		customTrackers := []string{
			"http://nyaa.tracker.wf:7777/announce",
			"udp://tracker.opentrackr.org:1337/announce",
			"http://nyaa.tracker.wf:7777/announce", // duplicate
			"",                                    // empty should be skipped
		}
		uri := BuildMagnetURI(strings.ToUpper(testHash), testName, customTrackers)

		if !strings.HasPrefix(uri, "magnet:?xt=urn:btih:"+testHash) {
			t.Errorf("expected lowercased hash in uri, got: %s", uri)
		}

		// Count occurrences of nyaa tracker
		expectedTr := "&tr=" + url.QueryEscape("http://nyaa.tracker.wf:7777/announce")
		count := strings.Count(uri, expectedTr)
		if count != 1 {
			t.Errorf("expected tracker to appear exactly once, appeared %d times", count)
		}
	})

	t.Run("base32 infohash input", func(t *testing.T) {
		rawBytes := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67}
		base32Hash := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawBytes)

		uri := BuildMagnetURI(base32Hash, "Test Base32", []string{"udp://tracker.example.com:1337/announce"})
		if !strings.HasPrefix(uri, "magnet:?xt=urn:btih:"+testHash) {
			t.Errorf("expected base32 to be converted to hex in uri, got: %s", uri)
		}
	})

	t.Run("empty or invalid hash returns empty string", func(t *testing.T) {
		if uri := BuildMagnetURI("", "Test", nil); uri != "" {
			t.Errorf("expected empty string for empty hash, got %q", uri)
		}
		if uri := BuildMagnetURI("not-valid-hash", "Test", nil); uri != "" {
			t.Errorf("expected empty string for invalid hash, got %q", uri)
		}
		if uri := BuildMagnetURI("0000000000000000000000000000000000000000", "Test", nil); uri != "" {
			t.Errorf("expected empty string for all-zero hash, got %q", uri)
		}
	})
}

func TestTorrentFormattedSize(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var tor *Torrent
		if got := tor.FormattedSize(); got != "0 B" {
			t.Errorf("expected '0 B' for nil torrent, got %q", got)
		}
	})

	t.Run("pre-populated SizeFormatted", func(t *testing.T) {
		tor := &Torrent{
			Size:          1000000,
			SizeFormatted: "1.23 Custom MB",
		}
		if got := tor.FormattedSize(); got != "1.23 Custom MB" {
			t.Errorf("expected pre-populated string, got %q", got)
		}
	})

	t.Run("zero or negative size", func(t *testing.T) {
		tor1 := &Torrent{Size: 0}
		tor2 := &Torrent{Size: -500}
		if got := tor1.FormattedSize(); got != "0 B" {
			t.Errorf("expected '0 B' for zero size, got %q", got)
		}
		if got := tor2.FormattedSize(); got != "0 B" {
			t.Errorf("expected '0 B' for negative size, got %q", got)
		}
	})

	t.Run("various byte sizes", func(t *testing.T) {
		tests := []struct {
			size     int64
			expected string
		}{
			{500, "500 B"},
			{1024, "1.00 KB"},
			{1536, "1.50 KB"},
			{1048576, "1.00 MB"},
			{1073741824, "1.00 GB"},
			{1610612736, "1.50 GB"},
			{1099511627776, "1.00 TB"},
		}

		for _, tc := range tests {
			tor := &Torrent{Size: tc.size}
			if got := tor.FormattedSize(); got != tc.expected {
				t.Errorf("FormattedSize for %d = %q; want %q", tc.size, got, tc.expected)
			}
		}
	})
}

func TestOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.Limit != 50 || opts.Page != 1 || opts.Timeout == 0 || opts.HTTPClient == nil {
		t.Fatalf("DefaultOptions returned unexpected defaults: %+v", opts)
	}

	WithBaseURL("https://example.com/api/")(&opts)
	if opts.BaseURL != "https://example.com/api" {
		t.Errorf("WithBaseURL did not trim trailing slash: %q", opts.BaseURL)
	}

	WithLimit(100)(&opts)
	if opts.Limit != 100 {
		t.Errorf("WithLimit failed: %d", opts.Limit)
	}

	WithPage(3)(&opts)
	if opts.Page != 3 {
		t.Errorf("WithPage failed: %d", opts.Page)
	}

	WithCategory("Movies/HD")(&opts)
	if opts.Category != "Movies/HD" {
		t.Errorf("WithCategory failed: %q", opts.Category)
	}
}
