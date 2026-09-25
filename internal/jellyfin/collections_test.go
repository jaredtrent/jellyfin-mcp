package jellyfin

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// collectionsTestServer serves an item, the collections that include it, and
// the server version. A zero status serves the route normally; any other
// status fails it. collectionsCalls counts requests to the collections route.
func collectionsTestServer(t *testing.T, version string, itemStatus, collectionsStatus int, collectionsCalls *int) *JellyfinClient {
	t.Helper()
	return newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public":
			_, _ = w.Write([]byte(`{"Version":"` + version + `"}`))
		case "/Items/item-1":
			if itemStatus != 0 {
				http.Error(w, "item failed", itemStatus)
				return
			}
			if r.URL.Query().Get("UserId") != "user-1" {
				t.Errorf("item request UserId = %q, want user-1", r.URL.Query().Get("UserId"))
			}
			_, _ = w.Write([]byte(`{"Id":"item-1","Name":"Test Movie","Type":"Movie"}`))
		case "/Items/item-1/Collections":
			*collectionsCalls++
			if collectionsStatus != 0 {
				http.Error(w, "collections failed", collectionsStatus)
				return
			}
			_, _ = w.Write([]byte(`{"Items":[{"Id":"col-1","Name":"Test Collection A"},{"Id":"col-2","Name":"Test Collection B"}]}`))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestFetchItemDetails(t *testing.T) {
	ctx := context.Background()

	t.Run("collections on 12", func(t *testing.T) {
		calls := 0
		c := collectionsTestServer(t, "12.1.0", 0, 0, &calls)
		item, err := FetchItemDetails(ctx, c, "item-1", "user-1")
		if err != nil {
			t.Fatal(err)
		}
		if item.ID != "item-1" || item.Name != "Test Movie" {
			t.Errorf("item = %+v", item)
		}
		want := []CollectionRef{{ID: "col-1", Name: "Test Collection A"}, {ID: "col-2", Name: "Test Collection B"}}
		if item.IncludedInCollections == nil || !reflect.DeepEqual(*item.IncludedInCollections, want) {
			t.Errorf("IncludedInCollections = %v, want %v", item.IncludedInCollections, want)
		}
		if len(item.Notes) != 0 {
			t.Errorf("Notes = %v, want none", item.Notes)
		}
	})

	t.Run("collections route fails on 12", func(t *testing.T) {
		calls := 0
		c := collectionsTestServer(t, "12.1.0", 0, http.StatusInternalServerError, &calls)
		item, err := FetchItemDetails(ctx, c, "item-1", "user-1")
		if err != nil {
			t.Fatalf("error = %v, want the item without its collections", err)
		}
		if item.ID != "item-1" {
			t.Errorf("item = %+v", item)
		}
		if item.IncludedInCollections != nil {
			t.Errorf("IncludedInCollections = %v, want nil", *item.IncludedInCollections)
		}
		if len(item.Notes) != 1 || !strings.Contains(item.Notes[0], "The collections that include this item could not be listed") {
			t.Errorf("Notes = %v, want the collections note", item.Notes)
		}
	})

	t.Run("no collections request on 10.11", func(t *testing.T) {
		calls := 0
		c := collectionsTestServer(t, "10.11.11", 0, 0, &calls)
		item, err := FetchItemDetails(ctx, c, "item-1", "user-1")
		if err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Errorf("collections requests = %d, want 0", calls)
		}
		if item.IncludedInCollections != nil {
			t.Errorf("IncludedInCollections = %v, want nil", *item.IncludedInCollections)
		}
		if len(item.Notes) != 0 {
			t.Errorf("Notes = %v, want none", item.Notes)
		}
	})

	t.Run("item route fails", func(t *testing.T) {
		calls := 0
		c := collectionsTestServer(t, "12.1.0", http.StatusInternalServerError, 0, &calls)
		if _, err := FetchItemDetails(ctx, c, "item-1", "user-1"); err == nil {
			t.Error("error = nil, want the item request's error")
		}
		if calls != 0 {
			t.Errorf("collections requests = %d, want 0 after the item failed", calls)
		}
	})
}
