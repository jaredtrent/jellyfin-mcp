package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// pollClient answers the poller's requests with the content set for each
// endpoint, and counts the requests by endpoint.
type pollClient struct {
	jf.Client
	mu      sync.Mutex
	content map[string]string
	calls   map[string]int
	params  map[string]url.Values // the last query sent to each endpoint
}

func newPollClient() *pollClient {
	return &pollClient{content: map[string]string{}, calls: map[string]int{}, params: map[string]url.Values{}}
}

func (c *pollClient) DoRequest(_ context.Context, _, endpoint string, _ url.Values, _ any) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[endpoint]++
	return []byte(c.content[endpoint]), nil
}

func (c *pollClient) GetUserID(context.Context) (string, error) { return "user-1", nil }

func (c *pollClient) ServerVersion(context.Context) (jf.ServerVersion, error) {
	return jf.ServerVersion{Major: 12, Minor: 1}, nil
}

func (c *pollClient) Get(_ context.Context, endpoint string, params url.Values, dest any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[endpoint]++
	c.params[endpoint], _ = url.ParseQuery(params.Encode())
	if body, ok := c.content[endpoint]; ok {
		return json.Unmarshal([]byte(body), dest)
	}
	return nil
}

func (c *pollClient) count(endpoint string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[endpoint]
}

func (c *pollClient) endpoints() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *pollClient) set(endpoint, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content[endpoint] = body
}

// watchers counts the sessions that subscribe to uri.
func (t *subscriptionTracker) watchers(uri string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, subscribed := range t.sessions {
		if subscribed[uri] {
			n++
		}
	}
	return n
}

// sessionCount counts the sessions the tracker holds.
func (t *subscriptionTracker) sessionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// connectClient connects a client of the protocol version to srv.
func connectClient(t *testing.T, srv *mcp.Server, version string, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, opts).
		Connect(t.Context(), ct, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// eventually fails the test unless cond holds within five seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A session's subscriptions end with the session, whether or not its client
// unsubscribes first, and each session's subscriptions are its own.
func TestSubscriptionsEndWithTheirSession(t *testing.T) {
	const uri = "jellyfin://sessions"
	for _, version := range []string{statelessProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			tracker := &subscriptionTracker{}
			srv := newServer(Config{}, fakeClient{}, tracker)
			first := connectClient(t, srv, version, nil)
			second := connectClient(t, srv, version, nil)
			for _, cs := range []*mcp.ClientSession{first, second} {
				if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: uri}); err != nil {
					t.Fatal(err)
				}
			}
			eventually(t, "both subscriptions are recorded", func() bool { return tracker.watchers(uri) == 2 })

			_ = first.Close()
			eventually(t, "the closed session is released", func() bool { return tracker.sessionCount() == 1 })
			if n := tracker.watchers(uri); n != 1 {
				t.Fatalf("%d sessions watch the resource after one of two closed, want 1", n)
			}
			if err := second.Unsubscribe(t.Context(), &mcp.UnsubscribeParams{URI: uri}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "no session watches the resource", func() bool { return !tracker.watched(uri) })
		})
	}
}

// Unsubscribing from one resource leaves the session's other subscriptions.
func TestUnsubscribeEndsOnlyThatResource(t *testing.T) {
	const kept, dropped = "jellyfin://sessions", "jellyfin://latest"
	for _, version := range []string{statelessProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			tracker := &subscriptionTracker{}
			cs := connectClient(t, newServer(Config{}, fakeClient{}, tracker), version, nil)
			for _, uri := range []string{kept, dropped} {
				if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: uri}); err != nil {
					t.Fatal(err)
				}
			}
			eventually(t, "both subscriptions are recorded", func() bool { return tracker.watchers(kept) == 1 && tracker.watchers(dropped) == 1 })
			if err := cs.Unsubscribe(t.Context(), &mcp.UnsubscribeParams{URI: dropped}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "the unsubscribed resource is released", func() bool { return !tracker.watched(dropped) })
			if !tracker.watched(kept) {
				t.Errorf("unsubscribing from %s ended the subscription to %s", dropped, kept)
			}
		})
	}
}

// A client that goes away without unsubscribing releases its subscriptions.
func TestSubscriptionsEndWhenTheClientLeaves(t *testing.T) {
	const uri = "jellyfin://latest"
	for _, version := range []string{statelessProtocolVersion, "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			tracker := &subscriptionTracker{}
			cs := connectClient(t, newServer(Config{}, fakeClient{}, tracker), version, nil)
			if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: uri}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "the subscription is recorded", func() bool { return tracker.watched(uri) })
			_ = cs.Close()
			eventually(t, "the subscription ends", func() bool { return !tracker.watched(uri) })
		})
	}
}

// postCounter is a fakeClient that counts the writes a tool performs.
type postCounter struct {
	fakeClient
	posts atomic.Int32
}

func (c *postCounter) PostNoContent(context.Context, string, url.Values, any) error {
	c.posts.Add(1)
	return nil
}

