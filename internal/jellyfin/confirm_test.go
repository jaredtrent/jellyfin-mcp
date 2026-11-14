package jellyfin

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type gatedInput struct {
	Confirm *bool `json:"confirm,omitempty"`
}

// newGatedServer serves one tool that performs its work only after
// ConfirmationGate lets it through.
func newGatedServer(performed *int) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "gated"}, func(ctx context.Context, req *mcp.CallToolRequest, in gatedInput) (*mcp.CallToolResult, any, error) {
		if res := ConfirmationGate(ctx, req, in.Confirm, "This will delete everything."); res != nil {
			return res, nil, nil
		}
		*performed++
		return TextResult("done"), nil, nil
	})
	return srv
}

type clientSetup struct {
	protocolVersion  string // empty negotiates the newest version
	answer           *mcp.ElicitResult
	elicitErr        error
	noElicitation    bool
	noMultiRoundTrip bool
	capabilities     *mcp.ClientCapabilities
}

func connectGated(t *testing.T, setup clientSetup, performed, elicited *int) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newGatedServer(performed).Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	opts := &mcp.ClientOptions{Capabilities: setup.capabilities}
	if !setup.noElicitation {
		opts.ElicitationHandler = func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			*elicited++
			return setup.answer, setup.elicitErr
		}
	}
	if setup.noMultiRoundTrip {
		opts.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	}
	var sessOpts *mcp.ClientSessionOptions
	if setup.protocolVersion != "" {
		sessOpts = &mcp.ClientSessionOptions{ProtocolVersion: setup.protocolVersion}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, opts).Connect(t.Context(), ct, sessOpts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	want := setup.protocolVersion
	if want == "" {
		want = "2026-07-28"
	}
	if got := cs.InitializeResult().ProtocolVersion; got != want {
		t.Fatalf("negotiated protocol %q, want %q", got, want)
	}
	return cs
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestConfirmationGate(t *testing.T) {
	accept := &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}
	acceptFalse := &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}
	decline := &mcp.ElicitResult{Action: "decline"}
	cancel := &mcp.ElicitResult{Action: "cancel"}
	declineConfirming := &mcp.ElicitResult{Action: "decline", Content: map[string]any{"confirm": true}}

	tests := []struct {
		name          string
		setup         clientSetup
		confirm       *bool
		wantText      string
		wantPerformed bool
		wantElicited  int
	}{
		{name: "confirm=true does not skip form when elicitation is supported (declined)", setup: clientSetup{answer: decline}, confirm: BoolPtr(true), wantText: DeclinedText, wantElicited: 1},
		{name: "confirm=true still elicits when elicitation is supported (accepted)", setup: clientSetup{answer: accept}, confirm: BoolPtr(true), wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "confirm=false still asks", setup: clientSetup{answer: accept}, confirm: BoolPtr(false), wantText: "done", wantPerformed: true, wantElicited: 1},

		{name: "current protocol: accept", setup: clientSetup{answer: accept}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "current protocol: accept with stray content", setup: clientSetup{answer: acceptFalse}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "current protocol: decline", setup: clientSetup{answer: decline}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: cancel", setup: clientSetup{answer: cancel}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: decline carrying confirm", setup: clientSetup{answer: declineConfirming}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: no elicitation", setup: clientSetup{noElicitation: true}, wantText: "CONFIRMATION REQUIRED"},
		{name: "current protocol: no elicitation confirm=true proceeds", setup: clientSetup{noElicitation: true}, confirm: BoolPtr(true), wantText: "done", wantPerformed: true},

		{name: "2025-11-25: accept", setup: clientSetup{protocolVersion: "2025-11-25", answer: accept}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "2025-11-25: confirm=true still elicits", setup: clientSetup{protocolVersion: "2025-11-25", answer: decline}, confirm: BoolPtr(true), wantText: DeclinedText, wantElicited: 1},
		{name: "2025-11-25: decline", setup: clientSetup{protocolVersion: "2025-11-25", answer: decline}, wantText: DeclinedText, wantElicited: 1},
		{name: "2025-11-25: cancel", setup: clientSetup{protocolVersion: "2025-11-25", answer: cancel}, wantText: DeclinedText, wantElicited: 1},
		{name: "2025-11-25: no elicitation", setup: clientSetup{protocolVersion: "2025-11-25", noElicitation: true}, wantText: "CONFIRMATION REQUIRED"},
		{name: "2025-11-25: no elicitation confirm=true proceeds", setup: clientSetup{protocolVersion: "2025-11-25", noElicitation: true}, confirm: BoolPtr(true), wantText: "done", wantPerformed: true},

		{
			name: "URL-only elicitation gets the text warning",
			setup: clientSetup{answer: accept, capabilities: &mcp.ClientCapabilities{
				Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
			}},
			wantText: "CONFIRMATION REQUIRED",
		},
		{
			name: "URL-only elicitation confirm=true proceeds",
			setup: clientSetup{answer: decline, capabilities: &mcp.ClientCapabilities{
				Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
			}},
			confirm:       BoolPtr(true),
			wantText:      "done",
			wantPerformed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			performed, elicited := 0, 0
			cs := connectGated(t, tt.setup, &performed, &elicited)
			args := map[string]any{}
			if tt.confirm != nil {
				args["confirm"] = *tt.confirm
			}
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "gated", Arguments: args})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if got := text(res); !strings.Contains(got, tt.wantText) {
				t.Errorf("result %q, want it to contain %q", got, tt.wantText)
			}
			if (performed == 1) != tt.wantPerformed || performed > 1 {
				t.Errorf("performed %d times, want performed=%v", performed, tt.wantPerformed)
			}
			if elicited != tt.wantElicited {
				t.Errorf("elicited %d times, want %d", elicited, tt.wantElicited)
			}
		})
	}
}

