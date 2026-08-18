package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// concurrentCallers is the number of goroutines that share one Client
// in TestConcurrentListToolsAndCallTool.
const concurrentCallers = 16

// TestConcurrentListToolsAndCallTool mixes ListTools and CallTool
// calls from many goroutines over one Client, proving client.go's
// "safe for concurrent use" claim for the two methods only
// client_progress_test.go's progress-token case covered before.
//
// Each CallTool sends a token unique to its goroutine and asserts the
// echoed result carries that same token, so a result crossing between
// two in-flight calls fails the test rather than passing silently.
// Run under go test -race.
func TestConcurrentListToolsAndCallTool(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo1")
	addEchoTool(server, "echo2")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, concurrentCallers)
	for i := 0; i < concurrentCallers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Alternate the two methods so both run against a client
			// another goroutine is already using.
			if i%2 == 0 {
				got, err := c.ListTools(ctx)
				if err != nil {
					errs[i] = fmt.Errorf("ListTools: %w", err)
					return
				}
				if len(got) != 2 {
					errs[i] = fmt.Errorf("ListTools returned %d tools, want 2", len(got))
				}
				return
			}
			token := fmt.Sprintf("token-%d", i)
			out, err := c.CallTool(ctx, "echo1", echoArgs{Message: token})
			if err != nil {
				errs[i] = fmt.Errorf("CallTool: %w", err)
				return
			}
			res, ok := out.Value.(*CallResult)
			if !ok {
				errs[i] = fmt.Errorf("CallTool result is %T, want *CallResult", out.Value)
				return
			}
			if res.IsError {
				errs[i] = fmt.Errorf("CallTool reported a tool-level error for %q", token)
				return
			}
			if len(res.Content) != 1 {
				errs[i] = fmt.Errorf("CallTool returned %d content blocks, want 1", len(res.Content))
				return
			}
			if got := res.Content[0].Text; got != token {
				errs[i] = fmt.Errorf("CallTool echoed %q, want %q: a result crossed between calls", got, token)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}
