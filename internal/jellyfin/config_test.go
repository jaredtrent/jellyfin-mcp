package jellyfin

import (
	"strings"
	"testing"
)

func TestValidateJellyfinURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "missing", raw: "", wantErr: true},
		{name: "whitespace", raw: "   ", wantErr: true},
		{name: "placeholder", raw: "https://jellyfin_host:8920", wantErr: true},
		{name: "uppercase placeholder", raw: "http://JELLYFIN_HOST:8096", wantErr: true},
		{name: "missing scheme", raw: "jellyfin.local:8096", wantErr: true},
		{name: "invalid scheme", raw: "ftp://jellyfin.local", wantErr: true},
		{name: "missing host", raw: "http:///jellyfin", wantErr: true},
		{name: "invalid port", raw: "http://jellyfin.local:abc", wantErr: true},
		{name: "http", raw: "http://jellyfin.local:8096"},
		{name: "https", raw: "https://media.example.com"},
		{name: "localhost", raw: "http://127.0.0.1:8096"},
		{name: "ipv6", raw: "http://[::1]:8096"},
		{name: "reverse proxy path", raw: "https://media.example.com/jellyfin/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateJellyfinURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateJellyfinURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateClientConfig(t *testing.T) {
	t.Parallel()

	valid := ClientConfig{BaseURL: "http://localhost:8096", APIKey: "secret"}
	if err := ValidateClientConfig(valid); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}

	tests := []struct {
		name string
		cfg  ClientConfig
	}{
		{name: "missing API key", cfg: ClientConfig{BaseURL: valid.BaseURL}},
		{name: "missing URL", cfg: ClientConfig{APIKey: valid.APIKey}},
		{
			name: "strict user ID missing",
			cfg: ClientConfig{
				BaseURL:       valid.BaseURL,
				APIKey:        valid.APIKey,
				RequireUserID: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateClientConfig(tt.cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	strict := valid
	strict.RequireUserID = true
	strict.UserID = "user-1"
	if err := ValidateClientConfig(strict); err != nil {
		t.Fatalf("valid strict configuration rejected: %v", err)
	}
}

func TestLoadClientConfigFromEnv(t *testing.T) {
	t.Setenv("JELLYFIN_URL", " https://media.example.com/jellyfin/ ")
	t.Setenv("JELLYFIN_API_KEY", "secret")
	t.Setenv("JELLYFIN_USER_ID", " user-1 ")
	t.Setenv("JELLYFIN_REQUIRE_USER_ID", "true")

	cfg, err := LoadClientConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://media.example.com/jellyfin/" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.APIKey != "secret" || cfg.UserID != "user-1" || !cfg.RequireUserID {
		t.Fatalf("unexpected configuration: %+v", cfg)
	}

	t.Setenv("JELLYFIN_REQUIRE_USER_ID", "not-a-boolean")
	if _, err := LoadClientConfigFromEnv(); err == nil {
		t.Fatal("invalid boolean value accepted")
	}
}

func TestNewClientNormalizesURLWithoutLeakingAPIKey(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientConfig{
		BaseURL: " https://media.example.com/jellyfin/ ",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.BaseURL(); got != "https://media.example.com/jellyfin" {
		t.Fatalf("BaseURL = %q", got)
	}

	_, err = NewClient(ClientConfig{
		BaseURL: ":// invalid",
		APIKey:  "do-not-leak-this",
	})
	if err == nil {
		t.Fatal("invalid URL accepted")
	}
	if strings.Contains(err.Error(), "do-not-leak-this") {
		t.Fatalf("API key leaked in error: %v", err)
	}
}
