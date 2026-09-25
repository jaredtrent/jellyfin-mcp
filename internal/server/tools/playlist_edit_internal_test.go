package tools

import (
	"context"
	"testing"
)

// A playlist's lock exists only while a change holds or waits for it.
func TestPlaylistLocks_ForgetIdlePlaylists(t *testing.T) {
	locks := newPlaylistLocks()
	release, err := locks.lock(context.Background(), "playlist-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(locks.locks) != 1 {
		t.Fatalf("%d entries while held, want 1", len(locks.locks))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := locks.lock(ctx, "playlist-1"); err == nil {
		t.Fatal("a cancelled wait returned a lock")
	}
	if len(locks.locks) != 1 {
		t.Fatalf("%d entries after a cancelled wait, want 1", len(locks.locks))
	}
	release()
	if len(locks.locks) != 0 {
		t.Fatalf("%d entries after release, want 0", len(locks.locks))
	}
}
