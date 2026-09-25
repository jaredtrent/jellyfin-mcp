package jellyfin

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestFetchLatest(t *testing.T) {
	var got url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items/Latest" {
			http.NotFound(w, r)
			return
		}
		got = r.URL.Query()
		_, _ = w.Write([]byte(`[{"Id":"item-1","Name":"Test Movie","Type":"Movie"},{"Id":"item-2","Name":"Test Episode","Type":"Episode"}]`))
	})
	c.userID = "user-1"

	items, err := FetchLatest(context.Background(), c, "library-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("UserId") != "user-1" || got.Get("ParentId") != "library-1" || got.Get("Limit") != "7" {
		t.Errorf("query = %v, want UserId=user-1 ParentId=library-1 Limit=7", got)
	}
	if len(items) != 2 || items[0].ID != "item-1" || items[1].Name != "Test Episode" {
		t.Errorf("items = %v", items)
	}

	if _, err := FetchLatest(context.Background(), c, "", LatestItemsLimit); err != nil {
		t.Fatal(err)
	}
	if got.Has("ParentId") {
		t.Errorf("ParentId sent without a library: %v", got)
	}
}
