package metadata

import (
	"regexp"
	"strings"
)

type Tags struct {
	Resolution   string `json:"resolution,omitempty"`    // e.g. "4K", "1080p", "720p", "480p"
	Codec        string `json:"codec,omitempty"`         // e.g. "x265", "x264", "AV1", "HEVC", "XviD"
	Quality      string `json:"quality,omitempty"`       // e.g. "BluRay", "WEB-DL", "WEBRip", "HDTV", "CAM"
	Audio        string `json:"audio,omitempty"`         // e.g. "Dolby Atmos", "DTS-HD", "DTS", "TrueHD", "DD5.1", "AAC", "FLAC", "MP3"
	HDR          string `json:"hdr,omitempty"`           // e.g. "HDR10+", "HDR10", "HDR", "Dolby Vision"
	ReleaseGroup string `json:"release_group,omitempty"` // e.g. "YTS", "TGx", "RARBG", "PSA", "NTb", "FLUX"
}

var (
	// Resolution patterns
	res4kReg    = regexp.MustCompile(`(?i)\b(2160p|4k|uhd)\b`)
	res1080pReg = regexp.MustCompile(`(?i)\b(1080p|1080i|fhd)\b`)
	res720pReg  = regexp.MustCompile(`(?i)\b(720p|hd)\b`)
	res480pReg  = regexp.MustCompile(`(?i)\b(480p|576p|sd)\b`)

	// Codec patterns
	codecAV1Reg  = regexp.MustCompile(`(?i)\b(av1)\b`)
	codecX265Reg = regexp.MustCompile(`(?i)\b(x265|h265|hevc|h\.265)\b`)
	codecX264Reg = regexp.MustCompile(`(?i)\b(x264|h264|avc|h\.264)\b`)
	codecXvidReg = regexp.MustCompile(`(?i)\b(xvid|divx)\b`)

	// HDR patterns
	hdrDVReg   = regexp.MustCompile(`(?i)\b(dovi|dv|dolby\s*vision)\b`)
	hdr10pReg  = regexp.MustCompile(`(?i)\b(hdr10\+|hdr10plus)(?:[^a-zA-Z0-9]|$)`)
	hdr10Reg   = regexp.MustCompile(`(?i)\b(hdr10|hdr)\b`)

	// Quality / Source patterns
	qualRemuxReg  = regexp.MustCompile(`(?i)\b(remux)\b`)
	qualBluRayReg = regexp.MustCompile(`(?i)\b(bluray|bdrip|brrip)\b`)
	qualWebDLReg  = regexp.MustCompile(`(?i)\b(web-dl|webdl)\b`)
	qualWebRipReg = regexp.MustCompile(`(?i)\b(webrip|web)\b`)
	qualHDTVReg   = regexp.MustCompile(`(?i)\b(hdtv|pdtv|dsr)\b`)
	qualCamReg    = regexp.MustCompile(`(?i)\b(cam|camrip|ts|telesync|hdts|hdcam)\b`)

	// Audio patterns
	audioAtmosReg  = regexp.MustCompile(`(?i)\b(atmos|dolby\s*atmos)\b`)
	audioTrueHDReg = regexp.MustCompile(`(?i)\b(truehd)\b`)
	audioDTSHDReg  = regexp.MustCompile(`(?i)\b(dts-hd|dts-hd\s*ma)\b`)
	audioDTSReg    = regexp.MustCompile(`(?i)\b(dts)\b`)
	audioDD51Reg   = regexp.MustCompile(`(?i)\b(ddp?5\.1|ac3|eac3|dd\+|5\.1ch?)\b`)
	audioFLACReg   = regexp.MustCompile(`(?i)\b(flac)\b`)
	audioAACReg    = regexp.MustCompile(`(?i)\b(aac(\s*2\.0)?)\b`)
	audioMP3Reg    = regexp.MustCompile(`(?i)\b(mp3)\b`)

	// Release groups (often after trailing hyphen or brackets)
	groupReg = regexp.MustCompile(`(?i)[-\[]\s*([a-zA-Z0-9]{2,15})(?:\]|\.[a-zA-Z0-9]{2,4}|$)$`)
	knownGroups = map[string]string{
		"yts": "YTS", "yify": "YIFY", "tgx": "TGx", "rarbg": "RARBG",
		"psa": "PSA", "ntb": "NTb", "flux": "FLUX", "galaxyrg": "GalaxyRG",
		"qxr": "QxR", "ettv": "ETTV", "eztv": "EZTV", "sparks": "SPARKS",
		"dimension": "DIMENSION", "geckos": "GECKOS", "fleet": "FLEET",
		"ion10": "ION10", "batv": "BAJS", "megusta": "MeGusta",
	}
)

