package server

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// validateHTTPAuth requires a token whenever the listener is not loopback-only.
func validateHTTPAuth(addr, token string) error {
	if token == "" && httpTokenRequired(addr) {
		return fmt.Errorf("--http-token is required when listening on non-localhost address %s", addr)
	}
	return nil
}

func httpTokenRequired(addr string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// bearerAuth protects the handler with a constant-time token comparison.
func bearerAuth(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(actual, expected) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
