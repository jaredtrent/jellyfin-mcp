package jellyfin

import (
	"math"
	"reflect"
	"testing"
)

// --- GetString ---

func TestGetString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{
			name: "string value",
			m:    map[string]any{"title": "Hello"},
			key:  "title",
			want: "Hello",
		},
		{
			name: "non-string value (int via Sprintf)",
			m:    map[string]any{"count": 42},
			key:  "count",
			want: "42",
		},
		{
			name: "non-string value (float64 via Sprintf)",
			m:    map[string]any{"rating": 7.5},
			key:  "rating",
			want: "7.5",
		},
		{
			name: "non-string value (bool via Sprintf)",
			m:    map[string]any{"active": true},
			key:  "active",
			want: "true",
		},
		{
			name: "missing key",
			m:    map[string]any{"other": "value"},
			key:  "missing",
			want: "",
		},
		{
			name: "nil map value",
			m:    map[string]any{"key": nil},
			key:  "key",
			want: "",
		},
		{
			name: "empty map",
			m:    map[string]any{},
			key:  "anything",
			want: "",
		},
		{
			name: "empty string value",
			m:    map[string]any{"empty": ""},
			key:  "empty",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetString(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetString(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

// --- GetInt ---

func TestGetInt(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want int
	}{
		{
			name: "float64 input (JSON number)",
			m:    map[string]any{"count": float64(42)},
			key:  "count",
			want: 42,
		},
		{
			name: "float64 with decimal truncation",
			m:    map[string]any{"count": float64(42.9)},
			key:  "count",
			want: 42,
		},
		{
			name: "missing key",
			m:    map[string]any{"other": float64(1)},
			key:  "missing",
			want: 0,
		},
		{
			name: "nil value",
			m:    map[string]any{"count": nil},
			key:  "count",
			want: 0,
		},
		{
			name: "non-numeric value (string)",
			m:    map[string]any{"count": "not a number"},
			key:  "count",
			want: 0,
		},
		{
			name: "zero float64",
			m:    map[string]any{"count": float64(0)},
			key:  "count",
			want: 0,
		},
		{
			name: "negative float64",
			m:    map[string]any{"count": float64(-5)},
			key:  "count",
			want: -5,
		},
		{
			name: "int value (not float64, returns 0)",
			m:    map[string]any{"count": 42},
			key:  "count",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetInt(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetInt(%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

// --- GetIntPtr ---

func TestGetIntPtr(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]any
		key     string
		wantNil bool
		wantVal int
	}{
		{
			name:    "float64 input returns pointer",
			m:       map[string]any{"val": float64(10)},
			key:     "val",
			wantNil: false,
			wantVal: 10,
		},
		{
			name:    "missing key returns nil",
			m:       map[string]any{},
			key:     "val",
			wantNil: true,
		},
		{
			name:    "nil value returns nil",
			m:       map[string]any{"val": nil},
			key:     "val",
			wantNil: true,
		},
		{
			name:    "non-float64 returns nil",
			m:       map[string]any{"val": "hello"},
			key:     "val",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetIntPtr(tt.m, tt.key)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetIntPtr() = %v, want nil", *got)
				}
			} else {
				if got == nil {
					t.Errorf("GetIntPtr() = nil, want %d", tt.wantVal)
				} else if *got != tt.wantVal {
					t.Errorf("GetIntPtr() = %d, want %d", *got, tt.wantVal)
				}
			}
		})
	}
}

// --- GetInt64 ---

func TestGetInt64(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want int64
	}{
		{
			name: "float64 input",
			m:    map[string]any{"ticks": float64(1000000)},
			key:  "ticks",
			want: 1000000,
		},
		{
			name: "large float64",
			m:    map[string]any{"ticks": float64(10000000000)},
			key:  "ticks",
			want: 10000000000,
		},
		{
			name: "missing key",
			m:    map[string]any{},
			key:  "ticks",
			want: 0,
		},
		{
			name: "nil value",
			m:    map[string]any{"ticks": nil},
			key:  "ticks",
			want: 0,
		},
		{
			name: "non-numeric value",
			m:    map[string]any{"ticks": "abc"},
			key:  "ticks",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetInt64(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetInt64(%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

// --- GetFloat ---

func TestGetFloat(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want float64
	}{
		{
			name: "float64 input",
			m:    map[string]any{"rating": float64(7.5)},
			key:  "rating",
			want: 7.5,
		},
		{
			name: "integer-valued float64",
			m:    map[string]any{"rating": float64(8)},
			key:  "rating",
			want: 8.0,
		},
		{
			name: "missing key",
			m:    map[string]any{},
			key:  "rating",
			want: 0,
		},
		{
			name: "nil value",
			m:    map[string]any{"rating": nil},
			key:  "rating",
			want: 0,
		},
		{
			name: "non-numeric value",
			m:    map[string]any{"rating": "good"},
			key:  "rating",
			want: 0,
		},
		{
			name: "zero float64",
			m:    map[string]any{"rating": float64(0)},
			key:  "rating",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFloat(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetFloat(%v, %q) = %f, want %f", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

// --- GetBool ---

func TestGetBool(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want bool
	}{
		{
			name: "true value",
			m:    map[string]any{"active": true},
			key:  "active",
			want: true,
		},
		{
			name: "false value",
			m:    map[string]any{"active": false},
			key:  "active",
			want: false,
		},
		{
			name: "missing key",
			m:    map[string]any{},
			key:  "active",
			want: false,
		},
		{
			name: "nil value",
			m:    map[string]any{"active": nil},
			key:  "active",
			want: false,
		},
		{
			name: "non-bool value (string)",
			m:    map[string]any{"active": "true"},
			key:  "active",
			want: false,
		},
		{
			name: "non-bool value (int)",
			m:    map[string]any{"active": 1},
			key:  "active",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBool(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("GetBool(%v, %q) = %v, want %v", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

// --- GetNum ---

func TestGetNum(t *testing.T) {
	t.Run("int target", func(t *testing.T) {
		tests := []struct {
			name string
			m    map[string]any
			key  string
			want int
		}{
			{
				name: "int value exact match",
				m:    map[string]any{"n": 42},
				key:  "n",
				want: 42,
			},
			{
				name: "float64 to int conversion",
				m:    map[string]any{"n": float64(99.7)},
				key:  "n",
				want: 99,
			},
			{
				name: "int64 to int conversion",
				m:    map[string]any{"n": int64(100)},
				key:  "n",
				want: 100,
			},
			{
				name: "missing key",
				m:    map[string]any{},
				key:  "n",
				want: 0,
			},
			{
				name: "nil value",
				m:    map[string]any{"n": nil},
				key:  "n",
				want: 0,
			},
			{
				name: "non-numeric type (string)",
				m:    map[string]any{"n": "hello"},
				key:  "n",
				want: 0,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := GetNum[int](tt.m, tt.key)
				if got != tt.want {
					t.Errorf("GetNum[int](%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
				}
			})
		}
	})

	t.Run("int64 target", func(t *testing.T) {
		tests := []struct {
			name string
			m    map[string]any
			key  string
			want int64
		}{
			{
				name: "int64 value exact match",
				m:    map[string]any{"n": int64(9999999999)},
				key:  "n",
				want: 9999999999,
			},
			{
				name: "float64 to int64 conversion",
				m:    map[string]any{"n": float64(123456789)},
				key:  "n",
				want: 123456789,
			},
			{
				name: "int to int64 conversion",
				m:    map[string]any{"n": 50},
				key:  "n",
				want: 50,
			},
			{
				name: "missing key",
				m:    map[string]any{},
				key:  "n",
				want: 0,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := GetNum[int64](tt.m, tt.key)
				if got != tt.want {
					t.Errorf("GetNum[int64](%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
				}
			})
		}
	})

	t.Run("float64 target", func(t *testing.T) {
		tests := []struct {
			name string
			m    map[string]any
			key  string
			want float64
		}{
			{
				name: "float64 value exact match",
				m:    map[string]any{"n": float64(3.14)},
				key:  "n",
				want: 3.14,
			},
			{
				name: "int to float64 conversion",
				m:    map[string]any{"n": 42},
				key:  "n",
				want: 42.0,
			},
			{
				name: "int64 to float64 conversion",
				m:    map[string]any{"n": int64(100)},
				key:  "n",
				want: 100.0,
			},
			{
				name: "missing key",
				m:    map[string]any{},
				key:  "n",
				want: 0.0,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := GetNum[float64](tt.m, tt.key)
				if math.Abs(got-tt.want) > 1e-9 {
					t.Errorf("GetNum[float64](%v, %q) = %f, want %f", tt.m, tt.key, got, tt.want)
				}
			})
		}
	})
}

// --- Truncate ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{
			name:   "shorter than max",
			s:      "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			s:      "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "longer than max",
			s:      "hello world",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "empty string",
			s:      "",
			maxLen: 5,
			want:   "",
		},
		{
			name:   "max zero",
			s:      "hello",
			maxLen: 0,
			want:   "",
		},
		{
			name:   "unicode multi-byte chars within limit",
			s:      "日本語テスト",
			maxLen: 6,
			want:   "日本語テスト",
		},
		{
			name:   "unicode multi-byte chars truncated",
			s:      "日本語テスト",
			maxLen: 3,
			want:   "日本語",
		},
		{
			name:   "mixed ascii and unicode",
			s:      "abc日本語",
			maxLen: 4,
			want:   "abc日",
		},
		{
			name:   "emoji truncation",
			s:      "Hello 🌍🌎🌏",
			maxLen: 7,
			want:   "Hello 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

// --- ToSlice ---

func TestToSlice(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []any
	}{
		{
			name: "nil input",
			v:    nil,
			want: nil,
		},
		{
			name: "valid []any",
			v:    []any{"a", "b", "c"},
			want: []any{"a", "b", "c"},
		},
		{
			name: "wrong type (string)",
			v:    "not a slice",
			want: nil,
		},
		{
			name: "wrong type (int)",
			v:    42,
			want: nil,
		},
		{
			name: "empty slice",
			v:    []any{},
			want: []any{},
		},
		{
			name: "wrong slice type ([]string)",
			v:    []string{"a", "b"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToSlice(tt.v)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToSlice(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// --- ToMap ---

func TestToMap(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want map[string]any
	}{
		{
			name: "nil input",
			v:    nil,
			want: nil,
		},
		{
			name: "valid map",
			v:    map[string]any{"key": "value"},
			want: map[string]any{"key": "value"},
		},
		{
			name: "wrong type (string)",
			v:    "not a map",
			want: nil,
		},
		{
			name: "wrong type (int)",
			v:    42,
			want: nil,
		},
		{
			name: "wrong map type (map[string]string)",
			v:    map[string]string{"key": "value"},
			want: nil,
		},
		{
			name: "empty map",
			v:    map[string]any{},
			want: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToMap(tt.v)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToMap(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// --- ToStringSlice ---

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{
			name: "[]string input",
			v:    []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "[]any with all strings",
			v:    []any{"x", "y", "z"},
			want: []string{"x", "y", "z"},
		},
		{
			name: "[]any with mixed types (strings extracted)",
			v:    []any{"a", 42, "b", true},
			want: []string{"a", "b"},
		},
		{
			name: "nil input",
			v:    nil,
			want: nil,
		},
		{
			name: "empty []any",
			v:    []any{},
			want: nil,
		},
		{
			name: "[]any with all non-strings",
			v:    []any{1, 2.0, true, nil},
			want: nil,
		},
		{
			name: "wrong type (string)",
			v:    "not a slice",
			want: nil,
		},
		{
			name: "empty []string",
			v:    []string{},
			want: []string{},
		},
		{
			name: "[]any with single string",
			v:    []any{"only"},
			want: []string{"only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToStringSlice(tt.v)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToStringSlice(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// --- FormatGB ---

func TestFormatGB(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{
			name:  "zero bytes",
			bytes: 0,
			want:  "0.00 GB",
		},
		{
			name:  "small - under 100 GB (2 decimal places)",
			bytes: 50 * BytesPerGB,
			want:  "50.00 GB",
		},
		{
			name:  "fractional GB (2 decimal places)",
			bytes: int64(1.5 * float64(BytesPerGB)),
			want:  "1.50 GB",
		},
		{
			name:  "exactly 100 GB (1 decimal place)",
			bytes: 100 * BytesPerGB,
			want:  "100.0 GB",
		},
		{
			name:  "large - over 100 GB (1 decimal place)",
			bytes: 500 * BytesPerGB,
			want:  "500.0 GB",
		},
		{
			name:  "very large - 1 TB",
			bytes: 1024 * BytesPerGB,
			want:  "1024.0 GB",
		},
		{
			name:  "1 byte",
			bytes: 1,
			want:  "0.00 GB",
		},
		{
			name:  "exactly 1 GB",
			bytes: BytesPerGB,
			want:  "1.00 GB",
		},
		{
			name:  "just under 100 GB boundary",
			bytes: 99*BytesPerGB + BytesPerGB*99/100, // ~99.99 GB
			want:  "99.99 GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatGB(tt.bytes)
			if got != tt.want {
				t.Errorf("FormatGB(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

// --- MaskToken ---

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "short token (under 12 chars)",
			token: "abc",
			want:  "abc",
		},
		{
			name:  "exactly 12 chars (at boundary, shown in full)",
			token: "123456789012",
			want:  "123456789012",
		},
		{
			name:  "13 chars (just over boundary, masked)",
			token: "1234567890123",
			want:  "1234...0123",
		},
		{
			name:  "long token",
			token: "abcdefghijklmnopqrstuvwxyz",
			want:  "abcd...wxyz",
		},
		{
			name:  "empty token",
			token: "",
			want:  "",
		},
		{
			name:  "single char",
			token: "x",
			want:  "x",
		},
		{
			name:  "16 char hex token",
			token: "a1b2c3d4e5f6g7h8",
			want:  "a1b2...g7h8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskToken(tt.token)
			if got != tt.want {
				t.Errorf("MaskToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

// --- DefaultInt ---

func TestDefaultInt(t *testing.T) {
	tests := []struct {
		name string
		v    int
		def  int
		want int
	}{
		{
			name: "zero returns default",
			v:    0,
			def:  25,
			want: 25,
		},
		{
			name: "negative returns default",
			v:    -5,
			def:  25,
			want: 25,
		},
		{
			name: "positive returns value",
			v:    10,
			def:  25,
			want: 10,
		},
		{
			name: "one returns value",
			v:    1,
			def:  50,
			want: 1,
		},
		{
			name: "large positive returns value",
			v:    999,
			def:  10,
			want: 999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultInt(tt.v, tt.def)
			if got != tt.want {
				t.Errorf("DefaultInt(%d, %d) = %d, want %d", tt.v, tt.def, got, tt.want)
			}
		})
	}
}

// --- ClampInt ---

func TestClampInt(t *testing.T) {
	tests := []struct {
		name string
		v    int
		def  int
		max  int
		want int
	}{
		{
			name: "zero returns default",
			v:    0,
			def:  25,
			max:  500,
			want: 25,
		},
		{
			name: "negative returns default",
			v:    -1,
			def:  50,
			max:  500,
			want: 50,
		},
		{
			name: "within range returns value",
			v:    100,
			def:  25,
			max:  500,
			want: 100,
		},
		{
			name: "exceeds max returns max",
			v:    1000,
			def:  25,
			max:  500,
			want: 500,
		},
		{
			name: "exactly at max returns max",
			v:    500,
			def:  25,
			max:  500,
			want: 500,
		},
		{
			name: "default exceeds max returns max",
			v:    0,
			def:  600,
			max:  500,
			want: 500,
		},
		{
			name: "one returns one",
			v:    1,
			def:  50,
			max:  500,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampInt(tt.v, tt.def, tt.max)
			if got != tt.want {
				t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.v, tt.def, tt.max, got, tt.want)
			}
		})
	}
}

// --- BoolPtr ---

func TestBoolPtr(t *testing.T) {
	tests := []struct {
		name string
		b    bool
		want bool
	}{
		{
			name: "true",
			b:    true,
			want: true,
		},
		{
			name: "false",
			b:    false,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BoolPtr(tt.b)
			if got == nil {
				t.Fatal("BoolPtr() returned nil")
			}
			if *got != tt.want {
				t.Errorf("BoolPtr(%v) = %v, want %v", tt.b, *got, tt.want)
			}
		})
	}
}

// --- FilterPrefix ---

func TestFilterPrefix(t *testing.T) {
	tests := []struct {
		name   string
		items  []string
		prefix string
		want   []string
	}{
		{
			name:   "empty prefix returns all items",
			items:  []string{"Action", "Comedy", "Drama"},
			prefix: "",
			want:   []string{"Action", "Comedy", "Drama"},
		},
		{
			name:   "matching prefix (case insensitive)",
			items:  []string{"Action", "Adventure", "Comedy", "Animation"},
			prefix: "a",
			want:   []string{"Action", "Adventure", "Animation"},
		},
		{
			name:   "matching prefix uppercase input",
			items:  []string{"Action", "Adventure", "Comedy"},
			prefix: "A",
			want:   []string{"Action", "Adventure"},
		},
		{
			name:   "no match",
			items:  []string{"Action", "Comedy", "Drama"},
			prefix: "z",
			want:   nil,
		},
		{
			name:   "exact match",
			items:  []string{"Action", "Comedy", "Drama"},
			prefix: "comedy",
			want:   []string{"Comedy"},
		},
		{
			name:   "empty items",
			items:  []string{},
			prefix: "a",
			want:   nil,
		},
		{
			name:   "nil items with empty prefix",
			items:  nil,
			prefix: "",
			want:   nil,
		},
		{
			name:   "partial word match",
			items:  []string{"Sci-Fi", "Science Fiction", "Scary Stories"},
			prefix: "sci",
			want:   []string{"Sci-Fi", "Science Fiction"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterPrefix(tt.items, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterPrefix(%v, %q) = %v, want %v", tt.items, tt.prefix, got, tt.want)
			}
		})
	}
}

// --- SanitizeID ---

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "normal UUID (no escaping needed)",
			id:   "d3b07384-d9a0-4e9a-8c5f-2f3e7b1a9c0d",
			want: "d3b07384-d9a0-4e9a-8c5f-2f3e7b1a9c0d",
		},
		{
			name: "string with slashes",
			id:   "path/to/item",
			want: "path%2Fto%2Fitem",
		},
		{
			name: "string with spaces",
			id:   "my item id",
			want: "my%20item%20id",
		},
		{
			name: "empty string",
			id:   "",
			want: "",
		},
		{
			name: "special characters",
			id:   "id?query=value&foo=bar",
			want: "id%3Fquery=value&foo=bar",
		},
		{
			name: "already safe string",
			id:   "simple-id-123",
			want: "simple-id-123",
		},
		{
			name: "a lone dot cannot collapse the path",
			id:   ".",
			want: "%2E",
		},
		{
			name: "a parent reference cannot climb the path",
			id:   "..",
			want: "%2E%2E",
		},
		{
			name: "a parent reference with a slash is escaped whole",
			id:   "../System/Shutdown",
			want: "..%2FSystem%2FShutdown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeID(tt.id)
			if got != tt.want {
				t.Errorf("SanitizeID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

// --- SplitIDs ---

func TestSplitIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{name: "one ID per element", ids: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "an element holding a list is split", ids: []string{"a,b, c"}, want: []string{"a", "b", "c"}},
		{name: "blank parts are dropped", ids: []string{" ", "a,,b", ""}, want: []string{"a", "b"}},
		{name: "nil", ids: nil, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitIDs(tt.ids)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitIDs(%q) = %q, want %q", tt.ids, got, tt.want)
			}
		})
	}
}

// --- JoinIDs ---

func TestJoinIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{
			name: "empty slice",
			ids:  []string{},
			want: "",
		},
		{
			name: "single ID",
			ids:  []string{"abc123"},
			want: "abc123",
		},
		{
			name: "multiple IDs",
			ids:  []string{"id1", "id2", "id3"},
			want: "id1,id2,id3",
		},
		{
			name: "nil slice",
			ids:  nil,
			want: "",
		},
		{
			name: "two IDs",
			ids:  []string{"first", "second"},
			want: "first,second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinIDs(tt.ids)
			if got != tt.want {
				t.Errorf("JoinIDs(%v) = %q, want %q", tt.ids, got, tt.want)
			}
		})
	}
}

// --- FormatJSON ---

func TestFormatJSON(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "map",
			v:    map[string]any{"key": "value"},
			want: "{\n  \"key\": \"value\"\n}",
		},
		{
			name: "slice",
			v:    []string{"a", "b"},
			want: "[\n  \"a\",\n  \"b\"\n]",
		},
		{
			name: "string",
			v:    "hello",
			want: "\"hello\"",
		},
		{
			name: "number",
			v:    42,
			want: "42",
		},
		{
			name: "nil",
			v:    nil,
			want: "null",
		},
		{
			name: "nested map",
			v:    map[string]any{"outer": map[string]any{"inner": "value"}},
			want: "{\n  \"outer\": {\n    \"inner\": \"value\"\n  }\n}",
		},
		{
			name: "invalid value (channel)",
			v:    make(chan int),
			want: "[json error: json: unsupported type: chan int]",
		},
		{
			name: "bool",
			v:    true,
			want: "true",
		},
		{
			name: "empty map",
			v:    map[string]any{},
			want: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatJSON(tt.v)
			if got != tt.want {
				t.Errorf("FormatJSON(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

// --- BuildProviderLinks ---

func TestBuildProviderLinks(t *testing.T) {
	tests := []struct {
		name        string
		providerIDs map[string]string
		itemType    string
		want        map[string]string
	}{
		{
			name:        "IMDb only",
			providerIDs: map[string]string{"Imdb": "tt1234567"},
			itemType:    "Movie",
			want: map[string]string{
				"IMDb": "https://www.imdb.com/title/tt1234567",
			},
		},
		{
			name:        "TMDb Movie",
			providerIDs: map[string]string{"Tmdb": "12345"},
			itemType:    "Movie",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/movie/12345",
			},
		},
		{
			name:        "TMDb Series",
			providerIDs: map[string]string{"Tmdb": "67890"},
			itemType:    "Series",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/tv/67890",
			},
		},
		{
			name:        "TMDb Season",
			providerIDs: map[string]string{"Tmdb": "111"},
			itemType:    "Season",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/tv/111",
			},
		},
		{
			name:        "TMDb Episode",
			providerIDs: map[string]string{"Tmdb": "222"},
			itemType:    "Episode",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/tv/222",
			},
		},
		{
			name:        "TMDb Person",
			providerIDs: map[string]string{"Tmdb": "555"},
			itemType:    "Person",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/person/555",
			},
		},
		{
			name:        "TVDB only",
			providerIDs: map[string]string{"Tvdb": "99999"},
			itemType:    "Series",
			want: map[string]string{
				"TVDB": "https://thetvdb.com/?id=99999&tab=series",
			},
		},
		{
			name: "all providers for a Movie",
			providerIDs: map[string]string{
				"Imdb": "tt0000001",
				"Tmdb": "100",
				"Tvdb": "200",
			},
			itemType: "Movie",
			want: map[string]string{
				"IMDb": "https://www.imdb.com/title/tt0000001",
				"TMDb": "https://www.themoviedb.org/movie/100",
				"TVDB": "https://thetvdb.com/?id=200&tab=movie",
			},
		},
		{
			name:        "empty provider IDs",
			providerIDs: map[string]string{},
			itemType:    "Movie",
			want:        map[string]string{},
		},
		{
			name:        "provider with empty ID is skipped",
			providerIDs: map[string]string{"Imdb": "", "Tmdb": "123"},
			itemType:    "Movie",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/movie/123",
			},
		},
		{
			name:        "unknown provider key is ignored",
			providerIDs: map[string]string{"UnknownProvider": "abc"},
			itemType:    "Movie",
			want:        map[string]string{},
		},
		{
			name:        "TMDb with unknown item type defaults to movie segment",
			providerIDs: map[string]string{"Tmdb": "999"},
			itemType:    "BoxSet",
			want: map[string]string{
				"TMDb": "https://www.themoviedb.org/movie/999",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildProviderLinks(tt.providerIDs, tt.itemType)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildProviderLinks(%v, %q) = %v, want %v",
					tt.providerIDs, tt.itemType, got, tt.want)
			}
		})
	}
}

// --- ApplyMetadataFields ---

// The item update endpoint reads the Genres and Tags string arrays. TagItems is
// not a BaseItemDto property, and GenreItems is a response-only projection of
// Genres, so neither is written.
func TestApplyMetadataFields_WritesOnlyFieldsTheUpdateEndpointReads(t *testing.T) {
	fetchedGenreItems := []any{map[string]any{"Name": "Genre A", "Id": "genre-a"}}
	current := map[string]any{
		"Id":         "item-1",
		"Name":       "Test Movie",
		"Genres":     []any{"Genre A"},
		"GenreItems": fetchedGenreItems,
		"Tags":       []any{"tag-a"},
	}

	ApplyMetadataFields(current, MetadataInput{
		Genres:  []string{"Genre B", "Genre C"},
		Tags:    []string{"tag-b"},
		Studios: []string{"Studio A"},
	})

	if got, want := current["Genres"], []string{"Genre B", "Genre C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Genres = %v, want %v", got, want)
	}
	if got, want := current["Tags"], []string{"tag-b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
	if got, want := current["Studios"], []map[string]any{{"Name": "Studio A"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Studios = %v, want %v", got, want)
	}
	if got, ok := current["TagItems"]; ok {
		t.Errorf("TagItems = %v, want it unset", got)
	}
	if got := current["GenreItems"]; !reflect.DeepEqual(got, fetchedGenreItems) {
		t.Errorf("GenreItems = %v, want the fetched value left unchanged", got)
	}
}
