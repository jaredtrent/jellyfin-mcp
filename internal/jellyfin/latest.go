package jellyfin

import (
	"context"
	"fmt"
	"net/url"
)

// LatestItemsLimit is how many items the latest-items views show.
const LatestItemsLimit = 20

// FetchLatest returns the most recently added items, newest first, as compact
// item summaries. parentID, when set, limits them to one library.
//
// /Items/Latest is user-scoped: without a user the server answers 404, and an
// API key carries no user, so the resolved user is always sent.
func FetchLatest(ctx context.Context, client Client, parentID string, limit int) ([]MediaItem, error) {
	userID, err := client.GetUserID(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{
		"UserId": {userID},
		"Limit":  {fmt.Sprintf("%d", limit)},
		"Fields": {"Overview"},
	}
	if parentID != "" {
		params.Set("ParentId", parentID)
	}
	var result []map[string]any
	if err := client.Get(ctx, "/Items/Latest", params, &result); err != nil {
		return nil, err
	}
	items := make([]MediaItem, 0, len(result))
	for _, raw := range result {
		if raw != nil {
			items = append(items, MediaItemFrom(raw))
		}
	}
	return items, nil
}
