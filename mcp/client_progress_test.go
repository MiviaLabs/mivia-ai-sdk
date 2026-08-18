package mcp

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// progressWaitTimeout bounds how long a test waits for a notification
// that the fixture server already sent. The SDK dispatches an
// incoming notification on its own goroutine, independent of the
// goroutine that unblocks the matching CallTool response (see
// registerHandler in progress.go), so a notification can legitimately
// arrive a short time after CallToolWithProgress has already
// returned; these tests wait for it instead of assuming it already
// landed.
const progressWaitTimeout = 2 * time.Second

// progressNotification is one call to a test's ProgressHandler,
// recorded onto a channel so the test goroutine can wait for it.
type progressNotification struct {
	token    any
	progress float64
}

// collectProgress returns a ProgressHandler that records every call
// onto a channel, and a function that waits for want recordings,
// failing t if they do not all arrive within progressWaitTimeout.
func collectProgress(t *testing.T, want int) (ProgressHandler, func() []progressNotification) {
	t.Helper()
	ch := make(chan progressNotification, want)
	handler := func(_ context.Context, token any, _ string, p, _ float64) {
		ch <- progressNotification{token: token, progress: p}
	}
	wait := func() []progressNotification {
		t.Helper()
		deadline := time.After(progressWaitTimeout)
		got := make([]progressNotification, 0, want)
		for len(got) < want {
			select {
			case n := <-ch:
				got = append(got, n)
			case <-deadline:
				t.Fatalf("received %d of %d notifications within %s", len(got), want, progressWaitTimeout)
			}
		}
		return got
	}
	return handler, wait
}

// addProgressTool registers a tool whose handler sends steps
// notifications of increasing Progress, using whatever progress token
// the call carried, before it returns its final result.
func addProgressTool(server *mcpsdk.Server, name string, steps int) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: name, Description: name + " tool"},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, _ echoArgs) (*mcpsdk.CallToolResult, any, error) {
			if token := req.Params.GetProgressToken(); token != nil {
				for i := 0; i < steps; i++ {
					_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
						ProgressToken: token,
						Message:       "step",
						Progress:      float64(i),
						Total:         float64(steps),
					})
				}
			}
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "done"}},
			}, nil, nil
		})
}

func TestCallToolWithProgressReceivesEveryNotification(t *testing.T) {
	server := newFixtureServer(nil)
	addProgressTool(server, "progress", 3)
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	onProgress, wait := collectProgress(t, 3)
	_, err := c.CallToolWithProgress(context.Background(), "progress", map[string]any{}, onProgress)
	if err != nil {
		t.Fatalf("CallToolWithProgress: %v", err)
	}

	got := wait()
	for i, n := range got {
		if n.progress != float64(i) {
			t.Fatalf("got[%d].progress = %v, want %v (out of order)", i, n.progress, float64(i))
		}
	}
	first := got[0].token
	for i, n := range got {
		if n.token != first {
			t.Fatalf("got[%d].token = %v, want %v (every notification for one call shares a token)", i, n.token, first)
		}
	}
}

func TestConcurrentCallToolWithProgressDoesNotMixTokens(t *testing.T) {
	server := newFixtureServer(nil)
	addProgressTool(server, "progress", 4)
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	onA, waitA := collectProgress(t, 4)
	onB, waitB := collectProgress(t, 4)

	go func() {
		if _, err := c.CallToolWithProgress(context.Background(), "progress", map[string]any{}, onA); err != nil {
			t.Errorf("call A: %v", err)
		}
	}()
	go func() {
		if _, err := c.CallToolWithProgress(context.Background(), "progress", map[string]any{}, onB); err != nil {
			t.Errorf("call B: %v", err)
		}
	}()

	a, b := waitA(), waitB()
	seenA := map[any]bool{}
	for _, n := range a {
		seenA[n.token] = true
	}
	for _, n := range b {
		if seenA[n.token] {
			t.Fatalf("call B observed token %v, also seen by call A", n.token)
		}
	}
}

func TestCallToolFallsBackToSessionWideHandler(t *testing.T) {
	server := newFixtureServer(nil)
	addProgressTool(server, "progress", 2)

	onProgress, wait := collectProgress(t, 2)
	c := connectFixture(t, server, ClientOptions{OnProgress: onProgress})
	defer c.Close()

	if _, err := c.CallTool(context.Background(), "progress", map[string]any{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	wait()
}

// TestHandleProgressDropsNonStringToken proves handleProgress ignores
// a notification whose token is not the string type mintToken always
// produces, instead of panicking or misdelivering it to an unrelated
// handler. A wrong-but-passing implementation that skipped the type
// assertion entirely, and instead compared token with reflect.DeepEqual
// or fmt.Sprint against every registered key, could still satisfy
// every other progress test in this file, since every real call always
// mints a string token; only a directly-constructed non-string token
// exercises the type-assertion failure branch itself.
func TestHandleProgressDropsNonStringToken(t *testing.T) {
	server := newFixtureServer(nil)
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	called := false
	c.opts.OnProgress = func(context.Context, any, string, float64, float64) {
		called = true
	}
	c.handleProgress(context.Background(), &mcpsdk.ProgressNotificationClientRequest{
		Params: &mcpsdk.ProgressNotificationParams{
			ProgressToken: 42,
			Message:       "step",
			Progress:      1,
		},
	})
	if called {
		t.Fatal("handleProgress invoked OnProgress for a non-string token, want it dropped")
	}
}

func TestCallToolWithNoHandlerDropsNotifications(t *testing.T) {
	server := newFixtureServer(nil)
	addProgressTool(server, "progress", 2)
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	out, err := c.CallTool(context.Background(), "progress", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok {
		t.Fatalf("out.Value is %T, want *CallResult", out.Value)
	}
	if result.IsError {
		t.Fatal("result.IsError = true, want false")
	}
}
