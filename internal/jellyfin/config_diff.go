package jellyfin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// ConfigChange describes one configuration field added or modified by a merge.
type ConfigChange struct {
	Key    string
	Old    any
	New    any
	HasOld bool
}

// AsConfigMap returns v as a JSON object map when v is one.
func AsConfigMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// AsSlice returns v as a JSON array slice when v is one, preserving empty non-nil slices.
func AsSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.([]any); ok {
		return s, true
	}
	return nil, false
}

// MergeConfig returns a copy of base with patch applied on top of it.
// Keys in patch match existing keys in base case-insensitively so canonical
// PascalCase property names from Jellyfin are preserved, and nested non-empty
// object maps are merged recursively.
func MergeConfig(base, patch map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(patch))
	for k, v := range base {
		out[k] = cloneConfigValue(v)
	}
	for _, pk := range sortedMapKeys(patch) {
		pv := patch[pk]
		targetKey := matchingConfigKey(out, pk)
		if existingMap, ok := out[targetKey].(map[string]any); ok {
			if patchMap, pok := pv.(map[string]any); pok && len(patchMap) > 0 {
				out[targetKey] = MergeConfig(existingMap, patchMap)
				continue
			}
		}
		out[targetKey] = cloneConfigValue(pv)
	}
	return out
}

func matchingConfigKey(m map[string]any, key string) string {
	if _, exact := m[key]; exact {
		return key
	}
	var matches []string
	for existing := range m {
		if strings.EqualFold(existing, key) {
			matches = append(matches, existing)
		}
	}
	if len(matches) > 0 {
		sort.Strings(matches)
		return matches[0]
	}
	return key
}

func cloneConfigValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(val))
		for k, item := range val {
			cp[k] = cloneConfigValue(item)
		}
		return cp
	case []any:
		cp := make([]any, len(val))
		for i, item := range val {
			cp[i] = cloneConfigValue(item)
		}
		return cp
	default:
		return v
	}
}

// DiffConfig returns the sorted list of fields that differ between base and merged.
// When base is empty, every field in merged is reported with HasOld=false.
func DiffConfig(base, merged map[string]any) []ConfigChange {
	if len(base) == 0 {
		keys := sortedMapKeys(merged)
		changes := make([]ConfigChange, 0, len(keys))
		for _, k := range keys {
			changes = append(changes, ConfigChange{Key: k, New: merged[k], HasOld: false})
		}
		return changes
	}
	return diffNestedConfig("", base, merged)
}

func diffNestedConfig(prefix string, base, merged map[string]any) []ConfigChange {
	seen := make(map[string]bool, len(base)+len(merged))
	for k := range base {
		seen[k] = true
	}
	for k := range merged {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var changes []ConfigChange
	for _, k := range keys {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		oldVal, hasOld := base[k]
		newVal, hasNew := merged[k]
		if !hasNew || configValuesEqual(oldVal, newVal) {
			continue
		}
		if hasOld {
			oldMap, okOld := oldVal.(map[string]any)
			newMap, okNew := newVal.(map[string]any)
			if okOld && okNew && len(newMap) > 0 {
				changes = append(changes, diffNestedConfig(fullKey, oldMap, newMap)...)
				continue
			}
		}
		changes = append(changes, ConfigChange{
			Key:    fullKey,
			Old:    oldVal,
			New:    newVal,
			HasOld: hasOld,
		})
	}
	return changes
}

func configValuesEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(aj) == string(bj)
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// maxShownConfigChanges caps how many individual field diffs appear in a single
// confirmation prompt before summarizing the remainder.
const maxShownConfigChanges = 6

// FormatConfigChanges renders ConfigChange entries into concise human-readable text
// suitable for a confirmation prompt, masking secret-looking string values.
func FormatConfigChanges(changes []ConfigChange) string {
	if len(changes) == 0 {
		return "none"
	}
	limit := min(len(changes), maxShownConfigChanges)
	parts := make([]string, 0, limit+1)
	for i := range limit {
		c := changes[i]
		newText := formatConfigValue(c.Key, c.New)
		if c.HasOld {
			oldText := formatConfigValue(c.Key, c.Old)
			parts = append(parts, fmt.Sprintf("%s: %s -> %s", c.Key, oldText, newText))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", c.Key, newText))
		}
	}
	if rest := len(changes) - limit; rest > 0 {
		parts = append(parts, fmt.Sprintf("and %d more", rest))
	}
	return strings.Join(parts, ", ")
}

func formatConfigValue(key string, v any) string {
	if v == nil {
		return "null"
	}
	if isSecretConfigKey(key) {
		switch val := v.(type) {
		case string:
			if val == "" {
				return `""`
			}
		case []any:
			if len(val) == 0 {
				return "[]"
			}
		case []string:
			if len(val) == 0 {
				return "[]"
			}
		case map[string]any:
			if len(val) == 0 {
				return "{}"
			}
		}
		return "(hidden)"
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	case string:
		return formatPromptString(key, val, 50)
	case []any:
		return formatConfigSlice(key, val)
	case []string:
		anySlice := make([]any, len(val))
		for i, s := range val {
			anySlice[i] = s
		}
		return formatConfigSlice(key, anySlice)
	case map[string]any:
		if len(val) == 0 {
			return "{}"
		}
		if len(val) == 1 {
			return "1 field"
		}
		return fmt.Sprintf("%d fields", len(val))
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

func formatPromptString(key, val string, maxRunes int) string {
	if shouldSanitizePromptURL(key, val) {
		return fmt.Sprintf("%q", sanitizePromptURL(val))
	}
	if len([]rune(val)) > maxRunes {
		return fmt.Sprintf("%q", Truncate(val, maxRunes)+"…")
	}
	return fmt.Sprintf("%q", val)
}

func formatConfigSlice(key string, items []any) string {
	if len(items) == 0 {
		return "[]"
	}
	if len(items) <= 3 {
		scalars := make([]string, 0, len(items))
		allScalar := true
		for _, item := range items {
			switch s := item.(type) {
			case string:
				if shouldSanitizePromptURL(key, s) {
					scalars = append(scalars, fmt.Sprintf("%q", sanitizePromptURL(s)))
				} else if len([]rune(s)) > 30 {
					allScalar = false
				} else {
					scalars = append(scalars, fmt.Sprintf("%q", s))
				}
			case bool, float64, int, int64:
				scalars = append(scalars, fmt.Sprintf("%v", s))
			default:
				allScalar = false
			}
		}
		if allScalar {
			return "[" + strings.Join(scalars, ", ") + "]"
		}
	}
	if len(items) == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", len(items))
}

func isSecretConfigKey(key string) bool {
	lower := strings.ToLower(key)
	if idx := strings.LastIndexByte(lower, '.'); idx >= 0 {
		lower = lower[idx+1:]
	}
	for _, marker := range []string{
		"password", "passwd", "secret", "token", "apikey", "api_key", "passphrase",
		"credential", "cookie", "privatekey", "private_key", "encryptionkey", "signingkey", "streamkey", "accesskey", "authkey",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isURLConfigKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, "url") || strings.HasSuffix(lower, "uri")
}

func shouldSanitizePromptURL(key, val string) bool {
	if !strings.Contains(val, "://") {
		return false
	}
	if isURLConfigKey(key) {
		return true
	}
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.User != nil || u.RawQuery != "" || u.Fragment != ""
}

func sanitizePromptURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(URL)"
	}
	origin := u.Scheme + "://" + u.Host
	if u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") {
		return origin
	}
	return origin + "/…"
}
