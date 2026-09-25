package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type JellyfinClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	// configuredUser is JELLYFIN_USER_ID as given: a user ID or a username.
	configuredUser string

	mu     sync.Mutex
	userID string // resolved user ID; empty until the first successful resolution

	versionMu      sync.Mutex
	version        ServerVersion
	versionFetched time.Time // zero until the version has been read once
}

func (c *JellyfinClient) BaseURL() string { return c.baseURL }

// NewJellyfinClient creates a client from environment variables.
// Exits if JELLYFIN_API_KEY is not set.
func NewJellyfinClient() *JellyfinClient {
	baseURL := os.Getenv("JELLYFIN_URL")
	if baseURL == "" {
		baseURL = "https://jellyfin_host:8920"
	}
	apiKey := os.Getenv("JELLYFIN_API_KEY")
	if apiKey == "" {
		log.Fatalf("JELLYFIN_API_KEY environment variable must be set")
	}
	return &JellyfinClient{
		baseURL:        baseURL,
		apiKey:         apiKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		configuredUser: strings.TrimSpace(os.Getenv("JELLYFIN_USER_ID")),
	}
}

// authorizationHeader is the value of the Authorization header that carries
// the API key. Jellyfin 12 honors only this header and the ApiKey query
// parameter unless the administrator re-enables legacy authorization, and
// every supported version parses it. For an API key the server fills in the
// client, device, and version fields itself, so the header sends only the
// token. The server URL-decodes each value, so the token is URL-encoded.
func (c *JellyfinClient) authorizationHeader() string {
	return `MediaBrowser Token="` + url.QueryEscape(c.apiKey) + `"`
}

// send is the single request path for every verb: it builds the URL,
// authenticates, and turns a non-2xx status into an error. The response body
// is returned for the caller to decode.
func (c *JellyfinClient) send(ctx context.Context, method, endpoint string, params url.Values, body io.Reader, size int64, contentType string) ([]byte, error) {
	u, err := url.JoinPath(c.baseURL, endpoint)
	if err != nil {
		return nil, fmt.Errorf("building URL: %w", err)
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if body != nil {
		req.ContentLength = size
	}
	req.Header.Set("Authorization", c.authorizationHeader())
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, noResponseError{err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: Truncate(string(respBody), ErrorBodyMaxLen)}
	}

	return respBody, nil
}

// APIError is a response with a status outside 2xx, from Jellyfin or from a
// proxy in front of it.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e.StatusCode == http.StatusUnauthorized {
		return fmt.Sprintf("API error 401 (Jellyfin rejected the API key; check JELLYFIN_API_KEY): %s", e.Body)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// noResponseError is a request that got no response, such as one that could
// not connect or that ran past the client's timeout.
type noResponseError struct{ err error }

func (e noResponseError) Error() string { return "connection error: " + e.err.Error() }
func (e noResponseError) Unwrap() error { return e.err }

// OutcomeUnknown reports whether err leaves it unknown whether Jellyfin
// carried out the request. That is the case when no response arrived, and
// when a gateway answered in Jellyfin's place with 502 Bad Gateway or 504
// Gateway Timeout: Jellyfin may have applied the request, or may still apply
// it, because its handlers keep running after the client goes away. Any other
// status comes after the request has finished, so what the server shows
// afterward is its result.
func OutcomeUnknown(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusBadGateway || apiErr.StatusCode == http.StatusGatewayTimeout
	}
	return errors.As(err, new(noResponseError))
}

// DoRequest sends a request whose body, when present, is encoded as JSON.
func (c *JellyfinClient) DoRequest(ctx context.Context, method, endpoint string, params url.Values, body any) ([]byte, error) {
	if body == nil {
		return c.send(ctx, method, endpoint, params, nil, 0, "")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}
	return c.send(ctx, method, endpoint, params, bytes.NewReader(b), int64(len(b)), "application/json")
}

