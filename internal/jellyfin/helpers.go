package jellyfin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func ErrResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

// ErrResultWithHint returns an error result with an actionable recovery hint appended.
func ErrResultWithHint(hint string, format string, args ...any) *mcp.CallToolResult {
	msg := fmt.Sprintf(format, args...)
	if hint != "" {
		msg += "\n\nHint: " + hint
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// ReportProgress sends a progress notification if the request includes a progress token.
func ReportProgress(ctx context.Context, req *mcp.CallToolRequest, progress, total float64, message string) {
	token := req.Params.GetProgressToken()
	if token != nil {
		if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      progress,
			Total:         total,
			Message:       message,
		}); err != nil {
			log.Printf("progress notification failed: %v", err)
		}
	}
}

func SanitizeID(id string) string {
	escaped := url.PathEscape(id)
	// A bare "." or ".." survives escaping and would be resolved by the URL
	// path join, so its dots are escaped too.
	if escaped == "." || escaped == ".." {
		return strings.ReplaceAll(escaped, ".", "%2E")
	}
	return escaped
}

// NormalizeID reduces a GUID to the form Jellyfin writes, 32 lower-case hex
// digits without dashes, so that an ID given in the dashed or upper-case form
// compares equal to the same ID as the server reports it.
func NormalizeID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}

func FormatJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("[json error: %v]", err)
	}
	return string(b)
}

func GetString(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func GetInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok && v != nil {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

func GetIntPtr(m map[string]any, key string) *int {
	if v, ok := m[key]; ok && v != nil {
		if f, ok := v.(float64); ok {
			i := int(f)
			return &i
		}
	}
	return nil
}

func GetInt64(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok && v != nil {
		if f, ok := v.(float64); ok {
			return int64(f)
		}
	}
	return 0
}

func GetFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok && v != nil {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func GetBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// GetNum extracts a numeric value from a map, handling the int/int64/float64
// type variations that arise from JSON unmarshaling and internal map building.
func GetNum[T int | int64 | float64](m map[string]any, key string) T {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case int:
			return T(n)
		case int64:
			return T(n)
		case float64:
			return T(n)
		case T:
			return n
		}
	}
	var zero T
	return zero
}

func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

func ToSlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func ToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// JoinIDs joins IDs into the comma-separated form Jellyfin's ids parameters
// take.
func JoinIDs(ids []string) string {
	return strings.Join(ids, ",")
}

// SplitIDs returns the IDs in ids one per element, splitting any element that
// holds a comma-separated list, so that counting them matches what an ids
// parameter built from them sends.
func SplitIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		for _, part := range strings.Split(id, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func ToStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func DefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// ClampInt returns v if positive, otherwise def, and caps the result at max.
func ClampInt(v, def, max int) int {
	n := DefaultInt(v, def)
	if n > max {
		return max
	}
	return n
}

func BoolPtr(b bool) *bool {
	return &b
}

func FormatGB(bytes int64) string {
	gb := float64(bytes) / float64(BytesPerGB)
	if gb >= 100 {
		return fmt.Sprintf("%.1f GB", gb)
	}
	return fmt.Sprintf("%.2f GB", gb)
}

// MaskToken reveals only the first/last 4 characters; returns the full string
// if it's too short for masking to be meaningful.
func MaskToken(token string) string {
	if len(token) <= TokenMaskMinLen {
		return token
	}
	return token[:TokenRevealChars] + "..." + token[len(token)-TokenRevealChars:]
}

// confirmInput names the confirmation form in a tool result's input requests
// and in the client's input responses.
const confirmInput = "confirm"

// DeclinedText is the result of a call whose confirmation the user declined
// or dismissed. It names the user as the one who stopped the call and says
// that nothing changed, so the assistant reports the outcome rather than a
// vague cancellation.
const DeclinedText = "The user didn't confirm, so nothing changed."

// ConfirmationGate decides whether an operation that needs the user's
// agreement may proceed. A client that declared form elicitation always gets
// an input request unless req carries the user's accepted form answer, so a
// model cannot skip native client confirmation by passing confirm=true on its
// own; the SDK turns the input request into a direct elicitation for a client
// on a protocol before 2026-07-28. Any client without form elicitation gets a
// text warning asking it to call again with confirm=true after the user
// agrees. An unanswered or failed form fails the call, and nothing is
// performed.
func ConfirmationGate(ctx context.Context, req *mcp.CallToolRequest, confirm *bool, warning string) *mcp.CallToolResult {
	if req != nil && req.Params != nil {
		if _, ok := req.Params.InputResponses[confirmInput]; ok {
			if ConfirmedByUser(req) {
				if state, ok := ctx.Value(confirmStateKey{}).(*confirmState); ok {
					state.confirmed = true
				}
				return nil
			}
			return TextResult(DeclinedText)
		}
	}
	if req != nil && supportsFormElicitation(req.ClientCapabilities()) {
		return &mcp.CallToolResult{
			InputRequests: mcp.InputRequestMap{
				confirmInput: &mcp.ElicitParams{
					Mode:    "form",
					Message: warning,
					// The form has no fields: accepting it is the confirmation
					// and declining it is the cancel, so there is nothing to
					// tick and no way to accept without confirming.
					RequestedSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				},
			},
		}
	}
	ledger := confirmationLedgerFrom(ctx)
	if confirm != nil && *confirm {
		if ledger == nil || ledger.consume(confirmKeyFor(req, warning), time.Now()) {
			return nil
		}
		ledger.record(confirmKeyFor(req, warning), time.Now())
		return TextResult("CONFIRMATION REQUIRED\n\n" + warning +
			"\n\nPresent this preview to the user and stop—do NOT call this tool again in this turn. Only after the user replies in a new message and agrees, call this tool again with the same arguments and confirm=true.")
	}
	if ledger != nil {
		ledger.record(confirmKeyFor(req, warning), time.Now())
	}
	return TextResult("CONFIRMATION REQUIRED\n\n" + warning +
		"\n\nPresent this to the user. Only after they agree, call this tool again with the same arguments and confirm=true.")
}

type confirmLedgerKey struct{}

type confirmEntryKey struct {
	sessionID  string
	toolName   string
	argsDigest string
	warning    string
}

const maxPendingConfirmations = 256

// ConfirmationLedger tracks CONFIRMATION REQUIRED warnings issued to clients
// without form elicitation so an assistant cannot skip the impact preview by
// passing confirm=true on its first call or swap masked/omitted arguments on
// the follow-up call.
type ConfirmationLedger struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[confirmEntryKey]time.Time
}

// NewConfirmationLedger returns a ConfirmationLedger whose pending previews
// expire after ttl.
func NewConfirmationLedger(ttl time.Duration) *ConfirmationLedger {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ConfirmationLedger{
		ttl:     ttl,
		entries: make(map[confirmEntryKey]time.Time),
	}
}

// WithConfirmationLedger attaches l to ctx so ConfirmationGate enforces that a
// matching warning was issued before accepting confirm=true.
func WithConfirmationLedger(ctx context.Context, l *ConfirmationLedger) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, confirmLedgerKey{}, l)
}

