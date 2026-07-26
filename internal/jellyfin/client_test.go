package jellyfin

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGetUserIDStrictModeNeverFallsBack(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	client := &JellyfinClient{
		baseURL:       "http://jellyfin.local",
		apiKey:        "secret",
		requireUserID: true,
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return jsonResponse(http.StatusOK, `[]`), nil
		})},
	}

	_, err := client.GetUserID(context.Background())
	if err == nil {
		t.Fatal("mode strict sans identifiant accepté")
	}
	if requests.Load() != 0 {
		t.Fatalf("%d requête(s) réseau effectuée(s) en mode strict", requests.Load())
	}
}

func TestGetUserIDPreservesAdminFallbackByDefault(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	client, err := NewClient(ClientConfig{
		BaseURL: "http://jellyfin.local",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		switch req.URL.Path {
		case "/Users/Me":
			return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		case "/Users":
			return jsonResponse(http.StatusOK, `[
				{"Id":"user-1","Policy":{"IsAdministrator":false}},
				{"Id":"admin-1","Policy":{"IsAdministrator":true}}
			]`), nil
		default:
			t.Fatalf("requête inattendue: %s", req.URL.Path)
			return nil, nil
		}
	})}

	userID, err := client.GetUserID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if userID != "admin-1" {
		t.Fatalf("GetUserID() = %q", userID)
	}
	if requests.Load() != 2 {
		t.Fatalf("requêtes = %d, attendu 2", requests.Load())
	}
}
