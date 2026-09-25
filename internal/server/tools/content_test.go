package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaredtrent/jellyfin-mcp/internal/server/tools"
)

// contentPost records one PostNoContent or PostRaw call; a raw JSON body is
// recorded decoded.
type contentPost struct {
	endpoint string
	params   url.Values
	body     any
}

// recordContentPosts returns a client that appends every PostNoContent call
// and every JSON PostRaw call to posts, checking that a streamed body's size
// is its Content-Length, and records a PostRaw of any other type as a test
// failure.
func recordContentPosts(t *testing.T, posts *[]contentPost) *mockClient {
	t.Helper()
	return &mockClient{
		getFunc: subtitleAppearsAfterUpload(posts),
		postNoContentFunc: func(_ context.Context, endpoint string, params url.Values, body any) error {
			*posts = append(*posts, contentPost{endpoint, maps.Clone(params), body})
			return nil
		},
		postRawFunc: func(_ context.Context, endpoint string, params url.Values, body io.Reader, size int64, contentType string) error {
			if contentType != "application/json" {
				t.Errorf("unexpected PostRaw of %s to %s", contentType, endpoint)
				return nil
			}
			b, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(b)) != size {
				t.Errorf("PostRaw to %s: body is %d bytes but size says %d", endpoint, len(b), size)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("PostRaw to %s: body is not a JSON object: %v", endpoint, err)
			}
			*posts = append(*posts, contentPost{endpoint, maps.Clone(params), m})
			return nil
		},
	}
}

// contentBodyJSON returns body as the JSON object the client would send.
func contentBodyJSON(t *testing.T, body any) map[string]any {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body is not a JSON object: %s", b)
	}
	return m
}

func TestMetadata_Apply_PostsBareRemoteSearchResult(t *testing.T) {
	var posts []contentPost
	mc := recordContentPosts(t, &posts)

	unconfirmed := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
		"action":        "apply",
		"item_id":       "item-1",
		"provider_name": "Tmdb",
		"provider_id":   "provider-id-1",
	})
	if text := resultText(t, unconfirmed); !strings.Contains(text, "confirm=true") || len(posts) != 0 {
		t.Fatalf("unconfirmed apply: want the confirmation warning and no post, got %d posts: %s", len(posts), text)
	}

	result := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
		"action":        "apply",
		"item_id":       "item-1",
		"provider_name": "Tmdb",
		"provider_id":   "provider-id-1",
		"confirm":       true,
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts, want 1", len(posts))
	}
	post := posts[0]
	if post.endpoint != "/Items/RemoteSearch/Apply/item-1" {
		t.Errorf("endpoint = %q, want /Items/RemoteSearch/Apply/item-1", post.endpoint)
	}
	if got := post.params.Get("ReplaceAllImages"); got != "true" {
		t.Errorf("ReplaceAllImages = %q, want true", got)
	}
	body := contentBodyJSON(t, post.body)
	if _, wrapped := body["SearchResult"]; wrapped {
		t.Errorf("body wraps the search result: %v", body)
	}
	ids, ok := body["ProviderIds"].(map[string]any)
	if !ok {
		t.Fatalf("body has no top-level ProviderIds object: %v", body)
	}
	if len(ids) != 1 || ids["Tmdb"] != "provider-id-1" {
		t.Errorf("ProviderIds = %v, want {Tmdb: provider-id-1}", ids)
	}
}

