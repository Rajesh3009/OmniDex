package config

import (
	"testing"
)

func TestValidateOutboundURL(t *testing.T) {
	tests := []struct {
		url       string
		shouldErr bool
		reason    string
	}{
		{"", true, "empty URL"},
		{"   ", true, "whitespace URL"},
		{"ftp://example.com/rss", true, "ftp scheme"},
		{"file:///etc/passwd", true, "file scheme"},
		{"gopher://example.com", true, "gopher scheme"},
		{"http://169.254.169.254/latest/meta-data/", true, "AWS/cloud metadata IPv4"},
		{"http://169.254.1.1/test", true, "link-local IPv4"},
		{"http://metadata.google.internal/computeMetadata/v1/", true, "GCP metadata"},
		{"https://nyaa.si/?page=rss", false, "legitimate HTTPS feed"},
		{"http://example.com/feed.xml", false, "legitimate HTTP feed"},
		{"http://localhost:9696/api/v1/search", false, "local Prowlarr instance"},
		{"http://127.0.0.1:8686/rss", false, "local indexer feed"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			err := ValidateOutboundURL(tt.url)
			if (err != nil) != tt.shouldErr {
				t.Errorf("ValidateOutboundURL(%q) err = %v, shouldErr = %v", tt.url, err, tt.shouldErr)
			}
		})
	}
}
