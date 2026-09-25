package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructionsMiddleware gives each initialize and server/discover result the
// server instructions as of the time now reports, so the current date they
// state is the date the client connects rather than the date the server
// started.
func instructionsMiddleware(now func() time.Time) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			switch r := result.(type) {
			case *mcp.InitializeResult:
				r.Instructions = serverInstructions(now())
			case *mcp.DiscoverResult:
				r.Instructions = serverInstructions(now())
			}
			return result, err
		}
	}
}

// timingMiddleware logs the duration of every tools/call to stderr.
func timingMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			result, err := next(ctx, method, req)
			if method != "tools/call" {
				return result, err
			}
			toolName := "unknown"
			if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
				toolName = p.Name
			}
			log.Printf("%s(%s): %s", method, toolName, time.Since(start))
			return result, err
		}
	}
}

// plainErrors is a receiving middleware that strips the structured content
// from a failed tool call. A tool with an output type gets a zero-valued
// structured block on every result, and a client that prefers structured
// content would show that block, an empty success, instead of the error text.
func plainErrors() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if r, ok := result.(*mcp.CallToolResult); ok && r != nil && r.IsError {
				r.StructuredContent = nil
			}
			return result, err
		}
	}
}

// elicitationTimeout is how long a confirmation form may wait for the user's
// answer: long enough to read the warning and decide, but finite.
var elicitationTimeout = 10 * time.Minute

// boundElicitation is a sending middleware that gives up on an
// elicitation/create request, the confirmation form asked of a client on a
// protocol before statelessProtocolVersion, once limit has passed. When it gives up, the tool call
// fails and nothing is performed.
//
// This works around an SDK limitation. The SDK's ServerSession.Close waits for
// the requests the server sent to be answered, so a form left open by a client
// that went away would keep its session, the session's subscriptions, and the
// resource poller alive for the life of the process, and would block the
// client's DELETE. The right fix is for the SDK to end a closing session's
// outstanding requests.
func boundElicitation(limit time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "elicitation/create" {
				return next(ctx, method, req)
			}
			ctx, cancel := context.WithTimeout(ctx, limit)
			defer cancel()
			result, err := next(ctx, method, req)
			if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, fmt.Errorf("the confirmation form was not answered within %v, so nothing was done: %w", limit, err)
			}
			return result, err
		}
	}
}