func confirmationLedgerFrom(ctx context.Context) *ConfirmationLedger {
	l, _ := ctx.Value(confirmLedgerKey{}).(*ConfirmationLedger)
	return l
}

func confirmKeyFor(req *mcp.CallToolRequest, warning string) confirmEntryKey {
	var sid, name, digest string
	if req != nil {
		if req.Session != nil {
			sid = req.Session.ID()
		}
		if req.Params != nil {
			name = req.Params.Name
			digest = canonicalArgsDigest(req.Params.Arguments)
		}
	}
	return confirmEntryKey{sessionID: sid, toolName: name, argsDigest: digest, warning: warning}
}

// canonicalArgsDigest hashes req's JSON tool arguments after removing control
// flags ("confirm" and "dry_run") so parameters omitted or masked in warning
// text (such as passwords, API keys, or truncated ID lists) remain bound to
// the previewed call.
func canonicalArgsDigest(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		sum := sha256.Sum256(bytes.TrimSpace(raw))
		return hex.EncodeToString(sum[:])
	}
	delete(m, "confirm")
	delete(m, "dry_run")
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (l *ConfirmationLedger) pruneLocked(now time.Time) {
	for k, exp := range l.entries {
		if !now.Before(exp) {
			delete(l.entries, k)
		}
	}
	for len(l.entries) >= maxPendingConfirmations {
		var oldestKey confirmEntryKey
		var oldestExp time.Time
		first := true
		for k, exp := range l.entries {
			if first || exp.Before(oldestExp) {
				oldestKey, oldestExp, first = k, exp, false
			}
		}
		delete(l.entries, oldestKey)
	}
}

func (l *ConfirmationLedger) record(k confirmEntryKey, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	l.entries[k] = now.Add(l.ttl)
}

