package agentloop_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	sdkagentloop "github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// surfaceRotationCompleter scripts two calls: first a tool_calls
// response naming alpha, then a final text. It records every request
// so the test can assert what tool definitions each iteration's
// request advertised.
type surfaceRotationCompleter struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (c *surfaceRotationCompleter) Name() string { return "surface-rotation" }

func (c *surfaceRotationCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := len(c.reqs)
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	if idx == 0 {
		call := provider.ToolCall{ID: "call-alpha", Name: "alpha", Index: 0}
		return provider.Response{
			Message:      provider.Message{Role: provider.RoleAssistant},
			ToolCalls:    []provider.ToolCall{call},
			FinishReason: "tool_calls",
		}, nil
	}
	return provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func (c *surfaceRotationCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("surfaceRotationCompleter: ChatStream not supported")
}

func (c *surfaceRotationCompleter) reqCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

func (c *surfaceRotationCompleter) toolsAt(idx int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.reqs[idx].Tools))
	for _, d := range c.reqs[idx].Tools {
		names = append(names, d.Name)
	}
	return names
}

// TestSurfaceHookRotatesAdvertisedSetFromSecondIteration pins the
// Surface contract: iteration 1 runs on the Options-level Tools; from
// iteration 2 onward the hook's Advertised set replaces the offered
// definitions wholesale, while Registry resolution keeps working for
// the narrowed set.
func TestSurfaceHookRotatesAdvertisedSetFromSecondIteration(t *testing.T) {
	alphaDef := provider.ToolDefinition{Name: "alpha", Schema: []byte(`{"type":"object"}`)}

	reg := tools.New()
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.Add(&schemaEchoTool{name: name, schema: []byte(`{"type":"object"}`)}); err != nil {
			t.Fatal(err)
		}
	}

	comp := &surfaceRotationCompleter{}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     comp,
		Tools:         reg,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			// From step 2 on, offer ONLY alpha.
			return &sdkagentloop.Surface{Advertised: []provider.ToolDefinition{alphaDef}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stop != sdkagentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want completed", res.Stop)
	}
	if comp.reqCount() != 2 {
		t.Fatalf("completer calls = %d, want 2", comp.reqCount())
	}
	got1 := comp.toolsAt(0)
	if len(got1) != 2 {
		t.Fatalf("iteration-1 tools = %v, want both alpha and beta (Options-level set)", got1)
	}
	got2 := comp.toolsAt(1)
	if len(got2) != 1 || got2[0] != "alpha" {
		t.Fatalf("iteration-2 tools = %v, want [alpha] (hook-narrowed set)", got2)
	}
}