// update previews with dry_run=true, asks for confirmation, and writes only
// once confirmed.
func TestMetadata_Update_DryRunThenConfirm(t *testing.T) {
	var posts []contentPost
	mc := recordContentPosts(t, &posts)
	mc.getFunc = func(_ context.Context, endpoint string, _ url.Values, dest any) error {
		if endpoint == "/Users" {
			return jsonInto([]map[string]any{{"Id": "user-1", "Policy": map[string]any{"IsAdministrator": true}}}, dest)
		}
		return jsonInto(map[string]any{"Id": "item-1", "Name": "Old", "Overview": "old"}, dest)
	}
	base := map[string]any{"action": "update", "item_id": "item-1", "overview": "new"}

	dry := maps.Clone(base)
	dry["dry_run"] = true
	if text := resultText(t, callTool(t, mc, "", "jellyfin_metadata", dry)); !strings.Contains(text, "Dry run") || !strings.Contains(text, "overview") || len(posts) != 0 {
		t.Fatalf("dry run: want a preview naming overview and no post, got %d posts: %s", len(posts), text)
	}
	if text := resultText(t, callTool(t, mc, "", "jellyfin_metadata", base)); !strings.Contains(text, "confirm=true") || len(posts) != 0 {
		t.Fatalf("unconfirmed: want the confirmation warning and no post, got %d posts: %s", len(posts), text)
	}
	confirmed := maps.Clone(base)
	confirmed["confirm"] = true
	if result := callTool(t, mc, "", "jellyfin_metadata", confirmed); result.IsError || len(posts) != 1 {
		t.Fatalf("confirmed: want one post, got %d: %s", len(posts), resultText(t, result))
	}
	if body := contentBodyJSON(t, posts[0].body); body["Overview"] != "new" || posts[0].endpoint != "/Items/item-1" {
		t.Errorf("post = %s %v", posts[0].endpoint, body)
	}
	if text := resultText(t, callTool(t, mc, "", "jellyfin_metadata", map[string]any{"action": "update", "item_id": "item-1", "confirm": true})); !strings.Contains(text, "No fields to update") {
		t.Errorf("no fields: got %s", text)
	}
}

func TestMetadata_Apply_ProviderKeySpelling(t *testing.T) {
	tests := []struct {
		name, providerName, wantKey string
	}{
		{"built-in key in lower case", "tmdb", "Tmdb"},
		{"built-in key in upper case with spaces", " IMDB ", "Imdb"},
		{"compound built-in key", "musicbrainzreleasegroup", "MusicBrainzReleaseGroup"},
		{"plugin key passes through", "PluginProvider", "PluginProvider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts []contentPost
			mc := recordContentPosts(t, &posts)

			result := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
				"action":        "apply",
				"item_id":       "item-1",
				"provider_name": tt.providerName,
				"provider_id":   " provider-id-1 ",
				"confirm":       true,
			})

			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(t, result))
			}
			if len(posts) != 1 {
				t.Fatalf("got %d posts, want 1", len(posts))
			}
			ids, _ := contentBodyJSON(t, posts[0].body)["ProviderIds"].(map[string]any)
			if len(ids) != 1 || ids[tt.wantKey] != "provider-id-1" {
				t.Errorf("ProviderIds = %v, want {%s: provider-id-1}", ids, tt.wantKey)
			}
		})
	}
}

func TestMetadata_Apply_RejectsBlankProvider(t *testing.T) {
	var posts []contentPost
	mc := recordContentPosts(t, &posts)

	result := callTool(t, mc, "", "jellyfin_metadata", map[string]any{
		"action":        "apply",
		"item_id":       "item-1",
		"provider_name": "   ",
		"provider_id":   "provider-id-1",
	})

	if !result.IsError {
		t.Fatalf("expected an error, got: %s", resultText(t, result))
	}
	if len(posts) != 0 {
		t.Errorf("got %d posts, want none", len(posts))
	}
}