// A confirmation form the user never answers gives up after its limit: the
// call fails with nothing done, and a session closed while the form is open
// still ends and releases its subscriptions.
func TestUnansweredConfirmationFormGivesUp(t *testing.T) {
	saved := elicitationTimeout
	elicitationTimeout = 200 * time.Millisecond
	t.Cleanup(func() { elicitationTimeout = saved })
	restart := &mcp.CallToolParams{Name: "jellyfin_system_control", Arguments: map[string]any{"action": "restart"}}

	// connect returns a legacy client whose user never answers a form, and a
	// channel that receives when the form is shown.
	connect := func(t *testing.T, srv *mcp.Server) (*mcp.ClientSession, chan struct{}) {
		asked, release := make(chan struct{}, 1), make(chan struct{})
		cs := connectClient(t, srv, "2025-11-25", &mcp.ClientOptions{
			ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				asked <- struct{}{}
				<-release
				return nil, errors.New("the client went away")
			},
		})
		t.Cleanup(func() { close(release) })
		return cs, asked
	}

	t.Run("the call fails", func(t *testing.T) {
		client := &postCounter{}
		cs, _ := connect(t, newServer(Config{}, client, &subscriptionTracker{}))
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, err := cs.CallTool(ctx, restart)
		if err == nil || !strings.Contains(err.Error(), "the confirmation form was not answered within 200ms, so nothing was done") {
			t.Errorf("call error %v, want the form's time limit", err)
		}
		if n := client.posts.Load(); n != 0 {
			t.Errorf("the tool performed %d writes", n)
		}
	})

	t.Run("a closing session ends", func(t *testing.T) {
		const uri = "jellyfin://sessions"
		tracker := &subscriptionTracker{}
		srv := newServer(Config{}, &postCounter{}, tracker)
		cs, asked := connect(t, srv)
		if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: uri}); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the subscription is recorded", func() bool { return tracker.watched(uri) })
		go func() { _, _ = cs.CallTool(context.Background(), restart) }()
		select {
		case <-asked:
		case <-time.After(5 * time.Second):
			t.Fatal("the confirmation form was never shown")
		}
		// The session closes as the stateful HTTP handler closes an idle one.
		for ss := range srv.Sessions() {
			go func() { _ = ss.Close() }()
		}
		eventually(t, "the subscription ends", func() bool { return !tracker.watched(uri) })
	})
}

func TestSubscribeRefusesOtherResources(t *testing.T) {
	tracker := &subscriptionTracker{}
	cs := connectClient(t, newServer(Config{}, fakeClient{}, tracker), "2025-11-25", nil)
	if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: "jellyfin://libraries"}); err == nil {
		t.Error("subscribing to jellyfin://libraries succeeded")
	}
	if tracker.watched("jellyfin://libraries") {
		t.Error("a refused subscription is recorded")
	}
}

// The poller reads only the groups a session watches. After a group has gone
// unwatched, the first poll sets a new baseline instead of reporting a change
// that happened while nobody was watching.
func TestResourceWatchPollsOnlyWatchedGroups(t *testing.T) {
	client := newPollClient()
	client.set("/Sessions", "one")
	tracker := &subscriptionTracker{}
	srv := newServer(Config{}, client, tracker)
	updates := make(chan string, 8)
	cs := connectClient(t, srv, "2025-11-25", &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) { updates <- req.Params.URI },
	})
	watches := newResourceWatches(client)
	pollAll := func() {
		for _, w := range []*resourceWatch{watches.sessions, watches.latest, watches.played} {
			w.poll(t.Context(), srv, tracker)
		}
	}

	pollAll()
	if n := client.endpoints(); n != 0 {
		t.Fatalf("polled %v with nothing watched", client.calls)
	}

	if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: "jellyfin://sessions/now-playing"}); err != nil {
		t.Fatal(err)
	}
	pollAll()
	if client.count("/Sessions") != 1 || client.endpoints() != 1 {
		t.Fatalf("polled %v, want only /Sessions", client.calls)
	}
	noUpdate(t, updates, "baseline poll")

	client.set("/Sessions", "two")
	watches.sessions.poll(t.Context(), srv, tracker)
	wantUpdate(t, updates, "jellyfin://sessions/now-playing")

	if err := cs.Unsubscribe(t.Context(), &mcp.UnsubscribeParams{URI: "jellyfin://sessions/now-playing"}); err != nil {
		t.Fatal(err)
	}
	watches.sessions.poll(t.Context(), srv, tracker)
	client.set("/Sessions", "three")
	if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: "jellyfin://sessions/now-playing"}); err != nil {
		t.Fatal(err)
	}
	watches.sessions.poll(t.Context(), srv, tracker)
	noUpdate(t, updates, "first poll after resubscribing")
}

