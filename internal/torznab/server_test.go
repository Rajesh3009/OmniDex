package torznab

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTorznabAuthentication(t *testing.T) {
	srv := NewServer(nil, "super-secret-key-12345", "http://localhost:8686")

	tests := []struct {
		name       string
		apiKey     string
		expectCode int
		expectErr  bool
	}{
		{"Valid query apikey", "super-secret-key-12345", http.StatusOK, false},
		{"Invalid query apikey", "wrong-key", http.StatusOK, true},
		{"Empty query apikey", "", http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/torznab?t=caps&apikey="+tt.apiKey, nil)
			w := httptest.NewRecorder()

			srv.HandleTorznab(w, req)

			body := w.Body.String()
			if tt.expectErr {
				if !strings.Contains(body, `error code="100"`) {
					t.Errorf("expected error code 100 in response, got: %s", body)
				}
			} else {
				if strings.Contains(body, `<error code=`) {
					t.Errorf("unexpected error in response: %s", body)
				}
				if !strings.Contains(body, `<caps>`) {
					t.Errorf("expected <caps> in response, got: %s", body)
				}
			}
		})
	}
}