func TestImages_RemoteDownload_SendsQueryWithoutBody(t *testing.T) {
	tests := []struct {
		name      string
		imageType string
		wantType  string
	}{
		{"explicit type", "Backdrop", "Backdrop"},
		{"default type", "", "Primary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts []contentPost
			mc := recordContentPosts(t, &posts)
			args := map[string]any{
				"action":    "remote_download",
				"item_id":   "item-1",
				"image_url": "https://images.example.com/image-1.jpg?size=original&lang=en",
			}
			if tt.imageType != "" {
				args["image_type"] = tt.imageType
			}

			result := callTool(t, mc, "", "jellyfin_images", args)

			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(t, result))
			}
			if len(posts) != 1 {
				t.Fatalf("got %d posts, want 1", len(posts))
			}
			post := posts[0]
			if post.endpoint != "/Items/item-1/RemoteImages/Download" {
				t.Errorf("endpoint = %q, want /Items/item-1/RemoteImages/Download", post.endpoint)
			}
			if got := post.params.Get("Type"); got != tt.wantType {
				t.Errorf("Type = %q, want %q", got, tt.wantType)
			}
			if got := post.params.Get("ImageUrl"); got != "https://images.example.com/image-1.jpg?size=original&lang=en" {
				t.Errorf("ImageUrl = %q", got)
			}
			if post.body != nil {
				t.Errorf("body = %v, want none", post.body)
			}
		})
	}
}

// Leading bytes of each image format the upload accepts.
var (
	contentJPEG = []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00")
	contentPNG  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0DIHDR")
	contentGIF  = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00")
	contentWebP = []byte("RIFF\x1a\x00\x00\x00WEBPVP8L\x0d\x00\x00\x00")
	contentBMP  = []byte("BM\x3a\x00\x00\x00\x00\x00\x00\x00\x36\x00")
)

// wrapBase64 breaks s into lines and pads it with spaces, as a caller
// pasting a base64 dump might.
func wrapBase64(s string) string {
	var b strings.Builder
	b.WriteString("  ")
	for len(s) > 8 {
		b.WriteString(s[:8])
		b.WriteString("\r\n\t")
		s = s[8:]
	}
	b.WriteString(s)
	b.WriteString(" \n")
	return b.String()
}

func TestImages_Upload_PostsBase64TextWithDetectedType(t *testing.T) {
	tests := []struct {
		name            string
		image           []byte
		wantContentType string
	}{
		{"jpeg", contentJPEG, "image/jpeg"},
		{"png", contentPNG, "image/png"},
		{"gif", contentGIF, "image/gif"},
		{"webp", contentWebP, "image/webp"},
		{"bmp", contentBMP, "image/bmp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString(tt.image)
			var gotEndpoint, gotContentType string
			var gotBody []byte
			var gotSize int64
			calls := 0
			mc := &mockClient{
				postRawFunc: func(_ context.Context, endpoint string, _ url.Values, body io.Reader, size int64, contentType string) error {
					calls++
					b, err := io.ReadAll(body)
					if err != nil {
						return err
					}
					gotEndpoint, gotBody, gotSize, gotContentType = endpoint, b, size, contentType
					return nil
				},
			}

			result := callTool(t, mc, "", "jellyfin_images", map[string]any{
				"action":     "upload",
				"item_id":    "item-1",
				"image_type": "Logo",
				"image_data": wrapBase64(encoded),
			})

			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(t, result))
			}
			if calls != 1 {
				t.Fatalf("got %d PostRaw calls, want 1", calls)
			}
			if gotEndpoint != "/Items/item-1/Images/Logo" {
				t.Errorf("endpoint = %q, want /Items/item-1/Images/Logo", gotEndpoint)
			}
			if gotContentType != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", gotContentType, tt.wantContentType)
			}
			if string(gotBody) != encoded {
				t.Errorf("body = %q, want the base64 text %q without whitespace", gotBody, encoded)
			}
			if gotSize != int64(len(encoded)) {
				t.Errorf("size = %d, want %d", gotSize, len(encoded))
			}
		})
	}
}