// TestSurfaceHookNilReturnKeepsPrior pins the nil-return contract: a
// hook that returns nil from step 2 leaves the Options-level surface
// in place.
func TestSurfaceHookNilReturnKeepsPrior(t *testing.T) {
	reg := tools.New()
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.Add(&schemaEchoTool{name: name, schema: []byte(`{"type":"object"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	comp := &surfaceRotationCompleter{}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     comp,
		Tools:         reg,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if got := comp.toolsAt(1); len(got) != 2 {
		t.Fatalf("iteration-2 tools = %v, want the unchanged 2-tool Options-level set", got)
	}
}

// TestSurfaceHookPanicFailsRunClosed pins the fail-closed contract: a
// panicking Surface hook converts to a Run error naming the hook, not
// a process crash and not a half-rotated iteration.
func TestSurfaceHookPanicFailsRunClosed(t *testing.T) {
	reg := tools.New()
	if err := reg.Add(&schemaEchoTool{name: "alpha", schema: []byte(`{"type":"object"}`)}); err != nil {
		t.Fatal(err)
	}
	comp := &surfaceRotationCompleter{}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     comp,
		Tools:         reg,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			panic("hook exploded")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if runErr == nil || !contains(runErr.Error(), "Surface hook panicked") {
		t.Fatalf("err = %v, want the Surface-hook-panic failure", runErr)
	}
}

// TestSurfaceConcurrentRunDoesNotRace proves that sharing one Loop with
// Surface hook across concurrent Run calls does not race or corrupt Loop fields.
func TestSurfaceConcurrentRunDoesNotRace(t *testing.T) {
	alphaDef := provider.ToolDefinition{Name: "alpha", Schema: []byte(`{"type":"object"}`)}
	reg := tools.New()
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.Add(&schemaEchoTool{name: name, schema: []byte(`{"type":"object"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     &surfaceRotationCompleter{},
		Tools:         reg,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			return &sdkagentloop.Surface{Advertised: []provider.ToolDefinition{alphaDef}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rerr := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
			if rerr != nil {
				t.Errorf("Run error: %v", rerr)
			}
		}()
	}
	wg.Wait()
}

// TestSurfaceHookScopeOverrideAndRetain proves that Surface{Scope: newScope} overrides
// the previous scope, while Surface{Scope: nil} retains the previous scope.
func TestSurfaceHookScopeOverrideAndRetain(t *testing.T) {
	reg := tools.New()
	for _, name := range []string{"alpha", "beta"} {
		if err := reg.Add(&schemaEchoTool{name: name, schema: []byte(`{"type":"object"}`), result: name + "-out"}); err != nil {
			t.Fatal(err)
		}
	}
	alphaDef := provider.ToolDefinition{Name: "alpha", Schema: []byte(`{"type":"object"}`)}
	betaDef := provider.ToolDefinition{Name: "beta", Schema: []byte(`{"type":"object"}`)}

	// Initial scope allows both.
	initScope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"alpha", "beta"}})

	// New scope denies alpha.
	denyAlphaScope := tools.NewScope(tools.ScopeOptions{ExtraDenylist: []string{"alpha"}})

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "alpha", Index: 0, Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "alpha", Index: 0, Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}

	var step int
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     completer,
		Tools:         reg,
		Scope:         initScope,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			step++
			if step == 1 {
				// Step 1 of Surface hook (iteration 2): deny alpha via new scope.
				return &sdkagentloop.Surface{
					Advertised: []provider.ToolDefinition{alphaDef, betaDef},
					Scope:      denyAlphaScope,
				}
			}
			// Step 2: nil Scope retains denyAlphaScope.
			return &sdkagentloop.Surface{
				Advertised: []provider.ToolDefinition{alphaDef, betaDef},
				Scope:      nil,
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stop != sdkagentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	// res.History:
	// 0: User "hi"
	// 1: Assistant call-1
	// 2: Tool call-1 result ("alpha-out")
	// 3: Assistant call-2
	// 4: Tool call-2 result (error because scope denied alpha)
	// 5: Assistant "done"
	if len(res.History) < 5 {
		t.Fatalf("history length = %d, want at least 5", len(res.History))
	}
	if res.History[2].Content != "alpha-out" {
		t.Fatalf("iter 1 tool result = %q, want alpha-out", res.History[2].Content)
	}
	if !contains(res.History[4].Content, sdkagentloop.ToolErrorPrefix) {
		t.Fatalf("iter 2 tool result = %q, want tool error due to scope denial", res.History[4].Content)
	}
}

// TestSurfaceHookRegistryOverrideAndRetain proves that Surface{Registry: newReg}
// replaces the registry, and Surface{Registry: nil} retains the previous registry.
func TestSurfaceHookRegistryOverrideAndRetain(t *testing.T) {
	reg1 := tools.New()
	if err := reg1.Add(&schemaEchoTool{name: "alpha", schema: []byte(`{"type":"object"}`), result: "reg1-alpha"}); err != nil {
		t.Fatal(err)
	}

	reg2 := tools.New()
	if err := reg2.Add(&schemaEchoTool{name: "alpha", schema: []byte(`{"type":"object"}`), result: "reg2-alpha"}); err != nil {
		t.Fatal(err)
	}

	alphaDef := provider.ToolDefinition{Name: "alpha", Schema: []byte(`{"type":"object"}`)}

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "alpha", Index: 0, Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "alpha", Index: 0, Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}

	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     completer,
		Tools:         reg1,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			return &sdkagentloop.Surface{
				Advertised: []provider.ToolDefinition{alphaDef},
				Registry:   reg2,
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stop != sdkagentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if len(res.History) < 5 {
		t.Fatalf("history length = %d, want at least 5", len(res.History))
	}
	if res.History[2].Content != "reg1-alpha" {
		t.Fatalf("iter 1 tool result = %q, want reg1-alpha", res.History[2].Content)
	}
	if res.History[4].Content != "reg2-alpha" {
		t.Fatalf("iter 2 tool result = %q, want reg2-alpha", res.History[4].Content)
	}
}

// TestSurfaceHookInvalidSchemaFailsRun proves that advertising a tool definition with
// an invalid schema in Surface causes schema compilation error (ErrInvalidSchema).
func TestSurfaceHookInvalidSchemaFailsRun(t *testing.T) {
	reg := tools.New()
	if err := reg.Add(&schemaEchoTool{name: "alpha", schema: []byte(`{"type":"object"}`)}); err != nil {
		t.Fatal(err)
	}

	comp := &surfaceRotationCompleter{}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:     comp,
		Tools:         reg,
		Model:         "m",
		MaxIterations: 4,
		Surface: func() *sdkagentloop.Surface {
			return &sdkagentloop.Surface{
				Advertised: []provider.ToolDefinition{
					{Name: "bad", Schema: []byte("not-valid-json")},
				},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = loop.Run(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "hi"}})
	if err == nil || !errors.Is(err, sdkagentloop.ErrInvalidSchema) {
		t.Fatalf("err = %v, want ErrInvalidSchema", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 || indexContains(haystack, needle))
}

func indexContains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
