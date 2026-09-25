package jellyfin

import (
	"context"
	"io"
	"net/url"
)

// Client is the interface for interacting with a Jellyfin server.
// It is implemented by JellyfinClient and can be replaced with a
// mock for testing.
type Client interface {
	Get(ctx context.Context, endpoint string, params url.Values, dest any) error
	GetRaw(ctx context.Context, endpoint string, params url.Values) (string, error)
	Post(ctx context.Context, endpoint string, params url.Values, reqBody any, dest any) error
	PostNoContent(ctx context.Context, endpoint string, params url.Values, reqBody any) error
	PostRaw(ctx context.Context, endpoint string, params url.Values, body io.Reader, size int64, contentType string) error
	Del(ctx context.Context, endpoint string, params url.Values) error
	DoRequest(ctx context.Context, method, endpoint string, params url.Values, body any) ([]byte, error)
	GetUserID(ctx context.Context) (string, error)
	ServerVersion(ctx context.Context) (ServerVersion, error)
	BaseURL() string
}

// compile-time check
var _ Client = (*JellyfinClient)(nil)
