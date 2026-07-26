package resources

import (
	"context"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubClient struct{}

func (*stubClient) Get(context.Context, string, url.Values, any) error {
	return nil
}

func (*stubClient) GetRaw(context.Context, string, url.Values) (string, error) {
	return "", nil
}

func (*stubClient) Post(context.Context, string, url.Values, any, any) error {
	return nil
}

func (*stubClient) PostNoContent(context.Context, string, url.Values, any) error {
	return nil
}

func (*stubClient) PostRaw(context.Context, string, url.Values, []byte, string) error {
	return nil
}

func (*stubClient) Del(context.Context, string, url.Values) error {
	return nil
}

func (*stubClient) DoRequest(context.Context, string, string, url.Values, any) ([]byte, error) {
	return nil, nil
}

func (*stubClient) GetUserID(context.Context) (string, error) {
	return "user-1", nil
}

func (*stubClient) BaseURL() string {
	return "http://jellyfin.local"
}

func (*stubClient) APIKey() string {
	return "secret"
}

func TestAdminResourcesEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		readOnly bool
		toolsets string
		want     bool
	}{
		{name: "surface complète", want: true},
		{name: "lecture seule", readOnly: true},
		{name: "sans admin", toolsets: "discovery,media"},
		{name: "admin explicite", toolsets: "discovery, admin", want: true},
		{name: "lecture seule prioritaire", readOnly: true, toolsets: "admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AdminResourcesEnabled(tt.readOnly, tt.toolsets); got != tt.want {
				t.Fatalf("AdminResourcesEnabled() = %v, attendu %v", got, tt.want)
			}
		})
	}
}

func TestRegisterResourcesScopesAdminResources(t *testing.T) {
	t.Parallel()

	for _, includeAdmin := range []bool{false, true} {
		t.Run(map[bool]string{false: "masquées", true: "exposées"}[includeAdmin], func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			RegisterResources(server, &stubClient{}, includeAdmin)

			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
				t.Fatal(err)
			}
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
			session, err := client.Connect(t.Context(), clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })

			resources, err := session.ListResources(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			templates, err := session.ListResourceTemplates(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}

			hasUsersResource := false
			for _, resource := range resources.Resources {
				if resource.URI == "jellyfin://users" {
					hasUsersResource = true
				}
			}
			hasUsersTemplate := false
			for _, template := range templates.ResourceTemplates {
				if template.URITemplate == "jellyfin://users/{userId}" {
					hasUsersTemplate = true
				}
			}
			if hasUsersResource != includeAdmin || hasUsersTemplate != includeAdmin {
				t.Fatalf(
					"surface admin inattendue: resource=%v template=%v includeAdmin=%v",
					hasUsersResource,
					hasUsersTemplate,
					includeAdmin,
				)
			}
		})
	}
}
