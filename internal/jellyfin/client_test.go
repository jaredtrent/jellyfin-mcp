package jellyfin

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
		t.Fatal("strict mode accepted a missing user ID")
	}
	if requests.Load() != 0 {
		t.Fatalf("%d network request(s) made in strict mode", requests.Load())
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
			t.Fatalf("unexpected request: %s", req.URL.Path)
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
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

type generatedBody struct {
	remaining int64
	read      int64
}

func (body *generatedBody) Read(dst []byte) (int, error) {
	if body.remaining == 0 {
		return 0, io.EOF
	}
	size := int64(len(dst))
	if size > body.remaining {
		size = body.remaining
	}
	for i := int64(0); i < size; i++ {
		dst[i] = 'x'
	}
	body.remaining -= size
	body.read += size
	return int(size), nil
}

func (*generatedBody) Close() error {
	return nil
}

func TestPostRawBoundsErrorResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int64
	}{
		{name: "at the limit", size: MaxResponseBodyBytes},
		{name: "above the limit", size: MaxResponseBodyBytes + 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &generatedBody{remaining: tt.size}
			client, err := NewClient(ClientConfig{
				BaseURL: "http://jellyfin.local",
				APIKey:  "secret",
			})
			if err != nil {
				t.Fatal(err)
			}
			client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     make(http.Header),
					Body:       body,
				}, nil
			})}

			err = client.PostRaw(context.Background(), "/Images/item", nil, []byte("image"), "image/jpeg")
			if err == nil || !strings.Contains(err.Error(), "502") {
				t.Fatalf("unexpected error: %v", err)
			}
			wantRead := tt.size
			if wantRead > MaxResponseBodyBytes {
				wantRead = MaxResponseBodyBytes
			}
			if body.read != wantRead {
				t.Fatalf("bytes read = %d, want %d", body.read, wantRead)
			}
			if len(err.Error()) > ErrorBodyMaxLen+64 {
				t.Fatalf("unbounded error message: %d bytes", len(err.Error()))
			}
		})
	}
}

func TestGetUserIDDoesNotLockDuringRequest(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startOnce sync.Once
	var requests atomic.Int64

	client, err := NewClient(ClientConfig{
		BaseURL: "http://jellyfin.local",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/Users/Me" {
			t.Fatalf("unexpected request: %s", req.URL.Path)
		}
		requests.Add(1)
		startOnce.Do(func() { close(requestStarted) })
		<-releaseRequest
		return jsonResponse(http.StatusOK, `{"Id":"user-1"}`), nil
	})}

	const callers = 8
	results := make(chan string, callers)
	errors := make(chan error, callers)
	for range callers {
		go func() {
			userID, err := client.GetUserID(context.Background())
			results <- userID
			errors <- err
		}()
	}

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("user ID resolution request did not start")
	}

	mutexAvailable := make(chan struct{})
	go func() {
		client.mu.Lock()
		client.mu.Unlock()
		close(mutexAvailable)
	}()
	select {
	case <-mutexAvailable:
	case <-time.After(time.Second):
		t.Fatal("mutex remained locked during the HTTP request")
	}

	close(releaseRequest)
	for range callers {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if userID := <-results; userID != "user-1" {
			t.Fatalf("GetUserID() = %q", userID)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("network resolutions = %d, want 1", requests.Load())
	}
}