// A client on the current protocol that handles input requests itself sees
// an input-required result that carries the form and no content.
func TestConfirmationGateReturnsInputRequest(t *testing.T) {
	performed, elicited := 0, 0
	cs := connectGated(t, clientSetup{answer: &mcp.ElicitResult{Action: "accept"}, noMultiRoundTrip: true}, &performed, &elicited)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "gated", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsInput() {
		t.Fatalf("NeedsInput() = false; result %+v", res)
	}
	if len(res.Content) != 0 {
		t.Errorf("input-required result carries content: %q", text(res))
	}
	form, ok := res.InputRequests[confirmInput].(*mcp.ElicitParams)
	if !ok {
		t.Fatalf("InputRequests[%q] = %T, want *mcp.ElicitParams", confirmInput, res.InputRequests[confirmInput])
	}
	if !strings.Contains(form.Message, "delete everything") {
		t.Errorf("form message %q does not carry the warning", form.Message)
	}

	// The form has no fields, so there is nothing to tick: accepting it
	// confirms, whatever content the client sends back.
	if schema, _ := form.RequestedSchema.(map[string]any); len(ToMap(schema["properties"])) != 0 {
		t.Errorf("form has fields %v, want none", schema["properties"])
	}
	for _, action := range []string{"decline", "cancel"} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
			Name:           "gated",
			Arguments:      map[string]any{},
			InputResponses: mcp.InputResponseMap{confirmInput: &mcp.ElicitResult{Action: action, Content: map[string]any{"confirm": true}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if text(res) != DeclinedText || performed != 0 {
			t.Errorf("%s: result %q, performed %d; want the declined text", action, text(res), performed)
		}
	}

	// Retrying with the accepted answer performs the operation.
	res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:           "gated",
		Arguments:      map[string]any{},
		InputResponses: mcp.InputResponseMap{confirmInput: &mcp.ElicitResult{Action: "accept", Content: map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text(res) != "done" || performed != 1 {
		t.Errorf("retry result %q, performed %d; want done once", text(res), performed)
	}
}

// A client whose elicitation fails gets an error for the call, and nothing is
// performed.
func TestConfirmationGateElicitationFailure(t *testing.T) {
	for _, version := range []string{"", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			performed, elicited := 0, 0
			cs := connectGated(t, clientSetup{protocolVersion: version, elicitErr: errors.New("no display")}, &performed, &elicited)
			if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "gated", Arguments: map[string]any{}}); err == nil {
				t.Error("CallTool succeeded, want the elicitation failure")
			}
			if performed != 0 || elicited != 1 {
				t.Errorf("performed %d, elicited %d; want 0 and 1", performed, elicited)
			}
		})
	}
}

