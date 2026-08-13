package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type JellyfinClient struct {
	baseURL       string
	apiKey        string
	httpClient    *http.Client
	userID        string
	requireUserID bool
	mu            sync.Mutex
	userIDReady   chan struct{}
}

func (c *JellyfinClient) BaseURL() string { return c.baseURL }
func (c *JellyfinClient) APIKey() string  { return c.apiKey }

// NewJellyfinClient creates a client from environment variables.
func NewJellyfinClient() *JellyfinClient {
	cfg, err := LoadClientConfigFromEnv()
	if err != nil {
		log.Fatalf("%v", err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		log.Fatalf("%v", err)
	}
	return client
}

// NewClient constructs a client from explicit configuration.
func NewClient(cfg ClientConfig) (*JellyfinClient, error) {
	if err := ValidateClientConfig(cfg); err != nil {
		return nil, err
	}
	return &JellyfinClient{
		baseURL:       normalizeBaseURL(cfg.BaseURL),
		apiKey:        cfg.APIKey,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		userID:        strings.TrimSpace(cfg.UserID),
		requireUserID: cfg.RequireUserID,
	}, nil
}

func (c *JellyfinClient) DoRequest(ctx context.Context, method, endpoint string, params url.Values, body any) ([]byte, error) {
	u, err := url.JoinPath(c.baseURL, endpoint)
	if err != nil {
		return nil, fmt.Errorf("building URL: %w", err)
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-MediaBrowser-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, Truncate(string(respBody), ErrorBodyMaxLen))
	}

	return respBody, nil
}

func (c *JellyfinClient) Get(ctx context.Context, endpoint string, params url.Values, dest any) error {
	body, err := c.DoRequest(ctx, "GET", endpoint, params, nil)
	if err != nil {
		return err
	}
	if dest != nil && len(body) > 0 {
		return json.Unmarshal(body, dest)
	}
	return nil
}

func (c *JellyfinClient) GetRaw(ctx context.Context, endpoint string, params url.Values) (string, error) {
	body, err := c.DoRequest(ctx, "GET", endpoint, params, nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *JellyfinClient) Post(ctx context.Context, endpoint string, params url.Values, reqBody any, dest any) error {
	body, err := c.DoRequest(ctx, "POST", endpoint, params, reqBody)
	if err != nil {
		return err
	}
	if dest != nil && len(body) > 0 {
		return json.Unmarshal(body, dest)
	}
	return nil
}

func (c *JellyfinClient) PostNoContent(ctx context.Context, endpoint string, params url.Values, reqBody any) error {
	_, err := c.DoRequest(ctx, "POST", endpoint, params, reqBody)
	return err
}

// PostRaw performs a POST request with a raw binary body and custom content type.
// Used for image uploads where the body is raw bytes, not JSON.
func (c *JellyfinClient) PostRaw(ctx context.Context, endpoint string, params url.Values, body []byte, contentType string) error {
	u, err := url.JoinPath(c.baseURL, endpoint)
	if err != nil {
		return fmt.Errorf("building URL: %w", err)
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-MediaBrowser-Token", c.apiKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyBytes))
		return fmt.Errorf("API error %d: %s", resp.StatusCode, Truncate(string(respBody), ErrorBodyMaxLen))
	}

	// Drain body so the connection can be reused (HTTP keep-alive)
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *JellyfinClient) Del(ctx context.Context, endpoint string, params url.Values) error {
	_, err := c.DoRequest(ctx, "DELETE", endpoint, params, nil)
	return err
}

// FetchAllPages pages through a Jellyfin list endpoint (any endpoint that
// returns {"Items": [...], "TotalRecordCount": N}) and collects all items up
// to maxItems. The caller's params are used as-is except Limit and StartIndex
// which are managed by the paginator.
func FetchAllPages(ctx context.Context, client Client, endpoint string, params url.Values, maxItems int) ([]any, int, error) {
	if maxItems <= 0 {
		maxItems = DefaultMaxItems
	}
	var allItems []any
	startIndex := 0
	totalRecords := 0
	for {
		fetchSize := DefaultPageSize
		if remaining := maxItems - len(allItems); remaining < fetchSize {
			fetchSize = remaining
		}
		params.Set("Limit", fmt.Sprintf("%d", fetchSize))
		params.Set("StartIndex", fmt.Sprintf("%d", startIndex))

		var result map[string]any
		if err := client.Get(ctx, endpoint, params, &result); err != nil {
			if len(allItems) > 0 {
				break // return what we collected so far
			}
			return nil, 0, err
		}
		rawItems := ToSlice(result["Items"])
		if len(rawItems) == 0 {
			totalRecords = GetInt(result, "TotalRecordCount")
			break
		}
		allItems = append(allItems, rawItems...)
		totalRecords = GetInt(result, "TotalRecordCount")
		startIndex += len(rawItems)
		if startIndex >= totalRecords || len(allItems) >= maxItems {
			break
		}
	}
	if len(allItems) > maxItems {
		allItems = allItems[:maxItems]
	}
	return allItems, totalRecords, nil
}

// GetUserID returns the cached user ID or resolves it once.
func (c *JellyfinClient) GetUserID(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		if c.userID != "" {
			userID := c.userID
			c.mu.Unlock()
			return userID, nil
		}
		if c.requireUserID {
			c.mu.Unlock()
			return "", fmt.Errorf("user ID not set: set JELLYFIN_USER_ID because JELLYFIN_REQUIRE_USER_ID is enabled")
		}
		if c.userIDReady != nil {
			ready := c.userIDReady
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("waiting for user ID resolution: %w", ctx.Err())
			case <-ready:
				continue
			}
		}

		ready := make(chan struct{})
		c.userIDReady = ready
		c.mu.Unlock()

		userID, err := c.resolveUserID(ctx)

		c.mu.Lock()
		if err == nil && c.userID == "" {
			c.userID = userID
		}
		if c.userID != "" {
			userID = c.userID
			err = nil
		}
		close(ready)
		c.userIDReady = nil
		c.mu.Unlock()

		return userID, err
	}
}

func (c *JellyfinClient) resolveUserID(ctx context.Context) (string, error) {
	// Try /Users/Me first -- works when the API token is a user auth token
	// and returns the correct user identity for the current session.
	var me map[string]any
	if err := c.Get(ctx, "/Users/Me", nil, &me); err == nil {
		if id, ok := me["Id"].(string); ok && id != "" {
			log.Printf("resolved user via /Users/Me: %s", id)
			return id, nil
		}
	}

	// Fall back to /Users list (server API key -- not user-scoped)
	var users []map[string]any
	if err := c.Get(ctx, "/Users", nil, &users); err != nil {
		return "", fmt.Errorf("fetching users: %w", err)
	}
	if len(users) == 0 {
		return "", fmt.Errorf("no users found on Jellyfin server")
	}

	// Prefer an admin user for broader API access
	var fallbackID string
	for _, u := range users {
		id, ok := u["Id"].(string)
		if !ok || id == "" {
			continue
		}
		if fallbackID == "" {
			fallbackID = id
		}
		if policy, ok := u["Policy"].(map[string]any); ok {
			if isAdmin, ok := policy["IsAdministrator"].(bool); ok && isAdmin {
				log.Printf("auto-detected admin user ID: %s (set JELLYFIN_USER_ID to override)", id)
				return id, nil
			}
		}
	}

	if fallbackID == "" {
		return "", fmt.Errorf("no valid user ID found on Jellyfin server")
	}
	log.Printf("WARNING: no admin user found, using first user ID: %s (set JELLYFIN_USER_ID to override)", fallbackID)
	return fallbackID, nil
}
