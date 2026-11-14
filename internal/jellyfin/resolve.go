package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxAvailableNames      = 20
	excludedTitleItemTypes = "Folder,CollectionFolder,AggregateFolder,UserRootFolder,UserView,BoxSet,Playlist,Person,MusicArtist,Studio,Genre,Photo,PhotoAlbum"
)

// IsJellyfinID reports whether s is a 32-hex-digit Jellyfin GUID or a
// canonical 36-character dashed UUID (8-4-4-4-12).
func IsJellyfinID(s string) bool {
	trimmed := strings.TrimSpace(s)
	switch len(trimmed) {
	case 32:
		return isHexASCII(trimmed)
	case 36:
		if trimmed[8] != '-' || trimmed[13] != '-' || trimmed[18] != '-' || trimmed[23] != '-' {
			return false
		}
		return isHexASCII(trimmed[0:8]) &&
			isHexASCII(trimmed[9:13]) &&
			isHexASCII(trimmed[14:18]) &&
			isHexASCII(trimmed[19:23]) &&
			isHexASCII(trimmed[24:36])
	default:
		return false
	}
}

func isHexASCII(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func shouldPropagateResolveErr(ctx context.Context, err error, allowedStatuses ...int) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return !slices.Contains(allowedStatuses, apiErr.StatusCode)
	}
	var noResp noResponseError
	var netErr net.Error
	var synErr *json.SyntaxError
	return errors.As(err, &noResp) || errors.As(err, &netErr) || errors.As(err, &synErr)
}

func formatAvailableNames(names []string) string {
	if len(names) <= maxAvailableNames {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:maxAvailableNames], ", "), len(names)-maxAvailableNames)
}

// ResolveLibraryID resolves a library name or ID to its Jellyfin ItemId.
// Standard 32-hex GUIDs return immediately with no network request.
// If /Library/VirtualFolders returns nothing (for example in minimal unit-test mocks),
// raw is returned unchanged.
func ResolveLibraryID(ctx context.Context, client Client, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || IsJellyfinID(trimmed) {
		return trimmed, nil
	}
	var libs []map[string]any
	if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
		if shouldPropagateResolveErr(ctx, err, 404) {
			return "", err
		}
		return trimmed, nil
	}
	if len(libs) == 0 {
		return trimmed, nil
	}
	return matchLibrary(trimmed, libs)
}

// ResolveLibraryIDs resolves a slice of library names or IDs to their Jellyfin ItemIds,
// fetching /Library/VirtualFolders once so callers also receive the ID-to-Name map
// for human-readable confirmation prompts.
func ResolveLibraryIDs(ctx context.Context, client Client, raws []string) ([]string, map[string]string, error) {
	if len(raws) == 0 {
		return nil, nil, nil
	}
	var libs []map[string]any
	if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
		if shouldPropagateResolveErr(ctx, err, 404) {
			return nil, nil, err
		}
		out := make([]string, len(raws))
		copy(out, raws)
		return out, nil, nil
	}
	if len(libs) == 0 {
		out := make([]string, len(raws))
		copy(out, raws)
		return out, nil, nil
	}
	namesByID := make(map[string]string, len(libs)*2)
	for _, lib := range libs {
		id := GetString(lib, "ItemId")
		name := GetString(lib, "Name")
		if id != "" && name != "" {
			namesByID[id] = name
			namesByID[NormalizeID(id)] = name
		}
	}
	out := make([]string, 0, len(raws))
	for _, raw := range raws {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if IsJellyfinID(trimmed) {
			out = append(out, trimmed)
			continue
		}
		id, err := matchLibrary(trimmed, libs)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, id)
	}
	return out, namesByID, nil
}

