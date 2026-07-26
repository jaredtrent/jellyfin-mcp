package server

import (
	"context"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type completionClient struct {
	usersCalls atomic.Int64
}

func (client *completionClient) Get(_ context.Context, endpoint string, _ url.Values, dest any) error {
	if endpoint == "/Users" {
		client.usersCalls.Add(1)
		users := dest.(*[]map[string]any)
		*users = []map[string]any{{"Id": "user-1", "Name": "Alice"}}
	}
	return nil
}

func (*completionClient) GetRaw(context.Context, string, url.Values) (string, error) {
	return "", nil
}

func (*completionClient) Post(context.Context, string, url.Values, any, any) error {
	return nil
}

func (*completionClient) PostNoContent(context.Context, string, url.Values, any) error {
	return nil
}

func (*completionClient) PostRaw(context.Context, string, url.Values, []byte, string) error {
	return nil
}

func (*completionClient) Del(context.Context, string, url.Values) error {
	return nil
}

func (*completionClient) DoRequest(context.Context, string, string, url.Values, any) ([]byte, error) {
	return nil, nil
}

func (*completionClient) GetUserID(context.Context) (string, error) {
	return "user-1", nil
}

func (*completionClient) BaseURL() string {
	return "http://jellyfin.local"
}

func (*completionClient) APIKey() string {
	return "secret"
}

func TestUserCompletionFollowsAdminSurface(t *testing.T) {
	t.Parallel()

	request := &mcp.CompleteRequest{Params: &mcp.CompleteParams{
		Ref: &mcp.CompleteReference{
			Type: "ref/resource",
			URI:  "jellyfin://users/{userId}",
		},
		Argument: mcp.CompleteParamsArgument{Name: "userId", Value: ""},
	}}

	client := &completionClient{}
	hiddenResult, err := completionHandler(client, false)(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(hiddenResult.Completion.Values) != 0 {
		t.Fatalf("complétions exposées sans surface admin: %v", hiddenResult.Completion.Values)
	}
	if client.usersCalls.Load() != 0 {
		t.Fatal("/Users appelé alors que la surface admin est masquée")
	}

	visibleResult, err := completionHandler(client, true)(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(visibleResult.Completion.Values) != 1 || visibleResult.Completion.Values[0] != "user-1" {
		t.Fatalf("complétions admin inattendues: %v", visibleResult.Completion.Values)
	}
	if client.usersCalls.Load() != 1 {
		t.Fatalf("appels /Users = %d, attendu 1", client.usersCalls.Load())
	}
}