func TestImages_Upload_RejectsInvalidData(t *testing.T) {
	tests := []struct {
		name      string
		imageData string
		wantError string
	}{
		{"not base64", "not base64!", "not valid base64"},
		{"data URL", "data:image/png;base64," + base64.StdEncoding.EncodeToString(contentPNG), "not valid base64"},
		{"whitespace only", " \n\t ", "not valid base64"},
		{"not an image", base64.StdEncoding.EncodeToString([]byte("plain text, not an image")), "not a JPEG, PNG, WebP, GIF, or BMP image"},
		{"corrupt past the header", base64.StdEncoding.EncodeToString(append(bytes.Clone(contentPNG), make([]byte, 2048)...))[:2000] + "!" + "AAAA", "not valid base64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockClient{
				postRawFunc: func(_ context.Context, endpoint string, _ url.Values, _ io.Reader, _ int64, _ string) error {
					t.Errorf("unexpected PostRaw to %s", endpoint)
					return nil
				},
			}

			result := callTool(t, mc, "", "jellyfin_images", map[string]any{
				"action":     "upload",
				"item_id":    "item-1",
				"image_data": tt.imageData,
			})

			text := resultText(t, result)
			if !result.IsError {
				t.Fatalf("expected an error, got: %s", text)
			}
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("error = %q, want it to mention %q", text, tt.wantError)
			}
		})
	}
}

// The largest upload the tools accept travels in one tool call and reaches
// Jellyfin in a request body Jellyfin accepts. One more byte is refused with
// the limit the schema states.
func TestUploads_SizeLimit(t *testing.T) {
	const (
		limit = 20 << 20
		// Kestrel's default request-body limit, which Jellyfin does not change.
		jellyfinMaxRequestBody = 30_000_000
	)
	// The largest call carries the upload's base64 broken into 64-character
	// lines, each CRLF escaped in JSON as four bytes, and the request around
	// it. A client whose JSON encoder escapes + as \u002B adds five bytes for
	// each +, which is one character in 64 of the base64 of an image or other
	// compressed data.
	encoded := base64.StdEncoding.EncodedLen(limit)
	lineBreaks := encoded / 64 * len(`\r\n`)
	escapedPlus := encoded / 64 * (len(`\u002B`) - 1)
	if largest := encoded + lineBreaks + escapedPlus + 64<<10; largest > tools.MaxMessageBytes {
		t.Errorf("the largest upload call is about %d bytes, over tools.MaxMessageBytes (%d)", largest, tools.MaxMessageBytes)
	}
	uploads := []struct {
		tool, action, field string
		args                map[string]any
		header              []byte
	}{
		{"jellyfin_images", "upload", "image_data", nil, contentPNG},
		{"jellyfin_subtitles_lyrics", "upload_subtitle", "subtitle_data", map[string]any{"subtitle_format": "sup"}, []byte("PG")},
	}
	for _, u := range uploads {
		t.Run(u.tool, func(t *testing.T) {
			var sent []int // sizes of the request bodies sent to Jellyfin
			var posts []contentPost
			mc := &mockClient{
				getFunc: subtitleAppearsAfterUpload(&posts),
				postRawFunc: func(_ context.Context, _ string, _ url.Values, body io.Reader, size int64, _ string) error {
					n, err := io.Copy(io.Discard, body)
					if n != size {
						t.Errorf("body is %d bytes but size says %d", n, size)
					}
					sent = append(sent, int(n))
					posts = append(posts, contentPost{})
					return err
				},
			}
			cs := newTestSession(t, mc, "")

			if got := inputDescription(t, cs, u.tool, u.field); !strings.Contains(got, "at most 20 MiB") {
				t.Errorf("%s description = %q, want it to state the 20 MiB limit", u.field, got)
			}
			for _, size := range []int{limit, limit + 1} {
				data := make([]byte, size)
				copy(data, u.header)
				args := map[string]any{"action": u.action, "item_id": "item-1", u.field: base64.StdEncoding.EncodeToString(data)}
				maps.Copy(args, u.args)
				result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: u.tool, Arguments: args})
				if err != nil {
					t.Fatalf("%d bytes: %v", size, err)
				}
				text := resultText(t, result)
				if size > limit {
					if !result.IsError || !strings.Contains(text, "exceeds the upload limit of 20 MiB") || len(sent) != 1 {
						t.Errorf("%d bytes: want the limit error and nothing sent, got %d bodies: %s", size, len(sent)-1, text)
					}
					continue
				}
				if result.IsError || len(sent) != 1 {
					t.Fatalf("%d bytes: want one upload, got %d bodies: %s", size, len(sent), text)
				}
				if sent[0] > jellyfinMaxRequestBody {
					t.Errorf("%d bytes: request body of %d bytes exceeds Jellyfin's limit of %d", size, sent[0], jellyfinMaxRequestBody)
				}
			}
		})
	}
}

