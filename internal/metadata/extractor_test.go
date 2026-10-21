package metadata

import (
	"testing"
)

func TestExtractTags(t *testing.T) {
	cases := []struct {
		name         string
		files        []string
		expectedRes  string
		expectedCodec string
		expectedHDR  string
		expectedQual string
		expectedAud  string
		expectedGrp  string
	}{
		{
			name:         "Dune.Part.Two.2024.2160p.UHD.HDR10+.BluRay.x265.TrueHD.Atmos.7.1-FLUX",
			expectedRes:  "4K",
			expectedCodec: "x265",
			expectedHDR:  "HDR10+",
			expectedQual: "BluRay",
			expectedAud:  "Atmos",
			expectedGrp:  "FLUX",
		},
		{
			name:         "The.Matrix.1999.1080p.BluRay.x264.DTS-HD.MA-RARBG.mp4",
			expectedRes:  "1080p",
			expectedCodec: "x264",
			expectedQual: "BluRay",
			expectedAud:  "DTS-HD",
			expectedGrp:  "RARBG",
		},
		{
			name:         "Oppenheimer.2023.720p.WEBRip.x264.AAC-[YTS.MX]",
			expectedRes:  "720p",
			expectedCodec: "x264",
			expectedQual: "WEBRip",
			expectedAud:  "AAC",
			expectedGrp:  "YTS",
		},
		{
			name:         "Interstellar.2014.2160p.DoVi.WEB-DL.AV1.Opus-TGx",
			expectedRes:  "4K",
			expectedCodec: "AV1",
			expectedHDR:  "Dolby Vision",
			expectedQual: "WEB-DL",
			expectedGrp:  "TGx",
		},
	}

	for _, c := range cases {
		tags := ExtractTags(c.name, c.files)
		if c.expectedRes != "" && tags.Resolution != c.expectedRes {
			t.Errorf("For %s: expected resolution %s, got %s", c.name, c.expectedRes, tags.Resolution)
		}
		if c.expectedCodec != "" && tags.Codec != c.expectedCodec {
			t.Errorf("For %s: expected codec %s, got %s", c.name, c.expectedCodec, tags.Codec)
		}
		if c.expectedHDR != "" && tags.HDR != c.expectedHDR {
			t.Errorf("For %s: expected HDR %s, got %s", c.name, c.expectedHDR, tags.HDR)
		}
		if c.expectedQual != "" && tags.Quality != c.expectedQual {
			t.Errorf("For %s: expected quality %s, got %s", c.name, c.expectedQual, tags.Quality)
		}
		if c.expectedAud != "" && tags.Audio != c.expectedAud {
			t.Errorf("For %s: expected audio %s, got %s", c.name, c.expectedAud, tags.Audio)
		}
		if c.expectedGrp != "" && tags.ReleaseGroup != c.expectedGrp {
			t.Errorf("For %s: expected group %s, got %s", c.name, c.expectedGrp, tags.ReleaseGroup)
		}
	}
}