func matchLibrary(target string, libs []map[string]any) (string, error) {
	normTarget := NormalizeID(target)
	for _, lib := range libs {
		id := GetString(lib, "ItemId")
		if id != "" && (id == target || NormalizeID(id) == normTarget) {
			return id, nil
		}
	}

	var exactIDs []string
	var foldIDs []string
	var foldNames []string
	var available []string
	for _, lib := range libs {
		id := GetString(lib, "ItemId")
		name := GetString(lib, "Name")
		if name != "" {
			available = append(available, name)
		}
		if id == "" || name == "" {
			continue
		}
		if name == target {
			exactIDs = append(exactIDs, id)
		}
		if strings.EqualFold(name, target) {
			foldIDs = append(foldIDs, id)
			foldNames = append(foldNames, name)
		}
	}
	if len(exactIDs) == 1 {
		return exactIDs[0], nil
	}
	if len(exactIDs) > 1 {
		return "", fmt.Errorf("multiple libraries are named '%s'; pass the library's item_id from jellyfin_libraries", target)
	}
	if len(foldIDs) == 1 {
		return foldIDs[0], nil
	}
	if len(foldIDs) > 1 {
		return "", fmt.Errorf("library name '%s' matches multiple libraries (%s); pass the exact name or item_id from jellyfin_libraries", target, strings.Join(foldNames, ", "))
	}
	if len(available) > 0 {
		return "", fmt.Errorf("no library matches '%s' (available libraries: %s); pass a library name or item ID", target, formatAvailableNames(available))
	}
	return target, nil
}

// EffectiveUserID returns raw resolved via ResolveUserID when non-empty, or
// falls back to client.GetUserID when raw is empty or whitespace.
func EffectiveUserID(ctx context.Context, client Client, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return client.GetUserID(ctx)
	}
	return ResolveUserID(ctx, client, raw)
}

// ResolveUserID resolves a username or user ID to a Jellyfin user ID.
// Standard 32-hex GUIDs return immediately with no network request.
func ResolveUserID(ctx context.Context, client Client, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || IsJellyfinID(trimmed) {
		return trimmed, nil
	}
	var users []map[string]any
	if err := client.Get(ctx, "/Users", nil, &users); err != nil {
		if shouldPropagateResolveErr(ctx, err, 403, 404) {
			return "", err
		}
		return trimmed, nil
	}
	if len(users) == 0 {
		return trimmed, nil
	}
	u, err := matchUser(trimmed, users)
	if err != nil {
		return "", err
	}
	if id := GetString(u, "Id"); id != "" {
		return id, nil
	}
	return trimmed, nil
}

// ResolveUser looks up a user by ID or username, returning their canonical ID,
// display name, and raw user object. For 32-hex GUIDs it queries GET /Users/{id}
// directly; for usernames it queries GET /Users first (avoiding a guaranteed 400
// on /Users/{username}) and falls back to GET /Users/{raw} only when /Users is
// unavailable or empty.
func ResolveUser(ctx context.Context, client Client, raw string) (id string, name string, user map[string]any, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", nil, fmt.Errorf("user_id is required")
	}

	if !IsJellyfinID(trimmed) {
		var users []map[string]any
		listErr := client.Get(ctx, "/Users", nil, &users)
		if listErr == nil && len(users) > 0 {
			matched, matchErr := matchUser(trimmed, users)
			if matchErr != nil {
				return "", "", nil, matchErr
			}
			uid := GetString(matched, "Id")
			if uid == "" {
				uid = trimmed
			}
			return uid, GetString(matched, "Name"), matched, nil
		}
		if shouldPropagateResolveErr(ctx, listErr, 400, 403, 404) {
			return "", "", nil, listErr
		}
	}

	var u map[string]any
	getErr := client.Get(ctx, fmt.Sprintf("/Users/%s", SanitizeID(trimmed)), nil, &u)
	if getErr == nil && len(u) > 0 {
		uid := GetString(u, "Id")
		if uid == "" {
			uid = trimmed
		}
		return uid, GetString(u, "Name"), u, nil
	}
	if getErr != nil {
		var apiErr *APIError
		if !errors.As(getErr, &apiErr) || (apiErr.StatusCode != 404 && apiErr.StatusCode != 400) {
			return "", "", nil, getErr
		}
		return "", "", nil, fmt.Errorf("no user has ID '%s'; use jellyfin_users action=list to find user IDs", trimmed)
	}
	return trimmed, "", map[string]any{}, nil
}