func (l *ConfirmationLedger) consume(k confirmEntryKey, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	if exp, ok := l.entries[k]; ok && now.Before(exp) {
		delete(l.entries, k)
		return true
	}
	// Fall back to matching (toolName, argsDigest, warning) when a stateful
	// bridge reconnects with a fresh session ID between the preview turn and
	// the user's approval turn.
	var matchKey confirmEntryKey
	var matchExp time.Time
	found := false
	for cand, exp := range l.entries {
		if !now.Before(exp) {
			continue
		}
		if cand.toolName == k.toolName && cand.argsDigest == k.argsDigest && cand.warning == k.warning {
			if !found || exp.Before(matchExp) {
				matchKey, matchExp, found = cand, exp, true
			}
		}
	}
	if !found {
		return false
	}
	delete(l.entries, matchKey)
	return true
}

// confirmStateKey holds a confirmState in the context of one tool call. The
// SDK answers the form for an older client inside the call and runs the
// handler again with the answer, so the outer middleware never sees the
// answered request; the gate records the confirmation here instead.
type confirmStateKey struct{}

type confirmState struct{ confirmed bool }

// WithConfirmationTracking returns a context in which ConfirmationGate records
// that the user confirmed the call, for ConfirmedInCall to read afterwards.
func WithConfirmationTracking(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmStateKey{}, &confirmState{})
}

// ConfirmedInCall reports whether a gate in this call passed on the user's
// acceptance of the confirmation form.
func ConfirmedInCall(ctx context.Context) bool {
	state, ok := ctx.Value(confirmStateKey{}).(*confirmState)
	return ok && state.confirmed
}

// ConfirmedByUser reports whether req carries the user's acceptance of the
// confirmation form.
func ConfirmedByUser(req *mcp.CallToolRequest) bool {
	if req == nil || req.Params == nil {
		return false
	}
	resp, ok := req.Params.InputResponses[confirmInput]
	if !ok {
		return false
	}
	answer, ok := resp.(*mcp.ElicitResult)
	return ok && answer.Action == "accept"
}

// confirmationNeeded reports whether ConfirmationGate will stop the call (with
// a prompt or declined notice) instead of letting the action proceed.
func confirmationNeeded(req *mcp.CallToolRequest, confirm *bool) bool {
	if req != nil && req.Params != nil {
		if _, ok := req.Params.InputResponses[confirmInput]; ok {
			return !ConfirmedByUser(req)
		}
	}
	if req != nil && supportsFormElicitation(req.ClientCapabilities()) {
		return true
	}
	return confirm == nil || !*confirm
}

// ConfirmationNeededIn reports whether ConfirmationGate will stop the call
// (with a prompt or declined notice), accounting for ctx's ConfirmationLedger.
// When a ledger is active, warning text is always computed so ConfirmationGate
// can verify the exact warning was previously issued before accepting confirm=true.
func ConfirmationNeededIn(ctx context.Context, req *mcp.CallToolRequest, confirm *bool) bool {
	if confirmationLedgerFrom(ctx) != nil {
		if req != nil && req.Params != nil {
			if _, ok := req.Params.InputResponses[confirmInput]; ok {
				return !ConfirmedByUser(req)
			}
		}
		return true
	}
	return confirmationNeeded(req, confirm)
}

// destructiveDisabledKey marks a context in which destructive actions are
// refused.
type destructiveDisabledKey struct{}

// WithDestructiveDisabled returns a context in which DestructiveGate refuses
// every action.
func WithDestructiveDisabled(ctx context.Context) context.Context {
	return context.WithValue(ctx, destructiveDisabledKey{}, true)
}

// DestructiveDisabled reports whether ctx refuses destructive actions.
func DestructiveDisabled(ctx context.Context) bool {
	v, _ := ctx.Value(destructiveDisabledKey{}).(bool)
	return v
}

// DestructiveDisabledError returns the policy refusal error when ctx refuses
// destructive actions, or nil when destructive actions are allowed. Callers
// that inspect server state before DestructiveGate can check this first so
// --disable-destructive fails immediately without extra API lookups.
func DestructiveDisabledError(ctx context.Context) *mcp.CallToolResult {
	if DestructiveDisabled(ctx) {
		return ErrResult("This action is disabled: the server runs with --disable-destructive, which refuses deletes, removals, metadata or image replacements, restores, plugin disables and uninstalls, restarts, shutdowns, revocations, cancellations, version merges and splits, Quick Connect authorizations, and password, policy, trigger, and configuration changes.")
	}
	return nil
}

