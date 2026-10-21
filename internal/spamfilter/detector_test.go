package spamfilter

import (
	"testing"
)

func TestInspect(t *testing.T) {
	cases := []struct {
		name          string
		files         []string
		category      string
		size          int64
		expectSpam    bool
		minScore      int
	}{
		{
			name:       "Avengers.Endgame.2019.1080p.mkv.exe",
			files:      []string{"Avengers.Endgame.2019.1080p.mkv.exe"},
			category:   "Video",
			size:       800000,
			expectSpam: true,
			minScore:   50,
		},
		{
			name:       "Inception.2010.1080p.BluRay.x264-[YTS.AM]",
			files:      []string{"Inception.2010.1080p.mp4", "Inception.2010.1080p.srt"},
			category:   "Video",
			size:       2147483648,
			expectSpam: false,
			minScore:   0,
		},
		{
			name:       "Top.Gun.Maverick.2022.Install_Codec_To_Play",
			files:      []string{"play_movie.url", "password.txt"},
			category:   "Video",
			size:       50000,
			expectSpam: true,
			minScore:   40,
		},
	}

	for _, c := range cases {
		res := Inspect(c.name, c.files, c.category, c.size)
		if res.IsSuspicious != c.expectSpam {
			t.Errorf("For %s: expected IsSuspicious=%v, got %v (score: %d, reasons: %v)", c.name, c.expectSpam, res.IsSuspicious, res.Score, res.Reasons)
		}
		if res.Score < c.minScore {
			t.Errorf("For %s: expected score >= %d, got %d", c.name, c.minScore, res.Score)
		}
	}
}
