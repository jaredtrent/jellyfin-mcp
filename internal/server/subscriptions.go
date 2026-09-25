package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

var subscribableURIs = map[string]bool{
	"jellyfin://sessions":             true,
	"jellyfin://sessions/now-playing": true,
	"jellyfin://latest":               true,
	"jellyfin://recently-played":      true,
}

// subscriptionTracker records which sessions subscribe to which resources, so
// the poller reads Jellyfin only for resources a session is watching. A
// session's subscriptions end when the session does, including one whose
// client goes away without unsubscribing. The HTTP session of a client on a
// protocol before statelessProtocolVersion ends when the client closes it or
// after the handler's SessionTimeout without a request, not when its
// connection drops.
type subscriptionTracker struct {
	mu       sync.Mutex
	sessions map[*mcp.ServerSession]map[string]bool
}

// subscribe records that ss watches uri. The first subscription of a session
// starts a wait for the session's end, which releases all of them.
func (t *subscriptionTracker) subscribe(ss *mcp.ServerSession, uri string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		t.sessions = make(map[*mcp.ServerSession]map[string]bool)
	}
	uris, known := t.sessions[ss]
	if !known {
		uris = make(map[string]bool)
		t.sessions[ss] = uris
		go func() {
			_ = ss.Wait()
			t.mu.Lock()
			defer t.mu.Unlock()
			delete(t.sessions, ss)
		}()
	}
	uris[uri] = true
}

func (t *subscriptionTracker) unsubscribe(ss *mcp.ServerSession, uri string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions[ss], uri)
}

// watched reports whether any session subscribes to any of uris.
func (t *subscriptionTracker) watched(uris ...string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, subscribed := range t.sessions {
		for _, uri := range uris {
			if subscribed[uri] {
				return true
			}
		}
	}
	return false
}

func subscribeHandler(tracker *subscriptionTracker) func(context.Context, *mcp.SubscribeRequest) error {
	return func(_ context.Context, req *mcp.SubscribeRequest) error {
		uri := req.Params.URI
		if !subscribableURIs[uri] {
			return fmt.Errorf("resource %q does not support subscriptions", uri)
		}
		tracker.subscribe(req.Session, uri)
		return nil
	}
}

func unsubscribeHandler(tracker *subscriptionTracker) func(context.Context, *mcp.UnsubscribeRequest) error {
	return func(_ context.Context, req *mcp.UnsubscribeRequest) error {
		uri := req.Params.URI
		if !subscribableURIs[uri] {
			return fmt.Errorf("resource %q does not support subscriptions", uri)
		}
		tracker.unsubscribe(req.Session, uri)
		return nil
	}
}

// resourceWatch notices changes to one group of subscribable resources by
// hashing what Jellyfin returns for them.
type resourceWatch struct {
	uris   []string // the resources fetch reads; each is notified on a change
	fetch  func(context.Context) ([]byte, error)
	last   [sha256.Size]byte
	seeded bool
}

// poll reads the group while a session watches one of its resources, and
// notifies the subscribers when the content changed since the previous poll.
// The first poll after the group becomes watched sets the baseline and
// notifies nothing.
func (w *resourceWatch) poll(ctx context.Context, server *mcp.Server, tracker *subscriptionTracker) {
	if !tracker.watched(w.uris...) {
		w.seeded = false
		return
	}
	data, err := w.fetch(ctx)
	if err != nil {
		log.Printf("poll %s: %v", w.uris[0], err)
		return
	}
	hash := sha256.Sum256(data)
	changed := w.seeded && hash != w.last
	w.last, w.seeded = hash, true
	if !changed {
		return
	}
	for _, uri := range w.uris {
		if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri}); err != nil {
			log.Printf("ResourceUpdated(%s): %v", uri, err)
		}
	}
}

// resourceWatches are the poller's groups: sessions, polled often, and the
// two content groups, polled less often.
type resourceWatches struct {
	sessions, latest, played *resourceWatch
}

func newResourceWatches(client jf.Client) resourceWatches {
	return resourceWatches{
		sessions: &resourceWatch{
			uris: []string{"jellyfin://sessions", "jellyfin://sessions/now-playing"},
			fetch: func(ctx context.Context) ([]byte, error) {
				return client.DoRequest(ctx, "GET", "/Sessions", nil, nil)
			},
		},
		latest: &resourceWatch{
			uris: []string{"jellyfin://latest"},
			fetch: func(ctx context.Context) ([]byte, error) {
				items, err := jf.FetchLatest(ctx, client, "", jf.LatestItemsLimit)
				if err != nil {
					return nil, err
				}
				return json.Marshal(items)
			},
		},
		played: &resourceWatch{
			uris: []string{"jellyfin://recently-played"},
			fetch: func(ctx context.Context) ([]byte, error) {
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return nil, err
				}
				// A change shows among the newest entries, so the watch
				// examines only those.
				found, err := jf.QueryActivity(ctx, client, jf.ActivityQuery{Type: "PlaybackStopped", UserID: userID, Limit: 25, Lookback: 200})
				if err != nil {
					return nil, err
				}
				return json.Marshal(found.Entries)
			},
		},
	}
}

// How often the poller reads each group: sessions change by the second, and
// library content changes slowly.
const (
	sessionPollInterval = 10 * time.Second
	contentPollInterval = 60 * time.Second
)

// startResourcePoller polls the sessions group every sessionInterval and the
// content groups every contentInterval, until ctx ends.
func startResourcePoller(ctx context.Context, server *mcp.Server, client jf.Client, tracker *subscriptionTracker, sessionInterval, contentInterval time.Duration) {
	watches := newResourceWatches(client)

	go func() {
		sessionTicker := time.NewTicker(sessionInterval)
		contentTicker := time.NewTicker(contentInterval)
		defer sessionTicker.Stop()
		defer contentTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sessionTicker.C:
				watches.sessions.poll(ctx, server, tracker)
			case <-contentTicker.C:
				watches.latest.poll(ctx, server, tracker)
				watches.played.poll(ctx, server, tracker)
			}
		}
	}()

	log.Printf("Resource subscription poller started (sessions: %s, content: %s)", sessionInterval, contentInterval)
}
