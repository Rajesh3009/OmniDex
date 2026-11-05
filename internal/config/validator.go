package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateOutboundURL validates that a URL is safe for outbound requests,
// preventing SSRF against internal/cloud metadata services and blocking unsafe schemes.
func ValidateOutboundURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme '%s' is not allowed; only http and https are permitted", scheme)
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return fmt.Errorf("URL must include a valid host")
	}

	// Prohibit access to AWS/GCP/Azure/DigitalOcean cloud instance metadata endpoints
	if hostname == "169.254.169.254" || hostname == "metadata.google.internal" || strings.HasPrefix(hostname, "169.254.") {
		return fmt.Errorf("access to cloud metadata endpoints is prohibited")
	}

	// Check if hostname is an IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("access to link-local/unspecified IP addresses is prohibited")
		}
	}

	return nil
}
