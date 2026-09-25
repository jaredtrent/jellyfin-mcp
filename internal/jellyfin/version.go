package jellyfin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ServerVersion is a Jellyfin server version such as 10.11.11 or 12.1.0.
type ServerVersion struct {
	Major, Minor, Patch int
}

// MinSupportedVersion is the oldest Jellyfin release this server is built
// and tested against.
var MinSupportedVersion = ServerVersion{Major: 10, Minor: 11}

// ParseServerVersion parses the Version field of /System/Info/Public. Jellyfin
// reports three components; a fourth, if present, is ignored.
func ParseServerVersion(s string) (ServerVersion, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 2 {
		return ServerVersion{}, fmt.Errorf("unrecognized Jellyfin version %q", s)
	}
	var nums [3]int
	for i := 0; i < len(nums) && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return ServerVersion{}, fmt.Errorf("unrecognized Jellyfin version %q", s)
		}
		nums[i] = n
	}
	return ServerVersion{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

func (v ServerVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// AtLeast reports whether v is major.minor or later.
func (v ServerVersion) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

// Supported reports whether v is at or above MinSupportedVersion.
func (v ServerVersion) Supported() bool {
	return v.AtLeast(MinSupportedVersion.Major, MinSupportedVersion.Minor)
}

// serverVersionTTL bounds how long a fetched version is trusted. A Jellyfin
// server can be upgraded while this process keeps running, and features gated
// on the version must follow the upgrade without a restart.
const serverVersionTTL = 10 * time.Minute

// ServerVersion returns the version of the connected Jellyfin server, read
// from the anonymous /System/Info/Public endpoint and cached for
// serverVersionTTL. If a refresh fails, the last known version is returned.
func (c *JellyfinClient) ServerVersion(ctx context.Context) (ServerVersion, error) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if !c.versionFetched.IsZero() && time.Since(c.versionFetched) < serverVersionTTL {
		return c.version, nil
	}

	var info map[string]any
	err := c.Get(ctx, "/System/Info/Public", nil, &info)
	if err == nil {
		var v ServerVersion
		if v, err = ParseServerVersion(GetString(info, "Version")); err == nil {
			c.version, c.versionFetched = v, time.Now()
			return v, nil
		}
	}
	if !c.versionFetched.IsZero() {
		return c.version, nil
	}
	return ServerVersion{}, fmt.Errorf("reading the Jellyfin server version: %w", err)
}
