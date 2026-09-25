package resources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterResources(server *mcp.Server, client jf.Client) {

	// --- jellyfin://server/info ---
	server.AddResource(&mcp.Resource{
		URI:         "jellyfin://server/info",
		Name:        "Jellyfin Server Info",
		Title:       "Server Info",
		Description: "Server name, version, ID, whether the startup wizard is complete, and the local address",
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
		items := jf.LibrariesFrom(libs)
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
		items := make([]jf.SessionInfo, 0, len(sessions))
		for _, s := range sessions {
			if jf.ToMap(s["NowPlayingItem"]) != nil {
				items = append(items, jf.SessionFrom(s))
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
		Description: "Up to 15 items with in-progress playback that can be continued",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.6},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		// /UserItems/Resume lists what the user is partway through, most
		// recently played first, leaving out what a session is playing now.
		params := url.Values{
			"UserId": {userID},
			"Limit":  {"15"},
			"Fields": {"Overview"},
		}
		var result map[string]any
		if err := client.Get(ctx, "/UserItems/Resume", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get resumable items: %w", err)
		}
		items := jf.ItemsFrom(result)
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
		Description: "Up to 20 next episodes to watch in series that are in progress",
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
			"Fields": {"Overview"},
		}
		var result map[string]any
		if err := client.Get(ctx, "/Shows/NextUp", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get next up: %w", err)
		}
		items := jf.ItemsFrom(result)
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
		Description: "Up to 50 items marked as favorite by the current user",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}
		params := url.Values{
			"UserId":     {userID},
			"Limit":      {"50"},
			"Recursive":  {"true"},
			"IsFavorite": {"true"},
			"SortBy":     {"SortName"},
			"SortOrder":  {"Ascending"},
			"Fields":     {"Overview"},
		}
		var result map[string]any
		if err := client.Get(ctx, "/Items", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get favorites: %w", err)
		}
		items := jf.ItemsFrom(result)
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
		Description: "Up to 20 recently added items across all libraries",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		items, err := jf.FetchLatest(ctx, client, "", jf.LatestItemsLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to get latest items: %w", err)
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
		Description: "Up to 25 items recently watched or listened to, newest first. It lists only playback verified in the activity log, so items marked played by hand are left out",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		userID, err := client.GetUserID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		playedViaPlayback, err := jf.PlayedItemIDs(ctx, client, userID)
		if err != nil {
			return nil, err
		}

		params := url.Values{
			"UserId":    {userID},
			"Limit":     {"100"},
			"Recursive": {"true"},
			"IsPlayed":  {"true"},
			"SortBy":    {"DatePlayed"},
			"SortOrder": {"Descending"},
			"Fields":    {"Overview"},
		}
		var result map[string]any
		if err := client.Get(ctx, "/Items", params, &result); err != nil {
			return nil, fmt.Errorf("failed to get recently played: %w", err)
		}
		rawItems := jf.ToSlice(result["Items"])
		items := make([]jf.MediaItem, 0, len(rawItems))
		for _, raw := range rawItems {
			m := jf.ToMap(raw)
			item := jf.MediaItemFrom(m)
			if !playedViaPlayback[jf.NormalizeID(item.ID)] {
				continue
			}
			if lp := jf.LastPlayedOf(m); lp != "" {
				item.LastPlayed = jf.LocalDate(lp)
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
		items := jf.SessionsFrom(sessions)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "jellyfin://sessions",
				MIMEType: "application/json",
				Text:     jf.FormatJSON(items),
			}},
		}, nil
	})

	// --- jellyfin://users ---
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

	// --- Reference Guides ---
	registerGuides(server)

	// --- jellyfin://items/{itemId} (template) ---
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "jellyfin://items/{itemId}",
		Name:        "Media Item",
		Title:       "Media Item",
		Description: "Detailed metadata for any media item. itemId is the item's Jellyfin ID from search or browse results",
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
		item, err := jf.FetchItemDetails(ctx, client, itemID, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get item: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     jf.FormatJSON(item),
			}},
		}, nil
	})

	// --- jellyfin://users/{userId} (template) ---
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "jellyfin://users/{userId}",
		Name:        "User Profile",
		Title:       "User Profile",
		Description: "User account details and policy settings. userId is a user ID from jellyfin_users action=list, not a username",
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

	// --- jellyfin://libraries/{libraryId}/latest (template) ---
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "jellyfin://libraries/{libraryId}/latest",
		Name:        "Library Latest",
		Title:       "Library Latest",
		Description: "Up to 20 recently added items in a specific library. libraryId is the library's item_id from jellyfin_libraries",
		MIMEType:    "application/json",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		libraryID := strings.TrimPrefix(req.Params.URI, "jellyfin://libraries/")
		libraryID = strings.TrimSuffix(libraryID, "/latest")
		if libraryID == "" {
			return nil, fmt.Errorf("library ID is required in URI")
		}
		items, err := jf.FetchLatest(ctx, client, libraryID, jf.LatestItemsLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to get library latest: %w", err)
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