// ExtractTags parses a torrent name and optional file paths into structured metadata tags.
func ExtractTags(name string, files []string) Tags {
	fullText := name
	for _, f := range files {
		fullText += " " + f
	}

	var tags Tags

	// 1. Resolution
	if res4kReg.MatchString(fullText) {
		tags.Resolution = "4K"
	} else if res1080pReg.MatchString(fullText) {
		tags.Resolution = "1080p"
	} else if res720pReg.MatchString(fullText) {
		tags.Resolution = "720p"
	} else if res480pReg.MatchString(fullText) {
		tags.Resolution = "480p"
	}

	// 2. Codec
	if codecAV1Reg.MatchString(fullText) {
		tags.Codec = "AV1"
	} else if codecX265Reg.MatchString(fullText) {
		tags.Codec = "x265"
	} else if codecX264Reg.MatchString(fullText) {
		tags.Codec = "x264"
	} else if codecXvidReg.MatchString(fullText) {
		tags.Codec = "XviD"
	}

	// 3. HDR
	if hdrDVReg.MatchString(fullText) {
		tags.HDR = "Dolby Vision"
	} else if hdr10pReg.MatchString(fullText) {
		tags.HDR = "HDR10+"
	} else if hdr10Reg.MatchString(fullText) {
		tags.HDR = "HDR"
	}

	// 4. Quality / Source
	if qualRemuxReg.MatchString(fullText) {
		tags.Quality = "REMUX"
	} else if qualBluRayReg.MatchString(fullText) {
		tags.Quality = "BluRay"
	} else if qualWebDLReg.MatchString(fullText) {
		tags.Quality = "WEB-DL"
	} else if qualWebRipReg.MatchString(fullText) {
		tags.Quality = "WEBRip"
	} else if qualHDTVReg.MatchString(fullText) {
		tags.Quality = "HDTV"
	} else if qualCamReg.MatchString(fullText) {
		tags.Quality = "CAM"
	}

	// 5. Audio
	if audioAtmosReg.MatchString(fullText) {
		tags.Audio = "Atmos"
	} else if audioTrueHDReg.MatchString(fullText) {
		tags.Audio = "TrueHD"
	} else if audioDTSHDReg.MatchString(fullText) {
		tags.Audio = "DTS-HD"
	} else if audioDTSReg.MatchString(fullText) {
		tags.Audio = "DTS"
	} else if audioDD51Reg.MatchString(fullText) {
		tags.Audio = "5.1 / AC3"
	} else if audioFLACReg.MatchString(fullText) {
		tags.Audio = "FLAC"
	} else if audioAACReg.MatchString(fullText) {
		tags.Audio = "AAC"
	} else if audioMP3Reg.MatchString(fullText) {
		tags.Audio = "MP3"
	}

	// 6. Release Group (detect from title)
	nameClean := strings.TrimSpace(name)
	// Check known groups first
	nameLower := strings.ToLower(nameClean)
	for k, v := range knownGroups {
		pattern := `(?i)(?:^|[-. _\[(])` + regexp.QuoteMeta(k) + `(?:$|[-. _\])])`
		if matched, _ := regexp.MatchString(pattern, nameLower); matched {
			tags.ReleaseGroup = v
			break
		}
	}
	if tags.ReleaseGroup == "" {
		if m := groupReg.FindStringSubmatch(nameClean); len(m) > 1 {
			candidate := strings.TrimSpace(m[1])
			if len(candidate) >= 2 && len(candidate) <= 15 {
				tags.ReleaseGroup = candidate
			}
		}
	}

	return tags
}
