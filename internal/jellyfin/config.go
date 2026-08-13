package jellyfin

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// ClientConfig contains the settings required by the Jellyfin client.
type ClientConfig struct {
	BaseURL       string
	APIKey        string
	UserID        string
	RequireUserID bool
}

// LoadClientConfigFromEnv reads client configuration from the environment.
func LoadClientConfigFromEnv() (ClientConfig, error) {
	cfg := ClientConfig{
		BaseURL: strings.TrimSpace(os.Getenv("JELLYFIN_URL")),
		APIKey:  os.Getenv("JELLYFIN_API_KEY"),
		UserID:  strings.TrimSpace(os.Getenv("JELLYFIN_USER_ID")),
	}

	rawRequireUserID := strings.TrimSpace(os.Getenv("JELLYFIN_REQUIRE_USER_ID"))
	if rawRequireUserID != "" {
		requireUserID, err := strconv.ParseBool(rawRequireUserID)
		if err != nil {
			return ClientConfig{}, fmt.Errorf("JELLYFIN_REQUIRE_USER_ID must be a boolean")
		}
		cfg.RequireUserID = requireUserID
	}

	return cfg, nil
}

// ValidateClientConfig validates configuration before any network request.
func ValidateClientConfig(cfg ClientConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("JELLYFIN_API_KEY environment variable must be set")
	}
	if err := ValidateJellyfinURL(cfg.BaseURL); err != nil {
		return err
	}
	if cfg.RequireUserID && strings.TrimSpace(cfg.UserID) == "" {
		return fmt.Errorf("JELLYFIN_USER_ID must be set when JELLYFIN_REQUIRE_USER_ID is enabled")
	}
	return nil
}

// ValidateJellyfinURL requires an absolute HTTP(S) URL and rejects the placeholder host.
func ValidateJellyfinURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("JELLYFIN_URL environment variable must be set")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("JELLYFIN_URL must be a valid absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("JELLYFIN_URL must use the http or https scheme")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("JELLYFIN_URL must include a host")
	}
	if strings.EqualFold(parsed.Hostname(), "jellyfin_host") {
		return fmt.Errorf("JELLYFIN_URL must not use the jellyfin_host placeholder")
	}

	return nil
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