// inputDescription returns the schema description of one input of a tool.
func inputDescription(t *testing.T, cs *mcp.ClientSession, tool, field string) string {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		if tl.Name != tool {
			continue
		}
		b, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]struct{ Description string }
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}
		return schema.Properties[field].Description
	}
	t.Fatalf("no tool %s", tool)
	return ""
}

func TestSubtitles_Search_ReportsThreeLetterLanguage(t *testing.T) {
	var gotEndpoint string
	mc := &mockClient{
		getFunc: func(_ context.Context, endpoint string, _ url.Values, dest any) error {
			gotEndpoint = endpoint
			return jsonInto([]map[string]any{{
				"Id":                         "subtitle-1",
				"Name":                       "Test Movie subtitle",
				"ProviderName":               "Provider A",
				"Format":                     "srt",
				"ThreeLetterISOLanguageName": "eng",
				"DownloadCount":              7,
			}}, dest)
		},
	}

	result := callTool(t, mc, "", "jellyfin_subtitles_lyrics", map[string]any{
		"action":   "search_subtitles",
		"item_id":  "item-1",
		"language": "eng",
	})

	text := resultText(t, result)
	if result.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	if gotEndpoint != "/Items/item-1/RemoteSearch/Subtitles/eng" {
		t.Errorf("endpoint = %q, want /Items/item-1/RemoteSearch/Subtitles/eng", gotEndpoint)
	}
	if !strings.Contains(text, `"language": "eng"`) {
		t.Errorf("expected language eng in result, got: %s", text)
	}
}

func TestSubtitles_Upload_SendsRequiredFlags(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nLine A\n"))
	tests := []struct {
		name                 string
		extraArgs            map[string]any
		wantForced, wantSDH  bool
		wantFormat, wantLang string
	}{
		{"flags unset", map[string]any{"subtitle_format": "srt"}, false, false, "srt", "eng"},
		{"flags set", map[string]any{"subtitle_format": "ass", "is_forced": true, "is_hearing_impaired": true, "subtitle_language": "spa"}, true, true, "ass", "spa"},
		{"format in upper case", map[string]any{"subtitle_format": " VTT "}, false, false, "vtt", "eng"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts []contentPost
			mc := recordContentPosts(t, &posts)
			args := map[string]any{
				"action":        "upload_subtitle",
				"item_id":       "item-1",
				"subtitle_data": wrapBase64(data),
			}
			maps.Copy(args, tt.extraArgs)

			result := callTool(t, mc, "", "jellyfin_subtitles_lyrics", args)

			if result.IsError {
				t.Fatalf("unexpected error: %s", resultText(t, result))
			}
			if len(posts) != 1 {
				t.Fatalf("got %d posts, want 1", len(posts))
			}
			if posts[0].endpoint != "/Videos/item-1/Subtitles" {
				t.Errorf("endpoint = %q, want /Videos/item-1/Subtitles", posts[0].endpoint)
			}
			body := contentBodyJSON(t, posts[0].body)
			for field, want := range map[string]bool{"IsForced": tt.wantForced, "IsHearingImpaired": tt.wantSDH} {
				got, present := body[field]
				if !present {
					t.Errorf("body has no %s: %v", field, body)
				} else if got != want {
					t.Errorf("%s = %v, want %v", field, got, want)
				}
			}
			if body["Format"] != tt.wantFormat {
				t.Errorf("Format = %v, want %s", body["Format"], tt.wantFormat)
			}
			if body["Language"] != tt.wantLang {
				t.Errorf("Language = %v, want %s", body["Language"], tt.wantLang)
			}
			if body["Data"] != data {
				t.Errorf("Data = %v, want the base64 text without whitespace", body["Data"])
			}
		})
	}
}

