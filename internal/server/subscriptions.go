package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/url"
	"sync/atomic"
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

type subscriptionTracker struct {
	count atomic.Int64
}

func (t *subscriptionTracker) increment() { t.count.Add(1) }

func (t *subscriptionTracker) decrement() {
	for {
		old := t.count.Load()
		if old <= 0 {
			return
		}
		if t.count.CompareAndSwap(old, old-1) {
			return
		}
	}
}

func (t *subscriptionTracker) active() bool { return t.count.Load() > 0 }

func subscribeHandler(tracker *subscriptionTracker) func(context.Context, *mcp.SubscribeRequest) error {
	return func(_ context.Context, req *mcp.SubscribeRequest) error {
		uri := req.Params.URI
		if !subscribableURIs[uri] {
			return fmt.Errorf("resource %q does not support subscriptions", uri)
		}
		tracker.increment()
		return nil
	}
}

func unsubscribeHandler(tracker *subscriptionTracker) func(context.Context, *mcp.UnsubscribeRequest) error {
	return func(_ context.Context, req *mcp.UnsubscribeRequest) error {
		uri := req.Params.URI
		if !subscribableURIs[uri] {
			return fmt.Errorf("resource %q does not support subscriptions", uri)
		}
		tracker.decrement()
		return nil
	}
}

func startResourcePoller(ctx context.Context, server *mcp.Server, client jf.Client, tracker *subscriptionTracker) {
	const (
		sessionInterval = 10 * time.Second
		contentInterval = 60 * time.Second
	)

	go func() {
		sessionTicker := time.NewTicker(sessionInterval)
		contentTicker := time.NewTicker(contentInterval)
		defer sessionTicker.Stop()
		defer contentTicker.Stop()

		var (
			lastSessionHash [sha256.Size]byte
			lastLatestHash  [sha256.Size]byte
			lastPlayedHash  [sha256.Size]byte
			seededSession   bool
			seededLatest    bool
			seededPlayed    bool
		)

		pollSessions := func() {
			if !tracker.active() {
				return
			}
			data, err := client.DoRequest(ctx, "GET", "/Sessions", nil, nil)
			if err != nil {
				log.Printf("poll sessions: %v", err)
				return
			}
			hash := sha256.Sum256(data)
			changed := hash != lastSessionHash
			lastSessionHash = hash
			if !seededSession {
				seededSession = true
				return
			}
			if changed {
				if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: "jellyfin://sessions"}); err != nil {
					log.Printf("ResourceUpdated(sessions): %v", err)
				}
				if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: "jellyfin://sessions/now-playing"}); err != nil {
					log.Printf("ResourceUpdated(sessions/now-playing): %v", err)
				}
			}
		}

		pollLatest := func() {
			if !tracker.active() {
				return
			}
			userID, err := client.GetUserID(ctx)
			if err != nil {
				log.Printf("poll latest: %v", err)
				return
			}
			params := url.Values{"Limit": {"20"}, "UserId": {userID}}
			data, err := client.DoRequest(ctx, "GET", "/Items/Latest", params, nil)
			if err != nil {
				log.Printf("poll latest: %v", err)
				return
			}
			hash := sha256.Sum256(data)
			changed := hash != lastLatestHash
			lastLatestHash = hash
			if !seededLatest {
				seededLatest = true
				return
			}
			if changed {
				if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: "jellyfin://latest"}); err != nil {
					log.Printf("ResourceUpdated(latest): %v", err)
				}
			}
		}

		pollRecentlyPlayed := func() {
			if !tracker.active() {
				return
			}
			params := url.Values{"Limit": {"25"}, "Type": {"VideoPlaybackStopped"}}
			data, err := client.DoRequest(ctx, "GET", "/System/ActivityLog/Entries", params, nil)
			if err != nil {
				log.Printf("poll recently-played: %v", err)
				return
			}
			hash := sha256.Sum256(data)
			changed := hash != lastPlayedHash
			lastPlayedHash = hash
			if !seededPlayed {
				seededPlayed = true
				return
			}
			if changed {
				if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: "jellyfin://recently-played"}); err != nil {
					log.Printf("ResourceUpdated(recently-played): %v", err)
				}
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-sessionTicker.C:
				pollSessions()
			case <-contentTicker.C:
				pollLatest()
				pollRecentlyPlayed()
			}
		}
	}()

	log.Printf("Resource subscription poller started (sessions: %s, content: %s)", sessionInterval, contentInterval)
}