func (c *JellyfinClient) Get(ctx context.Context, endpoint string, params url.Values, dest any) error {
	body, err := c.DoRequest(ctx, "GET", endpoint, params, nil)
	if err != nil {
		return err
	}
	if dest != nil && len(body) > 0 {
		if err := json.Unmarshal(body, dest); err != nil {
			return err
		}
		trimToLimit(params, dest)
	}
	return nil
}

// trimToLimit cuts a list result's Items to the Limit the request asked for,
// so that a server that ignores Limit, or one that answers in Jellyfin's
// place, cannot make a tool result many times larger than requested.
func trimToLimit(params url.Values, dest any) {
	limit, err := strconv.Atoi(params.Get("Limit"))
	if err != nil || limit <= 0 {
		return
	}
	m, ok := dest.(*map[string]any)
	if !ok || m == nil {
		return
	}
	if items, ok := (*m)["Items"].([]any); ok && len(items) > limit {
		(*m)["Items"] = items[:limit]
	}
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

// PostRaw performs a POST request whose body is streamed from body as-is,
// with size as its Content-Length and the given content type, for endpoints
// that do not take JSON and for bodies too large to build in memory.
func (c *JellyfinClient) PostRaw(ctx context.Context, endpoint string, params url.Values, body io.Reader, size int64, contentType string) error {
	_, err := c.send(ctx, http.MethodPost, endpoint, params, body, size, contentType)
	return err
}

func (c *JellyfinClient) Del(ctx context.Context, endpoint string, params url.Values) error {
	_, err := c.DoRequest(ctx, "DELETE", endpoint, params, nil)
	return err
}

// FetchAllPages pages through a Jellyfin list endpoint and collects every
// item up to maxItems. It is FetchAllPagesUntil without a stop condition.
func FetchAllPages(ctx context.Context, client Client, endpoint string, params url.Values, maxItems int) ([]any, int, error) {
	return FetchAllPagesUntil(ctx, client, endpoint, params, maxItems, nil)
}

// FetchAllPagesUntil pages through a Jellyfin list endpoint (any endpoint that
// returns {"Items": [...], "TotalRecordCount": N}) and collects items in the
// server's order, up to maxItems (DefaultMaxItems when maxItems is not
// positive). The pager sets Limit and StartIndex in params, so the caller's
// map is modified and must be non-nil.
//
// When stop is non-nil it is called on each item in order. The first item for
// which it returns true is excluded along with every item after it, and no
// further page is requested, so stop suits a list sorted on the field it tests.
//
// The returned total is the server's TotalRecordCount for the whole query. It
// counts items past maxItems and past the stop point, so a caller that stops
// early should count the returned items instead.
//
// An error on any page is returned without items, because a list missing its
// later pages cannot be told apart from a complete one.
func FetchAllPagesUntil(ctx context.Context, client Client, endpoint string, params url.Values, maxItems int, stop func(item map[string]any) bool) ([]any, int, error) {
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
			return nil, 0, err
		}
		rawItems := ToSlice(result["Items"])
		totalRecords = GetInt(result, "TotalRecordCount")
		if len(rawItems) == 0 {
			break
		}
		keep, stopped := len(rawItems), false
		if stop != nil {
			for i, raw := range rawItems {
				if stop(ToMap(raw)) {
					keep, stopped = i, true
					break
				}
			}
		}
		allItems = append(allItems, rawItems[:keep]...)
		startIndex += len(rawItems)
		if stopped || startIndex >= totalRecords || len(allItems) >= maxItems {
			break
		}
	}
	if len(allItems) > maxItems {
		allItems = allItems[:maxItems]
	}
	return allItems, totalRecords, nil
}

