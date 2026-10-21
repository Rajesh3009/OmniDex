package spamfilter

import (
	"path/filepath"
	"regexp"
	"strings"
)

// SpamCheckResult holds the analysis verdict.
type SpamCheckResult struct {
	IsSuspicious bool     `json:"is_suspicious"`
	Score        int      `json:"score"`         // 0 to 100
	Reasons      []string `json:"reasons"`       // e.g. ["Executable disguised as video", "Password-locked archive"]
}

var (
	// Executable and script extensions
	dangerousExts = map[string]bool{
		".exe": true, ".bat": true, ".cmd": true, ".vbs": true, ".js": true,
		".scr": true, ".pif": true, ".wsf": true, ".ps1": true, ".msi": true,
		".cpl": true, ".hta": true, ".jar": true,
	}

	// Archive extensions
	archiveExts = map[string]bool{
		".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	}

	// Media video/audio extensions
	mediaExts = map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
		".flv": true, ".webm": true, ".m4v": true, ".ts": true, ".mp3": true,
		".flac": true, ".wav": true, ".aac": true,
	}

	// Double extensions commonly used in trojans (e.g., .mp4.exe, .mkv.scr)
	doubleExtReg = regexp.MustCompile(`(?i)\.(mp4|mkv|avi|mov|mp3|pdf|jpg|png)\.(exe|scr|bat|cmd|vbs|js|pif|msi)$`)

	// Suspicious fake codec / password prompts in names
	fakeCodecReg = regexp.MustCompile(`(?i)\b(requires?\s+codec|install\s+codec|codec\s+pack|password\s+required|password\s+in\s+description|pass\s+in\s+txt|open\s+pass\.txt|see\s+password\.txt|unlock\s+password|passkey\.txt)\b`)
	fakeSiteReg  = regexp.MustCompile(`(?i)\b(free-codecs?\.com|codec-update\.com|videoplayer-update\.com)\b`)
	scamShortURL = regexp.MustCompile(`(?i)\b(bit\.ly|tinyurl\.com|t\.co|cutt\.ly|is\.gd|goo\.gl)\b`)
)

// Inspect inspects the torrent title, file list, category, and size to score suspiciousness.
func Inspect(name string, files []string, category string, totalSize int64) SpamCheckResult {
	var reasons []string
	score := 0

	lowerName := strings.ToLower(strings.TrimSpace(name))

	// 1. Double extension check in title or any file
	if doubleExtReg.MatchString(lowerName) {
		score += 60
		reasons = append(reasons, "Dangerous double file extension in title (e.g. .mp4.exe)")
	}

	for _, f := range files {
		fLower := strings.ToLower(f)
		if doubleExtReg.MatchString(fLower) {
			score += 60
			reasons = append(reasons, "Dangerous disguised executable file ("+filepath.Base(f)+")")
			break
		}
	}

	// 2. Executable masquerading as Video/Audio/Books
	if category == "Video" || category == "Audio" || category == "Books" {
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f))
			if dangerousExts[ext] {
				score += 50
				reasons = append(reasons, "Contains executable ("+filepath.Base(f)+") in a media/book category")
				break
			}
		}
	}

	// 3. Fake codec / password txt patterns in name or files
	if fakeCodecReg.MatchString(lowerName) {
		score += 45
		reasons = append(reasons, "Title references required codec download or password text file")
	}

	for _, f := range files {
		fLower := strings.ToLower(filepath.Base(f))
		if fakeCodecReg.MatchString(fLower) {
			score += 40
			reasons = append(reasons, "File references required codec/password ("+fLower+")")
			break
		}
		if fLower == "password.txt" || fLower == "pass.txt" || fLower == "how_to_unlock.txt" {
			score += 40
			reasons = append(reasons, "Contains locked password instructional file ("+fLower+")")
			break
		}
	}

	// 4. Known scam / redirect URLs in file names
	if scamShortURL.MatchString(lowerName) {
		score += 30
		reasons = append(reasons, "Title contains shortened URL redirection")
	}

	// 5. Video categorized torrent containing only small archive or text files and NO video stream
	if category == "Video" && len(files) > 0 && totalSize < 10*1024*1024 { // Less than 10MB video torrent
		hasVideoFile := false
		for _, f := range files {
			ext := strings.ToLower(filepath.Ext(f))
			if mediaExts[ext] {
				hasVideoFile = true
				break
			}
		}
		if !hasVideoFile {
			score += 40
			reasons = append(reasons, "Video torrent is abnormally tiny (<10MB) with no video files")
		}
	}

	// Cap score at 100
	if score > 100 {
		score = 100
	}

	isSuspicious := score >= 40

	return SpamCheckResult{
		IsSuspicious: isSuspicious,
		Score:        score,
		Reasons:      reasons,
	}
}
