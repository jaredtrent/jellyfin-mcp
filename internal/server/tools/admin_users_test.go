package tools_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

const testAdminTargetUserID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

// postCall records one PostNoContent request.
type postCall struct {
	endpoint string
	params   url.Values
	body     any
}

// recordingUsersClient returns a mock that records every PostNoContent call
// and fails the test if a request is sent with any other write verb.
func recordingUsersClient(t *testing.T, calls *[]postCall) *mockClient {
	t.Helper()
	return &mockClient{
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, body any) error {
			*calls = append(*calls, postCall{endpoint: endpoint, params: params, body: body})
			return nil
		},
		postFunc: func(_ context.Context, endpoint string, _ url.Values, _ any, _ any) error {
			t.Errorf("unexpected Post to %s", endpoint)
			return nil
		},
		delFunc: func(_ context.Context, endpoint string, _ url.Values) error {
			t.Errorf("unexpected Del to %s", endpoint)
			return nil
		},
	}
}

func assertSinglePost(t *testing.T, calls []postCall, wantEndpoint, wantUserID string) postCall {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("expected 1 POST, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.endpoint != wantEndpoint {
		t.Errorf("endpoint = %q, want %q", c.endpoint, wantEndpoint)
	}
	if got := c.params.Get("userId"); got != wantUserID {
		t.Errorf("userId query = %q, want %q (params: %v)", got, wantUserID, c.params)
	}
	return c
}

func TestUsers_UpdatePassword_UsesQueryUserID(t *testing.T) {
	var calls []postCall
	mc := recordingUsersClient(t, &calls)

	result := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":   "update_password",
		"confirm":  true,
		"user_id":  testAdminTargetUserID,
		"password": "placeholder-password",
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	c := assertSinglePost(t, calls, "/Users/Password", testAdminTargetUserID)
	body, ok := c.body.(map[string]any)
	if !ok || body["NewPw"] != "placeholder-password" {
		t.Errorf("body = %#v, want NewPw set", c.body)
	}
}

func TestUsers_UpdateConfig_RawConfigAsksConfirmation(t *testing.T) {
	var calls []postCall
	mc := recordingUsersClient(t, &calls)
	result := callTool(t, mc, "", "jellyfin_users", map[string]any{"action": "update_config", "user_id": "user-1", "config": map[string]any{"AudioLanguagePreference": "eng"}})
	if text := resultText(t, result); !strings.Contains(text, "CONFIRMATION REQUIRED") || !strings.Contains(text, "Replace the entire configuration") {
		t.Errorf("got %s", text)
	}
	if len(calls) != 0 {
		t.Errorf("POST sent without confirmation: %v", calls)
	}
}

func TestUsers_UpdateConfig_RawConfigUsesQueryUserID(t *testing.T) {
	var calls []postCall
	mc := recordingUsersClient(t, &calls)
	// The user is read only for its name in the confirmation, never to build
	// the body: the raw config is sent as given.
	mc.getFunc = func(_ context.Context, endpoint string, _ url.Values, dest any) error {
		if !strings.HasPrefix(endpoint, "/Users/") {
			t.Errorf("unexpected GET %s", endpoint)
		}
		if m, ok := dest.(*map[string]any); ok {
			*m = map[string]any{"Name": "Placeholder", "Configuration": map[string]any{"AudioLanguagePreference": "fra"}}
		}
		return nil
	}

	result := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":  "update_config",
		"confirm": true,
		"user_id": testAdminTargetUserID,
		"config":  map[string]any{"SubtitleLanguagePreference": "eng"},
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	c := assertSinglePost(t, calls, "/Users/Configuration", testAdminTargetUserID)
	body, ok := c.body.(map[string]any)
	if !ok || body["SubtitleLanguagePreference"] != "eng" {
		t.Errorf("body = %#v, want the raw config passed through", c.body)
	}
}

func TestUsers_UpdateConfig_MergeUsesQueryUserID(t *testing.T) {
	var calls []postCall
	var getEndpoint string
	mc := recordingUsersClient(t, &calls)
	mc.getFunc = func(_ context.Context, endpoint string, _ url.Values, dest any) error {
		getEndpoint = endpoint
		return jsonInto(map[string]any{
			"Name": "User A",
			"Configuration": map[string]any{
				"AudioLanguagePreference": "jpn",
			},
		}, dest)
	}

	result := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":            "update_config",
		"user_id":           testAdminTargetUserID,
		"subtitle_language": "eng",
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if want := "/Users/" + testAdminTargetUserID; getEndpoint != want {
		t.Errorf("GET endpoint = %q, want %q", getEndpoint, want)
	}
	c := assertSinglePost(t, calls, "/Users/Configuration", testAdminTargetUserID)
	body, ok := c.body.(map[string]any)
	if !ok {
		t.Fatalf("body = %#v, want a configuration map", c.body)
	}
	if body["SubtitleLanguagePreference"] != "eng" || body["AudioLanguagePreference"] != "jpn" {
		t.Errorf("body = %#v, want the fetched configuration merged with the new subtitle language", body)
	}
}

func TestUsers_UpdatePolicy_KeepsPathUserID(t *testing.T) {
	var calls []postCall
	mc := recordingUsersClient(t, &calls)
	mc.getFunc = func(_ context.Context, _ string, _ url.Values, dest any) error {
		return jsonInto(map[string]any{"Name": "User A", "Policy": map[string]any{}}, dest)
	}

	result := callTool(t, mc, "", "jellyfin_users", map[string]any{
		"action":   "update_policy",
		"confirm":  true,
		"user_id":  testAdminTargetUserID,
		"is_admin": false,
	})

	if text := resultText(t, result); result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(calls))
	}
	if want := "/Users/" + testAdminTargetUserID + "/Policy"; calls[0].endpoint != want {
		t.Errorf("endpoint = %q, want %q", calls[0].endpoint, want)
	}
	if len(calls[0].params) != 0 {
		t.Errorf("expected no query parameters, got %v", calls[0].params)
	}
}