// Each resource is read from its own endpoint, and a change there notifies
// that resource's subscribers.
func TestResourceWatchesReadTheirOwnEndpoint(t *testing.T) {
	for _, tt := range []struct{ uri, endpoint, before, after string }{
		{"jellyfin://sessions", "/Sessions", "one", "two"},
		{"jellyfin://latest", "/Items/Latest", `[{"Id":"a"}]`, `[{"Id":"b"}]`},
		{"jellyfin://recently-played", "/System/ActivityLog/Entries", playbackLog("item-a"), playbackLog("item-b")},
	} {
		t.Run(tt.uri, func(t *testing.T) {
			client := newPollClient()
			client.set(tt.endpoint, tt.before)
			tracker := &subscriptionTracker{}
			srv := newServer(Config{}, client, tracker)
			updates := make(chan string, 8)
			cs := connectClient(t, srv, "2025-11-25", &mcp.ClientOptions{
				ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) { updates <- req.Params.URI },
			})
			if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: tt.uri}); err != nil {
				t.Fatal(err)
			}
			watches := newResourceWatches(client)
			pollAll := func() {
				for _, w := range []*resourceWatch{watches.sessions, watches.latest, watches.played} {
					w.poll(t.Context(), srv, tracker)
				}
			}
			pollAll()
			if client.count(tt.endpoint) != 1 || client.endpoints() != 1 {
				t.Fatalf("polled %v, want only %s", client.calls, tt.endpoint)
			}
			noUpdate(t, updates, "baseline poll")
			client.set(tt.endpoint, tt.after)
			pollAll()
			wantUpdate(t, updates, tt.uri)
			if tt.uri == "jellyfin://recently-played" {
				if q := client.params[tt.endpoint]; q.Get("Type") != "PlaybackStopped" {
					t.Errorf("recently-played polled with %v, want the playback entries of video and audio", q)
				}
			}
		})
	}
}

// The poller reads each group on its own interval until its context ends.
func TestResourcePollerIntervals(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		sessionEvery, contentEvery time.Duration
		polled, idle               string
	}{
		{"sessions", time.Millisecond, time.Hour, "/Sessions", "/Items/Latest"},
		{"content", time.Hour, time.Millisecond, "/Items/Latest", "/Sessions"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newPollClient()
			tracker := &subscriptionTracker{}
			srv := newServer(Config{}, client, tracker)
			cs := connectClient(t, srv, "2025-11-25", nil)
			for _, uri := range []string{"jellyfin://sessions", "jellyfin://latest"} {
				if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: uri}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			startResourcePoller(ctx, srv, client, tracker, tt.sessionEvery, tt.contentEvery)
			eventually(t, tt.polled+" is polled repeatedly", func() bool { return client.count(tt.polled) >= 3 })
			if n := client.count(tt.idle); n != 0 {
				t.Errorf("%s polled %d times on the other group's interval", tt.idle, n)
			}
			cancel()
			time.Sleep(20 * time.Millisecond)
			stopped := client.count(tt.polled)
			time.Sleep(20 * time.Millisecond)
			if n := client.count(tt.polled); n != stopped {
				t.Errorf("%s was polled after the poller's context ended", tt.polled)
			}
		})
	}
}

// noUpdate fails the test if a resource update arrives soon.
func noUpdate(t *testing.T, updates <-chan string, when string) {
	t.Helper()
	select {
	case uri := <-updates:
		t.Errorf("%s: unexpected update of %s", when, uri)
	case <-time.After(50 * time.Millisecond):
	}
}

// wantUpdate fails the test unless the next resource update is of uri, and
// no other follows it soon.
func wantUpdate(t *testing.T, updates <-chan string, uri string) {
	t.Helper()
	select {
	case got := <-updates:
		if got != uri {
			t.Errorf("update of %s, want %s", got, uri)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no update of %s after its content changed", uri)
	}
	noUpdate(t, updates, "after the update of "+uri)
}

// playbackLog is an activity log page holding one playback of itemID by the
// user pollClient acts as.
func playbackLog(itemID string) string {
	return `{"Items":[{"Type":"AudioPlaybackStopped","UserId":"user-1","ItemId":"` + itemID + `"}],"TotalRecordCount":1}`
}

// jellyfin://recently-played belongs to the user pollClient acts as, so
// another user's playback does not change it.
func TestRecentlyPlayedWatchIgnoresOtherUsers(t *testing.T) {
	client := newPollClient()
	client.set("/System/ActivityLog/Entries", playbackLog("item-a"))
	tracker := &subscriptionTracker{}
	srv := newServer(Config{}, client, tracker)
	updates := make(chan string, 8)
	cs := connectClient(t, srv, "2025-11-25", &mcp.ClientOptions{
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) { updates <- req.Params.URI },
	})
	if err := cs.Subscribe(t.Context(), &mcp.SubscribeParams{URI: "jellyfin://recently-played"}); err != nil {
		t.Fatal(err)
	}
	played := newResourceWatches(client).played
	played.poll(t.Context(), srv, tracker)
	client.set("/System/ActivityLog/Entries", `{"Items":[`+
		`{"Type":"VideoPlaybackStopped","UserId":"user-2","ItemId":"item-b"},`+
		`{"Type":"AudioPlaybackStopped","UserId":"user-1","ItemId":"item-a"}],"TotalRecordCount":2}`)
	played.poll(t.Context(), srv, tracker)
	noUpdate(t, updates, "another user's playback")
}
