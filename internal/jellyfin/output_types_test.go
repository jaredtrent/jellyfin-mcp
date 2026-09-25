package jellyfin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// BaseItemDto has no FileName property, so the get_item output schema must not
// declare a file_name field that could only ever be empty.
func TestDetailedItemOutput_DeclaresNoFileName(t *testing.T) {
	typ := reflect.TypeOf(DetailedItemOutput{})
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "file_name" {
			t.Errorf("DetailedItemOutput.%s declares %q", typ.Field(i).Name, name)
		}
	}
}

// marshalJSON is v as JSON, failing the test when it cannot be encoded.
func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	return string(b)
}

// Counts and lists are present even when zero or empty, and an output that
// serves several actions shows only the fields of the action that filled it.
func TestOutputs_AlwaysPresentFields(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		present []string
		absent  []string
	}{
		{
			name:    "item list counts",
			value:   ItemListOutput{},
			present: []string{`"total_count":0`, `"shown":0`},
		},
		{
			name:    "item list with no items",
			value:   ItemListOutput{Items: []MediaItem{}},
			present: []string{`"items":[]`},
		},
		{
			name:    "sessions list with no sessions",
			value:   SessionsOutput{Sessions: &[]SessionInfo{}},
			present: []string{`"sessions":[]`},
			absent:  []string{`"resume"`},
		},
		{
			name:    "library stats with no types",
			value:   AnalyticsOutput{Stats: &[]TypeCount{}},
			present: []string{`"stats":[]`},
			absent: []string{
				`"unread_types"`, `"total_size_gb"`, `"total_size_mb"`, `"by_type"`,
				`"size_report_type"`, `"size_report"`, `"codec_report"`, `"items"`,
				`"total_count"`, `"total_is_lower_bound"`, `"shown"`, `"days"`,
				`"undated_count"`, `"duplicates"`, `"item_summary"`, `"users"`, `"notes"`,
			},
		},
		{
			name:    "played status of a user who never played",
			value:   PlayedStatusUser{},
			present: []string{`"played":false`},
		},
		{
			name:    "season with no episodes",
			value:   SeasonInfo{},
			present: []string{`"episode_count":0`},
		},
		{
			name:    "duplicates with an unknown year",
			value:   DuplicateGroup{},
			present: []string{`"year":0`},
		},
		{
			name:    "play state that is not paused",
			value:   PlayStateInfo{},
			present: []string{`"is_paused":false`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalJSON(t, tc.value)
			for _, want := range tc.present {
				if !strings.Contains(got, want) {
					t.Errorf("%s lacks %s", got, want)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(got, unwanted) {
					t.Errorf("%s has %s", got, unwanted)
				}
			}
		})
	}
}

// --- ToStringSlice (sanity check; extensive tests are in helpers_test.go) ---

func TestToStringSlice_Sanity(t *testing.T) {
	// []string pass-through
	got := ToStringSlice([]string{"a", "b"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("[]string: got %v", got)
	}

	// []any with strings
	got = ToStringSlice([]any{"x", "y", "z"})
	if !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Errorf("[]any: got %v", got)
	}

	// nil
	got = ToStringSlice(nil)
	if got != nil {
		t.Errorf("nil: expected nil, got %v", got)
	}

	// Unsupported type
	got = ToStringSlice(42)
	if got != nil {
		t.Errorf("int: expected nil, got %v", got)
	}

	// []any with no strings yields nil
	got = ToStringSlice([]any{1, 2, 3})
	if got != nil {
		t.Errorf("[]any no strings: expected nil, got %v", got)
	}
}
