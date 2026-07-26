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
		{name: "absent", raw: "", wantErr: true},
		{name: "espaces", raw: "   ", wantErr: true},
		{name: "placeholder", raw: "https://jellyfin_host:8920", wantErr: true},
		{name: "placeholder majuscules", raw: "http://JELLYFIN_HOST:8096", wantErr: true},
		{name: "schéma absent", raw: "jellyfin.local:8096", wantErr: true},
		{name: "schéma invalide", raw: "ftp://jellyfin.local", wantErr: true},
		{name: "hôte absent", raw: "http:///jellyfin", wantErr: true},
		{name: "port invalide", raw: "http://jellyfin.local:abc", wantErr: true},
		{name: "http", raw: "http://jellyfin.local:8096"},
		{name: "https", raw: "https://media.example.com"},
		{name: "localhost", raw: "http://127.0.0.1:8096"},
		{name: "ipv6", raw: "http://[::1]:8096"},
		{name: "chemin reverse proxy", raw: "https://media.example.com/jellyfin/"},
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
		t.Fatalf("configuration valide rejetée: %v", err)
	}

	tests := []struct {
		name string
		cfg  ClientConfig
	}{
		{name: "clé API absente", cfg: ClientConfig{BaseURL: valid.BaseURL}},
		{name: "URL absente", cfg: ClientConfig{APIKey: valid.APIKey}},
		{
			name: "identifiant strict absent",
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
				t.Fatal("erreur attendue")
			}
		})
	}

	strict := valid
	strict.RequireUserID = true
	strict.UserID = "user-1"
	if err := ValidateClientConfig(strict); err != nil {
		t.Fatalf("configuration stricte valide rejetée: %v", err)
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
		t.Fatalf("configuration inattendue: %+v", cfg)
	}

	t.Setenv("JELLYFIN_REQUIRE_USER_ID", "not-a-boolean")
	if _, err := LoadClientConfigFromEnv(); err == nil {
		t.Fatal("valeur booléenne invalide acceptée")
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
		t.Fatal("URL invalide acceptée")
	}
	if strings.Contains(err.Error(), "do-not-leak-this") {
		t.Fatalf("la clé API apparaît dans l'erreur: %v", err)
	}
}