// EachPage pages through every item of a Jellyfin list endpoint and passes
// each page to fn with its 1-based page number, keeping no items itself, so a
// report over a whole library holds only what fn keeps. It sets Limit and
// StartIndex in params, which must be non-nil, and returns the server's
// TotalRecordCount. An error on any page ends the walk and is returned.
func EachPage(ctx context.Context, client Client, endpoint string, params url.Values, fn func(page int, items []map[string]any)) (int, error) {
	startIndex, total := 0, 0
	for page := 1; ; page++ {
		params.Set("Limit", fmt.Sprintf("%d", DefaultPageSize))
		params.Set("StartIndex", fmt.Sprintf("%d", startIndex))
		var result map[string]any
		if err := client.Get(ctx, endpoint, params, &result); err != nil {
			return 0, err
		}
		raw := ToSlice(result["Items"])
		total = GetInt(result, "TotalRecordCount")
		if len(raw) == 0 {
			return total, nil
		}
		items := make([]map[string]any, 0, len(raw))
		for _, r := range raw {
			if m := ToMap(r); m != nil {
				items = append(items, m)
			}
		}
		fn(page, items)
		startIndex += len(raw)
		if startIndex >= total {
			return total, nil
		}
	}
}

// GetUserID returns the ID of the user that user-scoped calls act as. It is
// resolved on first use and cached; a failed resolution is not cached, so the
// next call retries.
//
// An explicit JELLYFIN_USER_ID always wins. It may be a user ID, with or
// without dashes, or a username, matched case-insensitively; a value that
// matches no user is an error rather than a silent fallback. Without it, a
// user token resolves to its own user through /Users/Me. An API key carries no
// user, so the first administrator is chosen, and failing that the first user.
func (c *JellyfinClient) GetUserID(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.userID != "" {
		return c.userID, nil
	}

	id, err := c.resolveUserID(ctx)
	if err != nil {
		return "", err
	}
	c.userID = id
	return id, nil
}

func (c *JellyfinClient) resolveUserID(ctx context.Context) (string, error) {
	if c.configuredUser == "" {
		// /Users/Me fails for API keys, which have no user; that is expected.
		var me map[string]any
		if err := c.Get(ctx, "/Users/Me", nil, &me); err == nil {
			if id := GetString(me, "Id"); id != "" {
				log.Printf("acting as the token's user %q (%s)", GetString(me, "Name"), id)
				return id, nil
			}
		}
	}

	var users []map[string]any
	if err := c.Get(ctx, "/Users", nil, &users); err != nil {
		return "", fmt.Errorf("fetching users: %w", err)
	}

	if c.configuredUser != "" {
		return matchConfiguredUser(users, c.configuredUser)
	}

	var first map[string]any
	for _, u := range users {
		if GetString(u, "Id") == "" {
			continue
		}
		if first == nil {
			first = u
		}
		if GetBool(ToMap(u["Policy"]), "IsAdministrator") {
			log.Printf("acting as administrator %q (%s); set JELLYFIN_USER_ID to choose another user", GetString(u, "Name"), GetString(u, "Id"))
			return GetString(u, "Id"), nil
		}
	}
	if first == nil {
		return "", fmt.Errorf("no users found on the Jellyfin server")
	}
	log.Printf("WARNING: no administrator found; acting as %q (%s). Set JELLYFIN_USER_ID to choose a user.", GetString(first, "Name"), GetString(first, "Id"))
	return GetString(first, "Id"), nil
}

// matchConfiguredUser finds the user named by JELLYFIN_USER_ID. An ID match
// takes precedence over a name match. Jellyfin writes IDs as 32 hex digits
// without dashes, so both sides are compared in that form.
func matchConfiguredUser(users []map[string]any, configured string) (string, error) {
	wantID := NormalizeID(configured)
	for _, u := range users {
		if id := GetString(u, "Id"); id != "" && NormalizeID(id) == wantID {
			return id, nil
		}
	}
	for _, u := range users {
		if id := GetString(u, "Id"); id != "" && strings.EqualFold(GetString(u, "Name"), configured) {
			log.Printf("JELLYFIN_USER_ID %q resolved to user ID %s", configured, id)
			return id, nil
		}
	}
	return "", fmt.Errorf("JELLYFIN_USER_ID %q does not match any user ID or username on the Jellyfin server", configured)
}
