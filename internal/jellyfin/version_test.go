package jellyfin

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestParseServerVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    ServerVersion
		wantErr bool
	}{
		{in: "12.1.0", want: ServerVersion{12, 1, 0}},
		{in: "10.11.11", want: ServerVersion{10, 11, 11}},
		{in: " 10.10.7 ", want: ServerVersion{10, 10, 7}},
		{in: "12.0", want: ServerVersion{12, 0, 0}},
		{in: "10.11.6.0", want: ServerVersion{10, 11, 6}},
		{in: "", wantErr: true},
		{in: "12", wantErr: true},
		{in: "12.x.0", wantErr: true},
		{in: "v12.1.0", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseServerVersion(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseServerVersion(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseServerVersion(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestServerVersionComparisons(t *testing.T) {
	tests := []struct {
		v              ServerVersion
		atLeast12      bool
		supported      bool
		atLeast10dot11 bool
	}{
		{v: ServerVersion{10, 10, 7}, atLeast12: false, supported: false, atLeast10dot11: false},
		{v: ServerVersion{10, 11, 0}, atLeast12: false, supported: true, atLeast10dot11: true},
		{v: ServerVersion{10, 11, 11}, atLeast12: false, supported: true, atLeast10dot11: true},
		{v: ServerVersion{12, 0, 0}, atLeast12: true, supported: true, atLeast10dot11: true},
		{v: ServerVersion{12, 1, 0}, atLeast12: true, supported: true, atLeast10dot11: true},
		{v: ServerVersion{13, 0, 0}, atLeast12: true, supported: true, atLeast10dot11: true},
	}
	for _, tt := range tests {
		if got := tt.v.AtLeast(12, 0); got != tt.atLeast12 {
			t.Errorf("%v.AtLeast(12, 0) = %v", tt.v, got)
		}
		if got := tt.v.AtLeast(10, 11); got != tt.atLeast10dot11 {
			t.Errorf("%v.AtLeast(10, 11) = %v", tt.v, got)
		}
		if got := tt.v.Supported(); got != tt.supported {
			t.Errorf("%v.Supported() = %v", tt.v, got)
		}
	}
}

func TestClientServerVersionCachesAndFallsBack(t *testing.T) {
	calls := 0
	fail := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/System/Info/Public" {
			http.NotFound(w, r)
			return
		}
		calls++
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"Version":"12.1.0"}`))
	})
	ctx := context.Background()

	for range 3 {
		v, err := c.ServerVersion(ctx)
		if err != nil || v != (ServerVersion{12, 1, 0}) {
			t.Fatalf("ServerVersion() = %v, %v", v, err)
		}
	}
	if calls != 1 {
		t.Errorf("server calls = %d, want 1 while cached", calls)
	}

	// After the TTL, a failed refresh keeps the last known version.
	c.versionFetched = time.Now().Add(-serverVersionTTL - time.Second)
	fail = true
	v, err := c.ServerVersion(ctx)
	if err != nil || v != (ServerVersion{12, 1, 0}) {
		t.Errorf("after failed refresh: ServerVersion() = %v, %v; want last known 12.1.0", v, err)
	}
	if calls != 2 {
		t.Errorf("server calls = %d, want a refresh attempt after the TTL", calls)
	}
}

func TestClientServerVersionUnreachable(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	if _, err := c.ServerVersion(context.Background()); err == nil {
		t.Error("ServerVersion() error = nil, want an error when the version was never read")
	}
}
