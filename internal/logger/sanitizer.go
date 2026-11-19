package logger

import (
	"regexp"
	"strings"
	"sync"
)

var (
	apiKeyParamRegex = regexp.MustCompile(`(?i)(apikey|api_key|token|secret)=([a-zA-Z0-9_-]{8,})`)
)

// Sanitizer redacts sensitive API keys and secrets from log strings.
type Sanitizer struct {
	mu      sync.RWMutex
	secrets []string
}

// NewSanitizer creates a new Sanitizer.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{
		secrets: make([]string, 0),
	}
}

// RegisterSecret adds a secret token to be redacted if encountered.
func (s *Sanitizer) RegisterSecret(secret string) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 5 {
		return // Ignore trivially short strings to avoid masking common words
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.secrets {
		if existing == secret {
			return
		}
	}
	s.secrets = append(s.secrets, secret)
}

// Sanitize replaces registered secrets and matching regex patterns with [REDACTED].
func (s *Sanitizer) Sanitize(input string) string {
	if input == "" {
		return input
	}

	s.mu.RLock()
	secretsList := make([]string, len(s.secrets))
	copy(secretsList, s.secrets)
	s.mu.RUnlock()

	result := input
	for _, sec := range secretsList {
		result = strings.ReplaceAll(result, sec, "[REDACTED]")
	}

	result = apiKeyParamRegex.ReplaceAllString(result, "${1}=[REDACTED]")
	return result
}
