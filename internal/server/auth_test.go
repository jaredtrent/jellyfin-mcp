package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTokenRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1:8080"},
		{addr: "127.0.0.2:8080"},
		{addr: "localhost:8080"},
		{addr: "[::1]:8080"},
		{addr: "0.0.0.0:8080", want: true},
		{addr: "[::]:8080", want: true},
		{addr: ":8080", want: true},
		{addr: "192.168.1.20:8080", want: true},
		{addr: "jellyfin.local:8080", want: true},
		{addr: "invalid-address", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			if got := httpTokenRequired(tt.addr); got != tt.want {
				t.Fatalf("httpTokenRequired(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestValidateHTTPAuth(t *testing.T) {
	t.Parallel()

	if err := validateHTTPAuth("127.0.0.1:8080", ""); err != nil {
		t.Fatalf("local listener rejected: %v", err)
	}
	if err := validateHTTPAuth("0.0.0.0:8080", "secret"); err != nil {
		t.Fatalf("authenticated listener rejected: %v", err)
	}
	if err := validateHTTPAuth("0.0.0.0:8080", ""); err == nil {
		t.Fatal("non-local listener without a token accepted")
	}
}

func TestBearerAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		token         string
		authorization string
		wantStatus    int
		wantCalled    bool
	}{
		{name: "authentication disabled", wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "missing token", token: "secret", wantStatus: http.StatusUnauthorized},
		{
			name:          "invalid scheme",
			token:         "secret",
			authorization: "Basic secret",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "invalid token",
			token:         "secret",
			authorization: "Bearer wrong",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "valid token",
			token:         "secret",
			authorization: "Bearer secret",
			wantStatus:    http.StatusNoContent,
			wantCalled:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			handler := bearerAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}), tt.token)

			request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.authorization != "" {
				request.Header.Set("Authorization", tt.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if called != tt.wantCalled {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCalled)
			}
		})
	}
}