// rawGatedConn connects a raw JSON-RPC connection to a gated server. The
// returned function sends one frame and returns the encoded reply.
func rawGatedConn(t *testing.T, performed *int) func(frame string) string {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newGatedServer(performed).Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := ct.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return func(frame string) string {
		t.Helper()
		msg, err := jsonrpc.DecodeMessage([]byte(frame))
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Write(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
		if req, ok := msg.(*jsonrpc.Request); ok && !req.IsCall() {
			return "" // a notification gets no reply
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		reply, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		b, err := jsonrpc.EncodeMessage(reply)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

// An initialize request without capabilities must not crash the server: the
// gate falls back to the text warning and the session keeps serving.
func TestConfirmationGateWithoutCapabilities(t *testing.T) {
	performed := 0
	send := rawGatedConn(t, &performed)

	if got := send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"raw","version":"0"}}}`); !strings.Contains(got, `"protocolVersion"`) {
		t.Fatalf("initialize response: %s", got)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	for id := 2; id <= 3; id++ {
		if got := send(`{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"tools/call","params":{"name":"gated","arguments":{}}}`); !strings.Contains(got, "CONFIRMATION REQUIRED") {
			t.Fatalf("call %d response: %s", id, got)
		}
	}
	if performed != 0 {
		t.Errorf("operation performed without confirmation")
	}
}

// On the current protocol each request declares its own client capabilities,
// so calls on one connection that declare different capabilities are each
// answered by what they declared.
func TestConfirmationGateReadsCapabilitiesPerRequest(t *testing.T) {
	performed := 0
	send := rawGatedConn(t, &performed)
	call := func(id int, caps string) string {
		return send(`{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"tools/call","params":{"name":"gated","arguments":{},` +
			`"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":` + caps + `}}}`)
	}
	for i, tt := range []struct {
		caps string
		want string
	}{
		{`{}`, "CONFIRMATION REQUIRED"},
		{`{"elicitation":{"form":{}}}`, `"inputRequests"`},
		{`{}`, "CONFIRMATION REQUIRED"},
		{`{"elicitation":{}}`, `"inputRequests"`},
		{`{"elicitation":{"url":{}}}`, "CONFIRMATION REQUIRED"},
		{`{"elicitation":{"form":{},"url":{}}}`, `"inputRequests"`},
	} {
		if got := call(i+1, tt.caps); !strings.Contains(got, tt.want) {
			t.Errorf("call %d declaring %s: response %s, want it to contain %s", i+1, tt.caps, got, tt.want)
		}
	}
	if performed != 0 {
		t.Errorf("operation performed without confirmation")
	}
}

// DestructiveGate refuses every action when the context carries the
// --disable-destructive policy, without asking for confirmation, and is
// otherwise the confirmation gate.
func TestDestructiveGate(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "gated"}}
	if got := DestructiveGate(context.Background(), req, BoolPtr(true), "warning"); got != nil {
		t.Errorf("enabled with confirm=true: got %+v, want nil", got)
	}
	if got := DestructiveGate(context.Background(), req, nil, "delete everything"); got == nil || !strings.Contains(text(got), "CONFIRMATION REQUIRED") {
		t.Errorf("enabled without confirmation: got %+v, want the confirmation warning", got)
	}
	ctx := WithDestructiveDisabled(context.Background())
	if !DestructiveDisabled(ctx) || DestructiveDisabled(context.Background()) {
		t.Fatal("DestructiveDisabled does not follow the context")
	}
	for _, confirm := range []*bool{nil, BoolPtr(true)} {
		got := DestructiveGate(ctx, req, confirm, "delete everything")
		if got == nil || !got.IsError || !strings.Contains(text(got), "--disable-destructive") {
			t.Errorf("disabled with confirm=%v: got %+v, want an error naming the flag", confirm, got)
		}
		if got != nil && len(got.InputRequests) != 0 {
			t.Errorf("disabled with confirm=%v: asked for input %v", confirm, got.InputRequests)
		}
	}
}

func TestConfirmationLedgerBlocksPrematureAndTamperedConfirm(t *testing.T) {
	ledger := NewConfirmationLedger(0)
	ctx := WithConfirmationLedger(context.Background(), ledger)
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "jellyfin_metadata"}}

	// 1. Passing confirm=true on the first call (before any preview) is blocked and records the preview.
	first := ConfirmationGate(ctx, req, BoolPtr(true), "Merge A and B?")
	if first == nil || !strings.Contains(text(first), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected premature confirm=true to be blocked, got %+v", first)
	}

	// 2. Changing the target warning on the follow-up call is still blocked.
	tampered := ConfirmationGate(ctx, req, BoolPtr(true), "Merge A and C?")
	if tampered == nil || !strings.Contains(text(tampered), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected tampered warning to be blocked, got %+v", tampered)
	}

	// 3. Confirming the previously previewed warning succeeds once.
	if ok := ConfirmationGate(ctx, req, BoolPtr(true), "Merge A and B?"); ok != nil {
		t.Fatalf("expected previewed confirm=true to succeed, got %+v", ok)
	}

	// 4. Reusing the consumed confirmation requires a fresh preview.
	replay := ConfirmationGate(ctx, req, BoolPtr(true), "Merge A and B?")
	if replay == nil || !strings.Contains(text(replay), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected replayed confirm=true to require fresh confirmation, got %+v", replay)
	}

	// 5. Swapping masked/omitted arguments (such as password) while keeping identical warning text is blocked.
	pwWarning := "Change the password for user 'User A'?"
	pwPreviewReq := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "jellyfin_users",
		Arguments: []byte(`{"action":"update_password","user_id":"User A","password":"expected-pw"}`),
	}}
	if pre := ConfirmationGate(ctx, pwPreviewReq, nil, pwWarning); pre == nil || !strings.Contains(text(pre), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected password preview prompt, got %+v", pre)
	}
	pwSwappedReq := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "jellyfin_users",
		Arguments: []byte(`{"action":"update_password","user_id":"User A","password":"different-pw","confirm":true}`),
	}}
	if swapped := ConfirmationGate(ctx, pwSwappedReq, BoolPtr(true), pwWarning); swapped == nil || !strings.Contains(text(swapped), "CONFIRMATION REQUIRED") {
		t.Fatalf("expected swapped password argument to be blocked, got %+v", swapped)
	}
	pwConfirmReq := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "jellyfin_users",
		Arguments: []byte(`{"password":"expected-pw","user_id":"User A","action":"update_password","confirm":true}`),
	}}
	if ok := ConfirmationGate(ctx, pwConfirmReq, BoolPtr(true), pwWarning); ok != nil {
		t.Fatalf("expected matching canonical JSON arguments to confirm cleanly, got %+v", ok)
	}
}

func TestMergeDiffAndFormatConfig(t *testing.T) {
	base := map[string]any{
		"EnableEmbeddedTitles":     false,
		"EnableRealtimeMonitor":    true,
		"DisabledSubtitleFetchers": []any{"Subtitle Provider A"},
		"Nested": map[string]any{
			"Keep":   "same",
			"Change": 1,
		},
		"ApiKey":     "old-secret",
		"WebhookUrl": "https://user:pass@example.com/hook?token=abc",
	}
	patch := map[string]any{
		"enableembeddedtitles": true,
		"Nested": map[string]any{
			"Change": 2,
		},
		"ApiKey":     "new-secret",
		"WebhookUrl": "https://hooks.example.com/v1/notify?key=xyz",
	}

	merged := MergeConfig(base, patch)
	if merged["EnableEmbeddedTitles"] != true {
		t.Fatalf("expected case-insensitive merge onto EnableEmbeddedTitles, got %+v", merged)
	}
	if _, stray := merged["enableembeddedtitles"]; stray {
		t.Fatalf("unexpected duplicate lowercase key in %+v", merged)
	}
	if merged["EnableRealtimeMonitor"] != true {
		t.Errorf("unmentioned field was dropped: %+v", merged)
	}
	nested, _ := AsConfigMap(merged["Nested"])
	if nested["Keep"] != "same" || nested["Change"] != 2 {
		t.Errorf("nested merge = %+v", nested)
	}

	changes := DiffConfig(base, merged)
	formatted := FormatConfigChanges(changes)
	for _, want := range []string{
		`ApiKey: (hidden) -> (hidden)`,
		`EnableEmbeddedTitles: false -> true`,
		`Nested.Change: 1 -> 2`,
		`WebhookUrl: "https://example.com/…" -> "https://hooks.example.com/…"`,
	} {
		if !strings.Contains(formatted, want) {
			t.Errorf("FormatConfigChanges = %q, want %q", formatted, want)
		}
	}
	for _, secret := range []string{"old-secret", "new-secret", "pass", "token=abc", "key=xyz"} {
		if strings.Contains(formatted, secret) {
			t.Errorf("FormatConfigChanges leaked %q in %q", secret, formatted)
		}
	}

	// Clearing a non-empty nested map to {} reports the wiped map rather than 0 changes.
	wiped := MergeConfig(base, map[string]any{"Nested": map[string]any{}})
	wipedFormatted := FormatConfigChanges(DiffConfig(base, wiped))
	if !strings.Contains(wipedFormatted, "Nested: 2 fields -> {}") {
		t.Errorf("clearing nested map = %q, want 'Nested: 2 fields -> {}'", wipedFormatted)
	}

	// Non-string secrets (slices/maps) and URLs with credentials/queries on non-*Url keys are sanitized.
	secretAndURL := FormatConfigChanges(DiffConfig(nil, map[string]any{
		"ApiKeys":      []any{"placeholder-key-1", "placeholder-key-2"},
		"TunerAddress": "http://admin:placeholder-pw@192.0.2.50/lineup.m3u?token=placeholder-token",
	}))
	if !strings.Contains(secretAndURL, "ApiKeys: (hidden)") || !strings.Contains(secretAndURL, `TunerAddress: "http://192.0.2.50/…"`) {
		t.Errorf("secret slice / credential URL format = %q", secretAndURL)
	}
	for _, leak := range []string{"placeholder-key", "placeholder-pw", "placeholder-token"} {
		if strings.Contains(secretAndURL, leak) {
			t.Errorf("FormatConfigChanges leaked %q in %q", leak, secretAndURL)
		}
	}

	// More than 6 changes truncates cleanly.
	many := map[string]any{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8}
	manyText := FormatConfigChanges(DiffConfig(nil, many))
	if !strings.Contains(manyText, "and 2 more") {
		t.Errorf("expected truncation suffix, got %q", manyText)
	}

	if s, ok := AsSlice([]any{}); !ok || len(s) != 0 {
		t.Errorf("AsSlice([]) = %v, %v; want empty slice, true", s, ok)
	}
	if _, ok := AsSlice("not-a-slice"); ok {
		t.Error("AsSlice(string) = true, want false")
	}
}
