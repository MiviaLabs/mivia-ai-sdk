package hooks_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
)

// countingHandler returns a Handler that allows and counts its calls
// through calls.
func countingHandler(calls *int) hooks.Handler {
	return func(context.Context, any) (bool, error) {
		*calls++
		return true, nil
	}
}

// TestFireInvalidPointCallsNoHandler pins Fire's first rule: an
// invalid Point returns its Validate error before any handler call.
// Point(0) stands in for the unexported pointUnset.
func TestFireInvalidPointCallsNoHandler(t *testing.T) {
	r := hooks.New()
	calls := 0
	if err := r.Add(hooks.PointPreTool, "canary", countingHandler(&calls)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, p := range []hooks.Point{0, 99} {
		err := r.Fire(context.Background(), p, nil)
		if err == nil {
			t.Fatalf("Fire(Point(%d)) = nil, want the Validate error", int(p))
		}
	}
	if calls != 0 {
		t.Fatalf("canary ran %d times, want 0", calls)
	}
}

// TestFireNoHandlersReturnsNil pins the no-op: a point with no
// registered handlers returns nil at once.
func TestFireNoHandlersReturnsNil(t *testing.T) {
	r := hooks.New()
	if err := r.Fire(context.Background(), hooks.PointStop, nil); err != nil {
		t.Fatalf("Fire(empty point) = %v, want nil", err)
	}
}

// TestFireOneAllowingHandlerReturnsNil pins the simplest success.
func TestFireOneAllowingHandlerReturnsNil(t *testing.T) {
	r := hooks.New()
	calls := 0
	if err := r.Add(hooks.PointPostTool, "only", countingHandler(&calls)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Fire(context.Background(), hooks.PointPostTool, nil); err != nil {
		t.Fatalf("Fire = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1", calls)
	}
}

// TestFireMiddleVetoStopsChain pins the veto: the middle handler of
// three returns false, Fire returns the ErrVetoed wrap, and the third
// handler never runs.
func TestFireMiddleVetoStopsChain(t *testing.T) {
	r := hooks.New()
	first, third := 0, 0
	if err := r.Add(hooks.PointPreTool, "first", countingHandler(&first)); err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	veto := func(context.Context, any) (bool, error) { return false, nil }
	if err := r.Add(hooks.PointPreTool, "veto", veto); err != nil {
		t.Fatalf("Add(veto): %v", err)
	}
	if err := r.Add(hooks.PointPreTool, "third", countingHandler(&third)); err != nil {
		t.Fatalf("Add(third): %v", err)
	}

	err := r.Fire(context.Background(), hooks.PointPreTool, nil)
	if !errors.Is(err, hooks.ErrVetoed) {
		t.Fatalf("Fire = %v, want ErrVetoed", err)
	}
	if first != 1 {
		t.Fatalf("first ran %d times, want 1", first)
	}
	if third != 0 {
		t.Fatalf("third ran %d times, want 0", third)
	}
}

// TestFireRunsHandlersInRegistrationOrder pins the order: every
// handler appends its name, and the slice shows the registration
// sequence exactly.
func TestFireRunsHandlersInRegistrationOrder(t *testing.T) {
	r := hooks.New()
	var order []string
	appendHandler := func(name string) hooks.Handler {
		return func(context.Context, any) (bool, error) {
			order = append(order, name)
			return true, nil
		}
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := r.Add(hooks.PointStop, name, appendHandler(name)); err != nil {
			t.Fatalf("Add(%s): %v", name, err)
		}
	}
	if err := r.Fire(context.Background(), hooks.PointStop, nil); err != nil {
		t.Fatalf("Fire = %v, want nil", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestFirePassesPayloadToEveryHandler pins payload delivery: every
// handler run receives the exact payload value passed to Fire.
func TestFirePassesPayloadToEveryHandler(t *testing.T) {
	r := hooks.New()
	payload := struct{ Tool string }{Tool: "grep"}
	var seen []struct{ Tool string }
	receiver := func(_ context.Context, p any) (bool, error) {
		got, ok := p.(struct{ Tool string })
		if !ok {
			t.Fatalf("payload has type %T, want struct{ Tool string }", p)
		}
		seen = append(seen, got)
		return true, nil
	}
	for _, name := range []string{"one", "two"} {
		if err := r.Add(hooks.PointPreTool, name, receiver); err != nil {
			t.Fatalf("Add(%s): %v", name, err)
		}
	}
	if err := r.Fire(context.Background(), hooks.PointPreTool, payload); err != nil {
		t.Fatalf("Fire = %v, want nil", err)
	}
	if len(seen) != 2 {
		t.Fatalf("payload seen %d times, want 2", len(seen))
	}
	for i, got := range seen {
		if got != payload {
			t.Fatalf("handler %d saw %+v, want %+v", i, got, payload)
		}
	}
}

// TestFireHandlerErrorStopsChain pins the handler-failure path: the
// error stops the chain, wraps as the plan states, and stays distinct
// from ErrVetoed under errors.Is. The failing handler returns
// (false, err) on purpose: a non-nil error wins over the veto, so a
// handler failure never reads as an explicit veto.
func TestFireHandlerErrorStopsChain(t *testing.T) {
	r := hooks.New()
	third := 0
	boom := errors.New("db down")
	failing := func(context.Context, any) (bool, error) { return false, boom }
	if err := r.Add(hooks.PointPostTool, "first", allowHandler()); err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	if err := r.Add(hooks.PointPostTool, "failing", failing); err != nil {
		t.Fatalf("Add(failing): %v", err)
	}
	if err := r.Add(hooks.PointPostTool, "third", countingHandler(&third)); err != nil {
		t.Fatalf("Add(third): %v", err)
	}

	err := r.Fire(context.Background(), hooks.PointPostTool, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Fire = %v, want boom wrapped", err)
	}
	if errors.Is(err, hooks.ErrVetoed) {
		t.Fatalf("Fire = %v, a handler failure must not read as ErrVetoed", err)
	}
	wantPrefix := `hooks: post-tool: handler "failing": `
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Fatalf("Fire = %q, want prefix %q", err.Error(), wantPrefix)
	}
	if third != 0 {
		t.Fatalf("third ran %d times, want 0", third)
	}
}

// TestFirePointIsolation pins grouping: a handler registered at
// PointPreTool never runs on a Fire call for PointPostTool.
func TestFirePointIsolation(t *testing.T) {
	r := hooks.New()
	calls := 0
	if err := r.Add(hooks.PointPreTool, "pre", countingHandler(&calls)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Fire(context.Background(), hooks.PointPostTool, nil); err != nil {
		t.Fatalf("Fire(post-tool) = %v, want nil", err)
	}
	if calls != 0 {
		t.Fatalf("pre-tool handler ran %d times on a post-tool Fire, want 0", calls)
	}
}
