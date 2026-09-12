package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testAPIKey = "test-key-0123456789abcdef"

func newTestClient(t *testing.T, handler http.HandlerFunc) *JellyfinClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &JellyfinClient{
		baseURL:    srv.URL,
		apiKey:     testAPIKey,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func assertAuthHeaders(t *testing.T, h http.Header) {
	t.Helper()
	want := `MediaBrowser Token="` + testAPIKey + `"`
	if got := h.Get("Authorization"); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got := h.Get("X-MediaBrowser-Token"); got != "" {
		t.Errorf("legacy X-MediaBrowser-Token header sent: %q", got)
	}
}

func TestDoRequestAuthHeader(t *testing.T) {
	var got http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	})

	var dest map[string]any
	if err := c.Get(context.Background(), "/System/Info", nil, &dest); err != nil {
		t.Fatalf("Get: %v", err)
	}
	assertAuthHeaders(t, got)
}

func TestPostRawAuthHeader(t *testing.T) {
	var got http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.PostRaw(context.Background(), "/Items/abc/Images/Primary", nil, []byte("img"), "image/png"); err != nil {
		t.Fatalf("PostRaw: %v", err)
	}
	assertAuthHeaders(t, got)
}