// DestructiveGate is ConfirmationGate for an action that deletes, removes, or
// overwrites data, disables or removes plugins, takes the server or a user's
// access away, authorizes Quick Connect, cancels a recording, or modifies
// server, library, plugin, or user configuration. When the server runs with
// --disable-destructive, which WithDestructiveDisabled records in ctx, the
// action is refused before any confirmation is asked.
func DestructiveGate(ctx context.Context, req *mcp.CallToolRequest, confirm *bool, warning string) *mcp.CallToolResult {
	if res := DestructiveDisabledError(ctx); res != nil {
		return res
	}
	return ConfirmationGate(ctx, req, confirm, warning)
}

// supportsFormElicitation reports whether the calling client declared form
// elicitation. The capabilities are the ones sent with the request itself on
// the current protocol, where each request declares its own, and those from
// initialization on older ones; either can be absent. A client that declares
// elicitation without naming a mode is taken to support forms, as the
// protocol specifies.
func supportsFormElicitation(caps *mcp.ClientCapabilities) bool {
	if caps == nil || caps.Elicitation == nil {
		return false
	}
	e := caps.Elicitation
	return e.Form != nil || e.URL == nil
}

// FilterPrefix returns items whose lowercase form starts with the given prefix.
func FilterPrefix(items []string, prefix string) []string {
	if prefix == "" {
		return items
	}
	lower := strings.ToLower(prefix)
	var out []string
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(item), lower) {
			out = append(out, item)
		}
	}
	return out
}

// ApplyMetadataFields applies user-provided metadata fields onto an existing
// Jellyfin item map (fetched via GET). Only non-zero fields are applied.
// Genres and tags are written only as the Genres and Tags string arrays, which
// are what the item update endpoint reads. GenreItems is a response-only
// projection of Genres that the endpoint ignores, and TagItems is not a
// BaseItemDto property.
func ApplyMetadataFields(current map[string]any, args MetadataInput) {
	// Strip response-only playback/media properties (such as Trickplay nested maps)
	// that cause Jellyfin's POST /Items/{id} deserializer to return HTTP 500 on Episodes/Movies.
	for _, roKey := range []string{"Trickplay", "MediaStreams", "MediaSources", "Chapters"} {
		delete(current, roKey)
	}
	if args.Name != "" {
		current["Name"] = args.Name
	}
	if args.Overview != "" {
		current["Overview"] = args.Overview
	}
	if len(args.Genres) > 0 {
		current["Genres"] = args.Genres
	}
	if len(args.Tags) > 0 {
		current["Tags"] = args.Tags
	}
	if len(args.Studios) > 0 {
		studioObjs := make([]map[string]any, len(args.Studios))
		for i, s := range args.Studios {
			studioObjs[i] = map[string]any{"Name": s}
		}
		current["Studios"] = studioObjs
	}
	if args.Year != nil {
		current["ProductionYear"] = *args.Year
	}
	if args.CommunityRating != nil {
		current["CommunityRating"] = *args.CommunityRating
	}
	if args.OfficialRating != "" {
		current["OfficialRating"] = args.OfficialRating
	}
	if args.SortName != "" {
		current["ForcedSortName"] = args.SortName
	}
	if len(args.LockedFields) > 0 {
		current["LockedFields"] = args.LockedFields
	}
}

// BuildProviderLinks converts provider IDs into clickable URLs.
// itemType is the Jellyfin item type (such as Movie, Series, or Person) used to
// pick the correct TMDb path segment.
func BuildProviderLinks(providerIDs map[string]string, itemType string) map[string]string {
	links := make(map[string]string)
	if id, ok := providerIDs["Imdb"]; ok && id != "" {
		links["IMDb"] = "https://www.imdb.com/title/" + id
	}
	if id, ok := providerIDs["Tmdb"]; ok && id != "" {
		segment := "movie"
		switch itemType {
		case "Series", "Season", "Episode":
			segment = "tv"
		case "Person":
			segment = "person"
		}
		links["TMDb"] = "https://www.themoviedb.org/" + segment + "/" + id
	}
	if id, ok := providerIDs["Tvdb"]; ok && id != "" {
		// TheTVDB catalogs movies separately from series.
		tab := "series"
		if itemType == "Movie" {
			tab = "movie"
		}
		links["TVDB"] = "https://thetvdb.com/?id=" + id + "&tab=" + tab
	}
	return links
}
