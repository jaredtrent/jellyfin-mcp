package jellyfin

import (
	"context"
	"fmt"
	"log"
	"net/url"
)

// FetchItemDetails reads an item as the user sees it, with the collections
// that include it on Jellyfin 12 or later. When the collections cannot be
// read, the item is returned without them and with a note that says so. The
// error is the item request's.
func FetchItemDetails(ctx context.Context, client Client, itemID, userID string) (DetailedItemOutput, error) {
	var raw map[string]any
	endpoint := fmt.Sprintf("/Items/%s", SanitizeID(itemID))
	if err := client.Get(ctx, endpoint, url.Values{"UserId": {userID}}, &raw); err != nil {
		return DetailedItemOutput{}, err
	}
	item := DetailedItemFrom(raw)
	collections, err := itemCollections(ctx, client, itemID, userID)
	if err != nil {
		log.Printf("%v", err)
		item.Notes = append(item.Notes, fmt.Sprintf("The collections that include this item could not be listed: %v", err))
	}
	item.IncludedInCollections = collections
	return item, nil
}

// itemCollections lists the collections that include an item, as an empty
// list for none. On a server older than 12.0, which lacks the route, or when
// the version is unknown, it returns nil and no error.
func itemCollections(ctx context.Context, client Client, itemID, userID string) (*[]CollectionRef, error) {
	if v, err := client.ServerVersion(ctx); err != nil || !v.AtLeast(12, 0) {
		return nil, nil
	}
	var result map[string]any
	endpoint := fmt.Sprintf("/Items/%s/Collections", SanitizeID(itemID))
	if err := client.Get(ctx, endpoint, url.Values{"UserId": {userID}}, &result); err != nil {
		return nil, fmt.Errorf("listing the collections that include item %s: %w", itemID, err)
	}
	raw := ToSlice(result["Items"])
	collections := make([]CollectionRef, 0, len(raw))
	for _, c := range raw {
		m := ToMap(c)
		if m == nil {
			continue
		}
		collections = append(collections, CollectionRef{ID: GetString(m, "Id"), Name: GetString(m, "Name")})
	}
	return &collections, nil
}