func matchUser(target string, users []map[string]any) (map[string]any, error) {
	normTarget := NormalizeID(target)
	for _, u := range users {
		id := GetString(u, "Id")
		if id != "" && (id == target || NormalizeID(id) == normTarget) {
			return u, nil
		}
	}

	var exact []map[string]any
	var fold []map[string]any
	var foldNames []string
	var available []string
	for _, u := range users {
		name := GetString(u, "Name")
		if name == "" {
			continue
		}
		available = append(available, name)
		if name == target {
			exact = append(exact, u)
		}
		if strings.EqualFold(name, target) {
			fold = append(fold, u)
			foldNames = append(foldNames, name)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return nil, fmt.Errorf("multiple users are named '%s'; use jellyfin_users action=list to get the exact user_id", target)
	}
	if len(fold) == 1 {
		return fold[0], nil
	}
	if len(fold) > 1 {
		return nil, fmt.Errorf("username '%s' matches multiple users (%s); pass the exact user_id from jellyfin_users action=list", target, strings.Join(foldNames, ", "))
	}
	if len(available) > 0 {
		return nil, fmt.Errorf("no user has ID '%s' (available users: %s); use jellyfin_users action=list to find user IDs", target, formatAvailableNames(available))
	}
	return nil, fmt.Errorf("no user has ID '%s'; use jellyfin_users action=list to find user IDs", target)
}

// stripTrailingYear removes a trailing " (YYYY)" release-year suffix when
// present so Jellyfin's SearchTerm matches the bare title while matchNamedItem
// still disambiguates against the full "Title (YYYY)" target.
func stripTrailingYear(s string) string {
	if len(s) < 7 || s[len(s)-1] != ')' {
		return s
	}
	open := strings.LastIndexByte(s, '(')
	if open <= 0 || len(s)-open != 6 || s[open-1] != ' ' {
		return s
	}
	for i := open + 1; i < len(s)-1; i++ {
		if s[i] < '0' || s[i] > '9' {
			return s
		}
	}
	return strings.TrimSpace(s[:open])
}

// ResolveSeriesID resolves a TV series name or ID to its Jellyfin ItemId.
// Standard 32-hex GUIDs return immediately with no network request.
// If /Items returns no envelope (for example in minimal unit-test mocks),
// raw is returned unchanged.
func ResolveSeriesID(ctx context.Context, client Client, userID, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || IsJellyfinID(trimmed) {
		return trimmed, nil
	}
	searchTerm := stripTrailingYear(trimmed)
	params := url.Values{
		"IncludeItemTypes": {"Series"},
		"Recursive":        {"true"},
		"SearchTerm":       {searchTerm},
		"Limit":            {"100"},
	}
	if userID != "" {
		params.Set("UserId", userID)
	}
	var resp map[string]any
	if err := client.Get(ctx, "/Items", params, &resp); err != nil {
		if shouldPropagateResolveErr(ctx, err, 400, 404) {
			return "", err
		}
		return trimmed, nil
	}
	if resp == nil || resp["Items"] == nil {
		return trimmed, nil
	}
	items := ToSlice(resp["Items"])
	if len(items) == 0 {
		// Fall back to listing series without SearchTerm so punctuation-heavy
		// titles still match and non-existent names report available series.
		fallbackParams := url.Values{
			"IncludeItemTypes": {"Series"},
			"Recursive":        {"true"},
			"Limit":            {"200"},
		}
		if userID != "" {
			fallbackParams.Set("UserId", userID)
		}
		var fallback map[string]any
		err := client.Get(ctx, "/Items", fallbackParams, &fallback)
		if shouldPropagateResolveErr(ctx, err, 400, 404) {
			return "", err
		}
		if err == nil && fallback != nil && fallback["Items"] != nil {
			items = fetchPagedTypedItems(ctx, client, fallbackParams, fallback)
		}
	}
	return matchNamedItem(trimmed, items, "series", "series", "use jellyfin_search with type=Series to find series IDs")
}

// ResolvePlaylistID resolves a playlist name or ID to its Jellyfin ItemId.
// Standard 32-hex GUIDs return immediately with no network request.
// If /Items returns no envelope (for example in minimal unit-test mocks),
// raw is returned unchanged.
func ResolvePlaylistID(ctx context.Context, client Client, userID, raw string) (string, error) {
	return resolveTypedItemID(ctx, client, userID, raw, "Playlist", "playlist", "playlists", "use jellyfin_playlists action=list to find playlists")
}

// ResolveCollectionID resolves a BoxSet collection name or ID to its Jellyfin ItemId.
// Standard 32-hex GUIDs return immediately with no network request.
// If /Items returns no envelope (for example in minimal unit-test mocks),
// raw is returned unchanged.
func ResolveCollectionID(ctx context.Context, client Client, userID, raw string) (string, error) {
	return resolveTypedItemID(ctx, client, userID, raw, "BoxSet", "collection", "collections", "use jellyfin_browse with type=BoxSet to find collections")
}

// ResolveItemByTitle resolves a human-readable media item title (optionally
// suffixed with " (YYYY)") to its Jellyfin ItemId via /Items SearchTerm.
func ResolveItemByTitle(ctx context.Context, client Client, userID, raw string) (string, error) {
	return ResolveItemByTitleTyped(ctx, client, userID, raw, "")
}

// ResolveItemByTitleTyped resolves a human-readable media item title
// (optionally suffixed with " (YYYY)") to its Jellyfin ItemId via /Items
// SearchTerm, optionally narrowing to itemType (for example "Movie" or "Series").
func ResolveItemByTitleTyped(ctx context.Context, client Client, userID, raw, itemType string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || IsJellyfinID(trimmed) {
		return trimmed, nil
	}
	wantType := strings.TrimSpace(itemType)
	params := url.Values{
		"Recursive":  {"true"},
		"SearchTerm": {stripTrailingYear(trimmed)},
		"Limit":      {"100"},
	}
	if wantType != "" {
		params.Set("IncludeItemTypes", wantType)
	} else {
		params.Set("ExcludeItemTypes", excludedTitleItemTypes)
	}
	if userID != "" {
		params.Set("UserId", userID)
	}
	var resp map[string]any
	if err := client.Get(ctx, "/Items", params, &resp); err != nil {
		return "", err
	}
	var filtered []any
	for _, elem := range ToSlice(resp["Items"]) {
		m := ToMap(elem)
		typ := GetString(m, "Type")
		if wantType != "" {
			if typ != "" && !strings.EqualFold(typ, wantType) {
				continue
			}
		} else {
			switch typ {
			case "Folder", "CollectionFolder", "AggregateFolder", "UserRootFolder", "UserView",
				"BoxSet", "Playlist", "Person", "MusicArtist", "Studio", "Genre", "Photo", "PhotoAlbum":
				continue
			}
		}
		filtered = append(filtered, elem)
	}
	hint := "use jellyfin_search to find the exact item_id"
	if wantType == "" {
		hint = "pass type (such as Movie or Series) or use jellyfin_search to find the exact item_id"
	}
	return matchNamedItem(trimmed, filtered, "item", "items", hint)
}

func resolveTypedItemID(ctx context.Context, client Client, userID, raw, itemType, singular, pluralWord, hint string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || IsJellyfinID(trimmed) {
		return trimmed, nil
	}
	params := url.Values{
		"IncludeItemTypes": {itemType},
		"Recursive":        {"true"},
		"Limit":            {"200"},
	}
	if userID != "" {
		params.Set("UserId", userID)
	}
	var resp map[string]any
	if err := client.Get(ctx, "/Items", params, &resp); err != nil {
		if shouldPropagateResolveErr(ctx, err, 400, 404) {
			return "", err
		}
		return trimmed, nil
	}
	if resp == nil || resp["Items"] == nil {
		return trimmed, nil
	}
	items := fetchPagedTypedItems(ctx, client, params, resp)
	return matchNamedItem(trimmed, items, singular, pluralWord, hint)
}

func fetchPagedTypedItems(ctx context.Context, client Client, params url.Values, firstResp map[string]any) []any {
	items := ToSlice(firstResp["Items"])
	if total := GetInt(firstResp, "TotalRecordCount"); total > len(items) && len(items) > 0 {
		for len(items) < total && len(items) < DefaultMaxItems {
			params.Set("StartIndex", fmt.Sprintf("%d", len(items)))
			var page map[string]any
			if err := client.Get(ctx, "/Items", params, &page); err != nil || page == nil {
				break
			}
			more := ToSlice(page["Items"])
			if len(more) == 0 {
				break
			}
			items = append(items, more...)
		}
	}
	return items
}

func matchNamedItem(target string, rawItems []any, singular, pluralWord, hint string) (string, error) {
	normTarget := NormalizeID(target)
	for _, raw := range rawItems {
		m := ToMap(raw)
		id := GetString(m, "Id")
		if id != "" && (id == target || NormalizeID(id) == normTarget) {
			return id, nil
		}
	}

	var exactIDs []string
	var exactLabels []string
	var foldIDs []string
	var foldLabels []string
	var yearExactIDs []string
	var yearExactLabels []string
	var yearFoldIDs []string
	var yearFoldLabels []string
	var available []string
	for _, raw := range rawItems {
		m := ToMap(raw)
		id := GetString(m, "Id")
		name := GetString(m, "Name")
		if name == "" {
			continue
		}
		year := GetInt(m, "ProductionYear")
		availLabel := name
		if year > 0 {
			availLabel = fmt.Sprintf("%s (%d)", name, year)
		}
		typeDesc := ""
		if singular == "item" {
			typeDesc = GetString(m, "Type")
			if typeDesc == "Episode" {
				if series := GetString(m, "SeriesName"); series != "" {
					typeDesc = fmt.Sprintf("Episode of %s", series)
				}
			}
		}
		var disambigParts []string
		if typeDesc != "" {
			disambigParts = append(disambigParts, typeDesc)
		}
		if year > 0 {
			disambigParts = append(disambigParts, fmt.Sprintf("%d", year))
		}
		disambigParts = append(disambigParts, fmt.Sprintf("id=%s", id))
		disambigLabel := fmt.Sprintf("%s (%s)", name, strings.Join(disambigParts, ", "))
		available = append(available, availLabel)
		if id == "" {
			continue
		}
		if name == target {
			exactIDs = append(exactIDs, id)
			exactLabels = append(exactLabels, disambigLabel)
		}
		if strings.EqualFold(name, target) {
			foldIDs = append(foldIDs, id)
			foldLabels = append(foldLabels, disambigLabel)
		}
		if year > 0 {
			if availLabel == target {
				yearExactIDs = append(yearExactIDs, id)
				yearExactLabels = append(yearExactLabels, disambigLabel)
			}
			if strings.EqualFold(availLabel, target) {
				yearFoldIDs = append(yearFoldIDs, id)
				yearFoldLabels = append(yearFoldLabels, disambigLabel)
			}
		}
	}
	if len(exactIDs) == 1 {
		return exactIDs[0], nil
	}
	if len(exactIDs) > 1 {
		return "", fmt.Errorf("multiple %s are named '%s' (%s); %s", pluralWord, target, strings.Join(exactLabels, ", "), hint)
	}
	if len(foldIDs) == 1 {
		return foldIDs[0], nil
	}
	if len(foldIDs) > 1 {
		return "", fmt.Errorf("%s name '%s' matches multiple %s (%s); %s", singular, target, pluralWord, strings.Join(foldLabels, ", "), hint)
	}
	if len(yearExactIDs) == 1 {
		return yearExactIDs[0], nil
	}
	if len(yearExactIDs) > 1 {
		return "", fmt.Errorf("multiple %s match '%s' (%s); %s", pluralWord, target, strings.Join(yearExactLabels, ", "), hint)
	}
	if len(yearFoldIDs) == 1 {
		return yearFoldIDs[0], nil
	}
	if len(yearFoldIDs) > 1 {
		return "", fmt.Errorf("%s name '%s' matches multiple %s (%s); %s", singular, target, pluralWord, strings.Join(yearFoldLabels, ", "), hint)
	}
	if len(available) > 0 {
		return "", fmt.Errorf("no %s matches '%s' (available %s: %s); %s", singular, target, pluralWord, formatAvailableNames(available), hint)
	}
	return "", fmt.Errorf("no %s matches '%s'; %s", singular, target, hint)
}

// ResolveErrResult formats an error from a Resolve* helper as an MCP tool error.
// Transport and API errors keep the standard "Jellyfin error: ..." prefix, while
// user-facing name/ID resolution errors are formatted as capitalized sentences.
func ResolveErrResult(err error) *mcp.CallToolResult {
	var apiErr *APIError
	var noResp noResponseError
	var netErr net.Error
	var synErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &apiErr) ||
		errors.As(err, &noResp) ||
		errors.As(err, &netErr) ||
		errors.As(err, &synErr) ||
		errors.As(err, &typeErr) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return ErrResult("Jellyfin error: %v", err)
	}
	msg := err.Error()
	if msg != "" && msg[0] >= 'a' && msg[0] <= 'z' {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	if msg != "" && !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return ErrResult("%s", msg)
}
