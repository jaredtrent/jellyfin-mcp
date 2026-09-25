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
		{name: "confirm=true skips the form", setup: clientSetup{answer: decline}, confirm: BoolPtr(true), wantText: "done", wantPerformed: true},
		{name: "confirm=false still asks", setup: clientSetup{answer: accept}, confirm: BoolPtr(false), wantText: "done", wantPerformed: true, wantElicited: 1},

		{name: "current protocol: accept", setup: clientSetup{answer: accept}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "current protocol: accept with stray content", setup: clientSetup{answer: acceptFalse}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "current protocol: decline", setup: clientSetup{answer: decline}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: cancel", setup: clientSetup{answer: cancel}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: decline carrying confirm", setup: clientSetup{answer: declineConfirming}, wantText: DeclinedText, wantElicited: 1},
		{name: "current protocol: no elicitation", setup: clientSetup{noElicitation: true}, wantText: "CONFIRMATION REQUIRED"},

		{name: "2025-11-25: accept", setup: clientSetup{protocolVersion: "2025-11-25", answer: accept}, wantText: "done", wantPerformed: true, wantElicited: 1},
		{name: "2025-11-25: decline", setup: clientSetup{protocolVersion: "2025-11-25", answer: decline}, wantText: DeclinedText, wantElicited: 1},
		{name: "2025-11-25: cancel", setup: clientSetup{protocolVersion: "2025-11-25", answer: cancel}, wantText: DeclinedText, wantElicited: 1},
		{name: "2025-11-25: no elicitation", setup: clientSetup{protocolVersion: "2025-11-25", noElicitation: true}, wantText: "CONFIRMATION REQUIRED"},

		{
			name: "URL-only elicitation gets the text warning",
			setup: clientSetup{answer: accept, capabilities: &mcp.ClientCapabilities{
				Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
			}},
			wantText: "CONFIRMATION REQUIRED",
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