func TestSubtitles_Upload_RejectsInvalidInput(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString([]byte("WEBVTT\n"))
	tests := []struct {
		name, format, data, wantError string
	}{
		{"unsupported format", "txt", valid, "ass, mks, sami, smi, srt, ssa, sub, sup, vtt"},
		{"format with a dot", ".srt", valid, "Unsupported subtitle_format"},
		{"raw subtitle text", "srt", "1\n00:00:01,000 --> 00:00:02,000\nLine A\n", "not valid base64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var posts []contentPost
			mc := recordContentPosts(t, &posts)

			result := callTool(t, mc, "", "jellyfin_subtitles_lyrics", map[string]any{
				"action":          "upload_subtitle",
				"item_id":         "item-1",
				"subtitle_format": tt.format,
				"subtitle_data":   tt.data,
			})

			text := resultText(t, result)
			if !result.IsError {
				t.Fatalf("expected an error, got: %s", text)
			}
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("error = %q, want it to mention %q", text, tt.wantError)
			}
			if len(posts) != 0 {
				t.Errorf("got %d posts, want none", len(posts))
			}
		})
	}
}

// image_type is a path segment of the image routes, so only Jellyfin's image
// types are accepted, and nothing is sent for any other value.
func TestImages_RejectsUnknownImageType(t *testing.T) {
	for _, imageType := range []string{"../../../System/Shutdown", "primary", "Poster"} {
		requests := 0
		mc := &mockClient{
			postRawFunc:       func(context.Context, string, url.Values, io.Reader, int64, string) error { requests++; return nil },
			postNoContentFunc: func(context.Context, string, url.Values, any) error { requests++; return nil },
		}
		for _, args := range []map[string]any{
			{"action": "upload", "item_id": "item-1", "image_type": imageType, "image_data": contentPNG},
			{"action": "remote_download", "item_id": "item-1", "image_type": imageType, "image_url": "https://images.example.com/a.jpg"},
			{"action": "get_url", "item_id": "item-1", "image_type": imageType},
		} {
			result := callTool(t, mc, "", "jellyfin_images", args)
			if text := resultText(t, result); !result.IsError || !strings.Contains(text, "is not a Jellyfin image type") {
				t.Errorf("%s with image_type %q: %s", args["action"], imageType, text)
			}
		}
		if requests != 0 {
			t.Errorf("image_type %q: %d requests were sent", imageType, requests)
		}
	}
}

func TestImages_GetURLEscapesItemID(t *testing.T) {
	mc := &mockClient{baseURLVal: "http://localhost:8096"}
	result := callTool(t, mc, "", "jellyfin_images", map[string]any{"action": "get_url", "item_id": "../System"})
	if text := resultText(t, result); !strings.Contains(text, "http://localhost:8096/Items/..%2FSystem/Images/Primary/0") {
		t.Errorf("got %s", text)
	}
}

// subtitleAppearsAfterUpload answers an item read with one external subtitle
// track once a post has been recorded, and with none before, the way the
// server lists an uploaded track after it re-probes the item.
func subtitleAppearsAfterUpload(posts *[]contentPost) func(context.Context, string, url.Values, any) error {
	return func(_ context.Context, endpoint string, _ url.Values, dest any) error {
		if !strings.HasPrefix(endpoint, "/Items/") {
			return nil
		}
		streams := []any{}
		if len(*posts) > 0 {
			streams = append(streams, map[string]any{"Type": "Subtitle", "IsExternal": true, "Index": 0})
		}
		if m, ok := dest.(*map[string]any); ok {
			*m = map[string]any{"Id": "item-1", "Name": "Item One", "MediaStreams": streams}
		}
		return nil
	}
}
