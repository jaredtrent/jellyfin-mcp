package jellyfin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanSchema_StripsOptionalNullUnionsAndKeepsRequiredSlicesNullable(t *testing.T) {
	inSchema := SchemaFor[BrowseInput]()
	b, err := json.Marshal(inSchema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"null"`) {
		t.Errorf("BrowseInput schema still contains null union: %s", b)
	}

	outSchema := SchemaFor[ItemListOutput]()
	resolved, err := outSchema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Zero-value ItemListOutput (substituted by go-sdk on error results) must validate cleanly.
	var zeroMap any
	zb, _ := json.Marshal(ItemListOutput{})
	_ = json.Unmarshal(zb, &zeroMap)
	if err := resolved.Validate(zeroMap); err != nil {
		t.Errorf("zero ItemListOutput failed validation: %v", err)
	}
	// Non-empty ItemListOutput must also validate cleanly.
	next := 50
	full := ItemListOutput{
		TotalCount:     100,
		Shown:          1,
		NextStartIndex: &next,
		Items:          []MediaItem{{ID: "1", Name: "Film", Type: "Movie", Year: 2024}},
		Notes:          []string{"more available"},
	}
	var fullMap any
	fb, _ := json.Marshal(full)
	_ = json.Unmarshal(fb, &fullMap)
	if err := resolved.Validate(fullMap); err != nil {
		t.Errorf("populated ItemListOutput failed validation: %v", err)
	}
}

func TestWithEnumsFiltered_PrunesPropertiesAndRequired(t *testing.T) {
	s := WithEnumsFiltered[UserDataInput](map[string][]any{
		"action": {"get_user_data"},
	}, []string{"action", "item_id"})
	if len(s.Properties) != 2 {
		t.Errorf("properties = %v, want only action and item_id", s.Properties)
	}
	if _, hasConfirm := s.Properties["confirm"]; hasConfirm {
		t.Error("confirm property should be pruned in read-only schema")
	}
}

func TestAllInputAndOutputSchemasCompileWithoutOptionalNulls(t *testing.T) {
	inputs := []any{
		SchemaFor[NoInput](),
		SchemaFor[SearchInput](),
		SchemaFor[BrowseInput](),
		SchemaFor[GetItemInput](),
		SchemaFor[RecommendationsInput](),
		SchemaFor[TVShowsInput](),
		SchemaFor[MusicInput](),
		SchemaFor[UserDataInput](),
		SchemaFor[PlaylistsInput](),
		SchemaFor[CollectionsInput](),
		SchemaFor[SessionsInput](),
		SchemaFor[PlaybackInput](),
		SchemaFor[SystemInfoInput](),
		SchemaFor[UsersInput](),
		SchemaFor[LibraryManageInput](),
		SchemaFor[PluginsInput](),
		SchemaFor[LiveTVInput](),
		SchemaFor[MetadataInput](),
		SchemaFor[SubtitlesLyricsInput](),
		SchemaFor[ImagesInput](),
		SchemaFor[DevicesInput](),
		SchemaFor[ServerManageInput](),
		SchemaFor[ItemExtrasInput](),
		SchemaFor[AnalyticsInput](),
		SchemaFor[PlaybackStatsInput](),
	}
	for i, s := range inputs {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("input[%d]: %v", i, err)
		}
		if strings.Contains(string(b), `"null"`) {
			t.Errorf("input[%d] still contains null union: %s", i, b)
		}
	}

	_ = SchemaFor[LibraryListOutput]()
	_ = SchemaFor[ItemListOutput]()
	_ = SchemaFor[DetailedItemOutput]()
	_ = SchemaFor[RecommendationsOutput]()
	_ = SchemaFor[TVShowsOutput]()
	_ = SchemaFor[SessionsOutput]()
	_ = SchemaFor[AnalyticsOutput]()
	statsOutSchema := SchemaFor[PlaybackStatsOutput]()
	statsResolved, err := statsOutSchema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var zeroStats any
	zb, _ := json.Marshal(PlaybackStatsOutput{})
	_ = json.Unmarshal(zb, &zeroStats)
	if err := statsResolved.Validate(zeroStats); err != nil {
		t.Errorf("zero PlaybackStatsOutput failed validation: %v", err)
	}
}
