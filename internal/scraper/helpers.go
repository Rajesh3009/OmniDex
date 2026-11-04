package scraper

import (
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	// sizeRegex matches numbers with optional decimal point followed by optional unit.
	sizeRegex = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z]+)?\s*$`)

	// magnetBTIHRegex extracts the 32 or 40 character hash from a magnet link xt param.
	magnetBTIHRegex = regexp.MustCompile(`(?i)xt=urn:btih:([a-zA-Z0-9]{32,40})`)

	// hex40Regex matches standard 40-character hexadecimal infohashes.
	hex40Regex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

	// base32Regex matches 32-character RFC 4648 Base32 encoded infohashes.
	base32Regex = regexp.MustCompile(`^[a-zA-Z2-7]{32}$`)
)

// ParseSize parses human-readable or raw size strings into bytes (int64) and a formatted string.
//
// Supported inputs:
//   - Pure integer byte counts (e.g. "23379695704", "5000")
//   - Byte units: "b", "byte", "bytes", "o", "octet", "octets" (multiplier 1)
//   - Binary units: "kib", "mib", "gib", "tib", "pib" (1024^n)
//   - Decimal/standard units: "k", "kb", "m", "mb", "g", "gb", "t", "tb", "p", "pb" (1024^n)
//   - French units: "ko", "mo", "go", "to", "po" (1024^n)
//   - Units with trailing 's': "kbs", "mbs", "gbs", "tbs", "pbs" (1024^n)
//   - Commas and extraneous whitespace are automatically cleaned.
//
// Never panics on empty or corrupt inputs; returns 0, "", and an error.
func ParseSize(raw string) (int64, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, "", fmt.Errorf("empty size input")
	}

	// Remove thousands commas
	cleaned := strings.ReplaceAll(trimmed, ",", "")

	// Fast path: check for pure non-negative integer bytes
	if bytesVal, err := strconv.ParseInt(cleaned, 10, 64); err == nil {
		if bytesVal < 0 {
			return 0, "", fmt.Errorf("negative byte count: %d", bytesVal)
		}
		return bytesVal, formatBytes(bytesVal), nil
	}

	matches := sizeRegex.FindStringSubmatch(cleaned)
	if len(matches) < 2 {
		return 0, "", fmt.Errorf("invalid size format: %q", raw)
	}

	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil || val < 0 || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, "", fmt.Errorf("invalid numeric value in size: %q", matches[1])
	}

	unitStr := ""
	if len(matches) >= 3 {
		unitStr = strings.ToLower(strings.TrimSpace(matches[2]))
	}

	var mult float64
	var canonicalUnit string

	switch unitStr {
	case "", "b", "byte", "bytes", "o", "octet", "octets":
		mult = 1
		canonicalUnit = "B"
	case "k", "kb", "kbs", "ko":
		mult = 1024
		canonicalUnit = "KB"
	case "kib":
		mult = 1024
		canonicalUnit = "KiB"
	case "m", "mb", "mbs", "mo":
		mult = 1024 * 1024
		canonicalUnit = "MB"
	case "mib":
		mult = 1024 * 1024
		canonicalUnit = "MiB"
	case "g", "gb", "gbs", "go":
		mult = 1024 * 1024 * 1024
		canonicalUnit = "GB"
	case "gib":
		mult = 1024 * 1024 * 1024
		canonicalUnit = "GiB"
	case "t", "tb", "tbs", "to":
		mult = 1024 * 1024 * 1024 * 1024
		canonicalUnit = "TB"
	case "tib":
		mult = 1024 * 1024 * 1024 * 1024
		canonicalUnit = "TiB"
	case "p", "pb", "pbs", "po":
		mult = 1024 * 1024 * 1024 * 1024 * 1024
		canonicalUnit = "PB"
	case "pib":
		mult = 1024 * 1024 * 1024 * 1024 * 1024
		canonicalUnit = "PiB"
	default:
		return 0, "", fmt.Errorf("unknown size unit: %q", unitStr)
	}

	totalBytes := int64(math.Round(val * mult))
	if totalBytes < 0 {
		return 0, "", fmt.Errorf("size overflow for input %q", raw)
	}

	var formatted string
	if mult == 1 {
		formatted = fmt.Sprintf("%d B", totalBytes)
	} else {
		formatted = fmt.Sprintf("%.2f %s", val, canonicalUnit)
	}

	return totalBytes, formatted, nil
}

// formatBytes returns a standardized human-readable string representation of raw bytes.
func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}

// ParseIntDefensive cleanly extracts an integer from a raw string.
//
// Features:
//   - Trims whitespace, commas, and leading '+' signs.
//   - Identifies the TorrentDownloads "16777215" (2^24 - 1) unknown swarm sentinel and returns 0.
//   - Returns defaultVal for empty strings, non-numeric values ("-", "N/A", "unknown"), or parse failures.
func ParseIntDefensive(raw string, defaultVal int) int {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimPrefix(cleaned, "+")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		return defaultVal
	}

	// 16777215 (2^24 - 1) is TorrentDownloads sentinel for unknown swarm size
	if cleaned == "16777215" {
		return 0
	}

	val, err := strconv.Atoi(cleaned)
	if err != nil {
		return defaultVal
	}
	return val
}

// NormalizeInfoHash converts raw hash inputs, base32 strings, or magnet links into a
// 40-character lowercase hexadecimal infohash.
//
// Rules:
//   - 40-character hex strings are normalized to lowercase.
//   - 32-character RFC 4648 Base32 strings are decoded and converted to 40 lowercase hex characters.
//   - Magnet URIs with `xt=urn:btih:...` have their hash extracted and normalized.
//   - All-zero hashes ("0000000000000000000000000000000000000000") are rejected as invalid (returns "").
//   - Malformed, empty, or unparseable inputs return "".
//   - Never panics.
func NormalizeInfoHash(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	// Check if this is a magnet link
	if strings.HasPrefix(strings.ToLower(trimmed), "magnet:") {
		// Attempt URL unescaping first in case parameters were encoded
		unescaped, err := url.QueryUnescape(trimmed)
		if err != nil {
			unescaped = trimmed
		}
		if match := magnetBTIHRegex.FindStringSubmatch(unescaped); len(match) >= 2 {
			return NormalizeInfoHash(match[1])
		}
		return ""
	}

	// Case 1: 40-character hex string
	if len(trimmed) == 40 && hex40Regex.MatchString(trimmed) {
		lower := strings.ToLower(trimmed)
		if lower == "0000000000000000000000000000000000000000" {
			return ""
		}
		return lower
	}

	// Case 2: 32-character Base32 encoded infohash
	if len(trimmed) == 32 && base32Regex.MatchString(trimmed) {
		upper := strings.ToUpper(trimmed)
		decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(upper)
		if err == nil && len(decoded) == 20 {
			lower := hex.EncodeToString(decoded)
			if lower == "0000000000000000000000000000000000000000" {
				return ""
			}
			return lower
		}
	}

	return ""
}

// BuildMagnetURI constructs a standard RFC 3986 BitTorrent magnet URI:
// `magnet:?xt=urn:btih:<hash>&dn=<encoded_name>&tr=<encoded_tracker1>&tr=...`
//
// Rules:
//   - InfoHash is normalized to lowercase hex. If normalization fails, returns "".
//   - Display name is trimmed and URL query escaped.
//   - If trackers slice is empty, defaults to PublicTrackers.
//   - Trackers are trimmed, deduplicated, and URL query escaped.
func BuildMagnetURI(infohash, name string, trackers []string) string {
	hash := NormalizeInfoHash(infohash)
	if hash == "" {
		// Fallback: if not valid via full normalize, try cleaning lowercase hex
		cleaned := strings.ToLower(strings.TrimSpace(infohash))
		if len(cleaned) == 40 && hex40Regex.MatchString(cleaned) && cleaned != "0000000000000000000000000000000000000000" {
			hash = cleaned
		} else {
			return ""
		}
	}

	if len(trackers) == 0 {
		trackers = PublicTrackers
	}

	var sb strings.Builder
	sb.WriteString("magnet:?xt=urn:btih:")
	sb.WriteString(hash)

	trimmedName := strings.TrimSpace(name)
	if trimmedName != "" {
		sb.WriteString("&dn=")
		sb.WriteString(url.QueryEscape(trimmedName))
	}

	seen := make(map[string]bool, len(trackers))
	for _, tr := range trackers {
		trTrimmed := strings.TrimSpace(tr)
		if trTrimmed == "" || seen[trTrimmed] {
			continue
		}
		seen[trTrimmed] = true
		sb.WriteString("&tr=")
		sb.WriteString(url.QueryEscape(trTrimmed))
	}

	return sb.String()
}
