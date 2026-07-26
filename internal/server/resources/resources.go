package resources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// AdminResourcesEnabled applies the admin tool surface to resources.
func AdminResourcesEnabled(readOnly bool, toolsets string) bool {
	if readOnly {
		return false
	}
	if toolsets == "" {
		return true
	}
	for _, toolset := range strings.Split(toolsets, ",") {
		if strings.TrimSpace(toolset) == "admin" {
			return true
		}
	}
	return false
}

func RegisterResources(server *mcp.Server, client jf.Client, includeAdmin bool) {

	// --- jellyfin://server/info ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://server/info",
		Name:        "Jellyfin Server Info",
		Title:       "Server Info",
		Description: "Server name, version, OS, and network details",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.3},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var info map[string]any
		if err := client.Get(ctx, "/System/Info", nil, &info); err != nil {
			return nil, fmt.Errorf("failed to get server info: %w", err)
		}
		result := map[string]any{
			"server_name":              jf.GetString(info, "ServerName"),
			"version":                  jf.GetString(info, "Version"),
			"os":                       jf.GetString(info, "OperatingSystem"),
			"id":                       jf.GetString(info, "Id"),
			"startup_wizard_completed": jf.GetBool(info, "StartupWizardCompleted"),
		}
		if lo := jf.GetString(info, "LocalAddress"); lo != "" {
			result["local_address"] = lo
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://server/info",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(result),
			}},
		}, nil
	})

	// --- jellyfin://libraries ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://libraries",
		Name:        "Media Libraries",
		Title:       "Libraries",
		Description: "All configured media libraries with IDs, collection types, and paths",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.8},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var libs []map[string]any
		if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
			return nil, fmt.Errorf("failed to get libraries: %w", err)
		}
		items := jf.ExtractLibraries(libs)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://libraries",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://sessions/now-playing ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://sessions/now-playing",
		Name:        "Now Playing",
		Title:       "Now Playing",
		Description: "Sessions with active playback only (excludes idle/connected sessions with no media playing)",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var sessions []map[string]any
		if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
			return nil, fmt.Errorf("failed to get sessions: %w", err)
		}
		items := make([]map[string]any, 0, len(sessions))
		for _, s := range sessions {
			if jf.ToMap(s["NowPlayingItem"]) != nil {
				items = append(items, jf.ExtractSessionInfo(s))
			}
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://sessions/now-playing",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://resume ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://resume",
		Name:        "Resume Watching",
		Title:       "Resume",
		Description: "Items with in-progress playback that can be continued",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.6},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"Limit":     {"15"},
			"Recursive": {"true"},
			"Filters":   {"IsResumable"},
			"SortBy":    {"DatePlayed"},
			"SortOrder": {"Descending"},
			"Fields":    {"Overview,ProductionYear,CommunityRating"},
		}
		var result map[string]any
		endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
		if err := client.Get(ctx, endpoint, params, &result); err != nil {
			return nil, fmt.Errorf("failed to get resumable items: %w", err)
		}
		items := jf.ExtractItemList(result)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://resume",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://next-up ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://next-up",
		Name:        "Next Up",
		Title:       "Next Up",
		Description: "Next episodes to watch in series that are in progress",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.6},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"Limit":  {"20"},
			"UserId": {userID},
			"Fields": {"Overview,ProductionYear,CommunityRating"},
		}
		var result map[string]any
		if err := client.Get(ctx, "/Shows/NextUp", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get next up: %w", err)
		}
		items := jf.ExtractItemList(result)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://next-up",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://favorites ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://favorites",
		Name:        "Favorites",
		Title:       "Favorites",
		Description: "Items marked as favorite by the current user",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"Limit":      {"50"},
			"Recursive":  {"true"},
			"IsFavorite": {"true"},
			"SortBy":     {"SortName"},
			"SortOrder":  {"Ascending"},
			"Fields":     {"Overview,ProductionYear,CommunityRating"},
		}
		var result map[string]any
		endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
		if err := client.Get(ctx, endpoint, params, &result); err != nil {
			return nil, fmt.Errorf("failed to get favorites: %w", err)
		}
		items := jf.ExtractItemList(result)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://favorites",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://latest ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://latest",
		Name:        "Recently Added",
		Title:       "Latest",
		Description: "Recently added items across all libraries",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"Limit":  {"20"},
			"UserId": {userID},
			"Fields": {"Overview,ProductionYear,CommunityRating"},
		}
		var result []map[string]any
		if err := client.Get(ctx, "/Items/Latest", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get latest items: %w", err)
		}
		items := make([]map[string]any, 0, len(result))
		for _, raw := range result {
			items = append(items, jf.ExtractMediaItem(raw))
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://latest",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://recently-played ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://recently-played",
		Name:        "Recently Played",
		Title:       "Recently Played",
		Description: "Items recently watched or listened to (verified playback only), sorted by play date",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Build set of item IDs with actual playback events from activity log
		playedViaPlayback := map[string]bool{}
		logItems, _, logErr := jf.FetchAllPages(ctx, client, "/System/ActivityLog/Entries", url.Values{}, 2000)
		if logErr == nil {
			for _, raw := range logItems {
				m := jf.ToMap(raw)
				if jf.GetString(m, "Type") != "VideoPlaybackStopped" {
					continue
				}
				if jf.GetString(m, "UserId") != userID {
					continue
				}
				if itemID := jf.GetString(m, "ItemId"); itemID != "" {
					playedViaPlayback[itemID] = true
				}
			}
		}

		params := url.Values{
			"Limit":     {"100"},
			"Recursive": {"true"},
			"IsPlayed":  {"true"},
			"SortBy":    {"DatePlayed"},
			"SortOrder": {"Descending"},
			"Fields":    {"Overview,ProductionYear,CommunityRating,UserData"},
		}
		var result map[string]any
		endpoint := fmt.Sprintf("/Users/%s/Items", jf.SanitizeID(userID))
		if err := client.Get(ctx, endpoint, params, &result); err != nil {
			return nil, fmt.Errorf("failed to get recently played: %w", err)
		}
		rawItems := jf.ToSlice(result["Items"])
		items := make([]map[string]any, 0, len(rawItems))
		for _, raw := range rawItems {
			m := jf.ToMap(raw)
			item := jf.ExtractMediaItem(m)
			id, _ := item["id"].(string)
			if !playedViaPlayback[id] {
				continue
			}
			if ud := jf.ToMap(m["UserData"]); ud != nil {
				if lp := jf.GetString(ud, "LastPlayedDate"); lp != "" {
					item["last_played"] = jf.Truncate(lp, jf.DateOnlyLen)
				}
			}
			items = append(items, item)
			if len(items) >= 25 {
				break
			}
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://recently-played",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://sessions ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://sessions",
		Name:        "All Sessions",
		Title:       "Sessions",
		Description: "All connected client sessions with playback status",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.4},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var sessions []map[string]any
		if err := client.Get(ctx, "/Sessions", nil, &sessions); err != nil {
			return nil, fmt.Errorf("failed to get sessions: %w", err)
		}
		items := make([]map[string]any, 0, len(sessions))
		for _, s := range sessions {
			info := jf.ExtractSessionInfo(s)
			if jf.ToMap(s["NowPlayingItem"]) != nil {
				info["status"] = "playing"
			} else {
				info["status"] = "connected"
			}
			items = append(items, info)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://sessions",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://users ---
	if includeAdmin {
		server.AddResource(&mcp.Resource{
			URI:         "jellyfin://users",
			Name:        "User Accounts",
			Title:       "Users",
			Description: "All user accounts with admin status and last activity",
			MIMEType:    "application/json",
			Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.4},
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			var users []map[string]any
			if err := client.Get(ctx, "/Users", nil, &users); err != nil {
				return nil, fmt.Errorf("failed to get users: %w", err)
			}
			items := make([]map[string]any, 0, len(users))
			for _, u := range users {
				items = append(items, jf.ExtractUserSummary(u))
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      "jellyfin://users",
					MIMEType: "application/json",
					Text:     jf.FormatJSON(items),
				}},
			}, nil
		})
	}

	// --- Reference Guides ---
	registerGuides(server)

	// --- jellyfin://items/{itemId} (template) ---
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "jellyfin://items/{itemId}",
		Name:        "Media Item",
		Title:       "Media Item",
		Description: "Detailed metadata for any media item by its Jellyfin ID",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.7},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		itemID := strings.TrimPrefix(req.Params.URI, "jellyfin://items/")
		if itemID == "" {
			return nil, fmt.Errorf("item ID is required in URI")
		}
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		var result map[string]any
		endpoint := fmt.Sprintf("/Users/%s/Items/%s", jf.SanitizeID(userID), jf.SanitizeID(itemID))
		if err := client.Get(ctx, endpoint, nil, &result); err != nil {
			return nil, fmt.Errorf("failed to get item: %w", err)
		}
		item := jf.ExtractDetailedItem(result)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     jf.FormatJSON(item),
			}},
		}, nil
	})

	// --- jellyfin://users/{userId} (template) ---
	if includeAdmin {
		server.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: "jellyfin://users/{userId}",
			Name:        "User Profile",
			Title:       "User Profile",
			Description: "User account details and policy settings",
			MIMEType:    "application/json",
			Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			userID := strings.TrimPrefix(req.Params.URI, "jellyfin://users/")
			if userID == "" {
				return nil, fmt.Errorf("user ID is required in URI")
			}
			var user map[string]any
			endpoint := fmt.Sprintf("/Users/%s", jf.SanitizeID(userID))
			if err := client.Get(ctx, endpoint, nil, &user); err != nil {
				return nil, fmt.Errorf("failed to get user: %w", err)
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     jf.FormatJSON(jf.ExtractDetailedUser(user)),
				}},
			}, nil
		})
	}

	// --- jellyfin://libraries/{libraryId}/latest (template) ---
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "jellyfin://libraries/{libraryId}/latest",
		Name:        "Library Latest",
		Title:       "Library Latest",
		Description: "Recently added items in a specific library",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		libraryID := strings.TrimPrefix(req.Params.URI, "jellyfin://libraries/")
		libraryID = strings.TrimSuffix(libraryID, "/latest")
		if libraryID == "" {
			return nil, fmt.Errorf("library ID is required in URI")
		}
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"Limit":    {"20"},
			"UserId":   {userID},
			"ParentId": {jf.SanitizeID(libraryID)},
			"Fields":   {"Overview,ProductionYear,CommunityRating"},
		}
		var result []map[string]any
		if err := client.Get(ctx, "/Items/Latest", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get library latest: %w", err)
		}
		items := make([]map[string]any, 0, len(result))
		for _, raw := range result {
			items = append(items, jf.ExtractMediaItem(raw))
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})
}
