package tools_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// gateTool blocks until its gate closes or its context ends. started,
// when set, receives one non-blocking signal at Run entry, so tests
// synchronize on progress instead of guessing with sleep alone.
type gateTool struct {
	name    string
	gate    chan struct{}
	started chan struct{}
}

func (g *gateTool) Name() string { return g.name }

func (g *gateTool) Run(ctx context.Context, _ tools.InOut) (tools.Out, error) {
	if g.started != nil {
		select {
		case g.started <- struct{}{}:
		default:
		}
	}
	select {
	case <-g.gate:
		return tools.Out{Value: "opened"}, nil
	case <-ctx.Done():
		return tools.Out{}, ctx.Err()
	}
}

// profiledGateTool is a gateTool whose published profile carries one
// Timeout declaration.
type profiledGateTool struct {
	gateTool
	timeout time.Duration
}

func (p *profiledGateTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Timeout: p.timeout}
}

// napTool returns val after d, or the context error when canceled
// first. It models a bounded slow tool without leaking timers.
type napTool struct {
	name string
	d    time.Duration
	val  any
}

func (n *napTool) Name() string { return n.name }

func (n *napTool) Run(ctx context.Context, _ tools.InOut) (tools.Out, error) {
	timer := time.NewTimer(n.d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return tools.Out{Value: n.val}, nil
	case <-ctx.Done():
		return tools.Out{}, ctx.Err()
	}
}

// profiledNapTool publishes one Class and one Timeout alongside
// napTool behavior.
type profiledNapTool struct {
	napTool
	class   tools.ExecutionClass
	timeout time.Duration
}

func (p *profiledNapTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: p.class, Timeout: p.timeout}
}

// selfDeadlineTool fails immediately with its own
// context.DeadlineExceeded, well inside any registry bound.
type selfDeadlineTool struct{}

func (selfDeadlineTool) Name() string { return "self-deadline" }

func (selfDeadlineTool) Run(context.Context, tools.InOut) (tools.Out, error) {
	return tools.Out{}, context.DeadlineExceeded
}

// panicTool always panics, modeling a hostile tool.
type panicTool struct{}

func (panicTool) Name() string { return "panicker" }

func (panicTool) Run(context.Context, tools.InOut) (tools.Out, error) {
	panic("exploded")
}

// TestToolPanicBecomesError pins the bounded dispatch's fail-closed
// rule: a panicking tool yields an error carrying its name and panic
// value, never a crashed process and never ErrRunTimeout.
func TestToolPanicBecomesError(t *testing.T) {
	r := tools.New()
	if err := r.Add(panicTool{}); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	_, err := r.Run(context.Background(), "panicker", tools.InOut{})
	if err == nil {
		t.Fatal("Run error = nil, want the panicking tool's wrapped error")
	}
	if !strings.Contains(err.Error(), `"panicker"`) || !strings.Contains(err.Error(), "exploded") {
		t.Fatalf("error %q lacks the tool name or the panic value", err)
	}
	if errors.Is(err, tools.ErrRunTimeout) {
		t.Fatalf("error %v wraps ErrRunTimeout, want the panic conversion", err)
	}
}

// errPanicked is the sentinel a panicking tool throws.
var errPanicked = errors.New("seam exploded")

// errPanicTool panics with an error value, modeling a seam whose
// fault carries identity.
type errPanicTool struct{}

func (errPanicTool) Name() string { return "err-panicker" }

func (errPanicTool) Run(context.Context, tools.InOut) (tools.Out, error) {
	panic(errPanicked)
}

// TestToolErrorPanicKeepsIdentity pins the unwrap rule: a tool that
// panics with an error keeps that error's chain. Callers match the
// seam through errors.Is, never through message text.
func TestToolErrorPanicKeepsIdentity(t *testing.T) {
	r := tools.New()
	if err := r.Add(errPanicTool{}); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	_, err := r.Run(context.Background(), "err-panicker", tools.InOut{})
	if !errors.Is(err, errPanicked) {
		t.Fatalf("error %v does not wrap the thrown sentinel", err)
	}
	if !strings.Contains(err.Error(), `"err-panicker"`) {
		t.Fatalf("error %q lacks the tool name", err)
	}
}

// TestRunDeclaredTimeoutExpires pins invariant 8: a declared positive
// Timeout expires as ErrRunTimeout wrapped with the tool name and the
// effective bound.
func TestRunDeclaredTimeoutExpires(t *testing.T) {
	r := tools.New()
	tl := &profiledGateTool{
		gateTool: gateTool{name: "declared-slow", gate: make(chan struct{})},
		timeout:  40 * time.Millisecond,
	}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	defer close(tl.gate)

	_, err := r.Run(context.Background(), "declared-slow", tools.InOut{})
	if !errors.Is(err, tools.ErrRunTimeout) {
		t.Fatalf("Run error = %v, want errors.Is ErrRunTimeout", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"declared-slow"`) || !strings.Contains(msg, "40ms") {
		t.Fatalf("error message %q lacks the tool name or the 40ms bound", msg)
	}
}

// TestRunConfiguredDefaultExpires pins the configured-default path
// end to end: no declaration, WithDefaultRunTimeout bounds the run,
// and the expiry identity matches invariant 8.
func TestRunConfiguredDefaultExpires(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(20 * time.Millisecond))
	tl := &gateTool{name: "configured-slow", gate: make(chan struct{})}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	defer close(tl.gate)

	_, err := r.Run(context.Background(), "configured-slow", tools.InOut{})
	if !errors.Is(err, tools.ErrRunTimeout) {
		t.Fatalf("Run error = %v, want errors.Is ErrRunTimeout", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"configured-slow"`) || !strings.Contains(msg, "20ms") {
		t.Fatalf("error message %q lacks the tool name or the 20ms bound", msg)
	}
}

// TestFastToolRepeatedSuccessUnderDefault pins invariant 7 through
// repetitions against a plain New() registry: the built-in default
// never hinders a fast tool, and results stay byte-identical.
func TestFastToolRepeatedSuccessUnderDefault(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "fast", result: "same"}); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	want := tools.Out{Value: "same"}
	for i := 0; i < 5; i++ {
		out, err := r.Run(context.Background(), "fast", tools.InOut{})
		if err != nil {
			t.Fatalf("iteration %d: Run error = %v, want nil", i, err)
		}
		if out != want {
			t.Fatalf("iteration %d: Run Value = %+v, want %+v", i, out, want)
		}
	}
}

// TestProfileLongerThanConfiguredFiresLonger proves precedence rule
// 2: a declared 120 ms bound governs a 30 ms configured registry, so
// a 60 ms tool succeeds; a silent min() would expire it instead.
func TestProfileLongerThanConfiguredFiresLonger(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(30 * time.Millisecond))
	tl := &profiledNapTool{
		napTool: napTool{name: "long-declared", d: 60 * time.Millisecond, val: "done"},
		timeout: 120 * time.Millisecond,
	}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	out, err := r.Run(context.Background(), "long-declared", tools.InOut{})
	if err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	if out.Value != "done" {
		t.Fatalf("Run Value = %v, want done", out.Value)
	}
}

// TestNegativeProfileExemptsUnderAggressiveConfigured pins invariant
// 3: a negative profile Timeout exempts the tool under a tight
// registry default.
func TestNegativeProfileExemptsUnderAggressiveConfigured(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(20 * time.Millisecond))
	tl := &profiledNapTool{
		napTool: napTool{name: "exempt-declared", d: 100 * time.Millisecond, val: "finished"},
		timeout: tools.TimeoutNone,
	}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	out, err := r.Run(context.Background(), "exempt-declared", tools.InOut{})
	if err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	if out.Value != "finished" {
		t.Fatalf("Run Value = %v, want finished", out.Value)
	}
}

// TestWithDefaultRunTimeoutNoneExemptsUndeclared pins invariant 4:
// WithDefaultRunTimeout(TimeoutNone) restores unbounded runs for
// undeclared tools.
func TestWithDefaultRunTimeoutNoneExemptsUndeclared(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(tools.TimeoutNone))
	tl := &napTool{name: "exempt-configured", d: 150 * time.Millisecond, val: "late-but-fine"}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	out, err := r.Run(context.Background(), "exempt-configured", tools.InOut{})
	if err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	if out.Value != "late-but-fine" {
		t.Fatalf("Run Value = %v, want late-but-fine", out.Value)
	}
}

// TestParentCancelMidRunBeatsDeadline pins invariant 6: canceling the
// parent mid-run surfaces context.Canceled, never ErrRunTimeout.
func TestParentCancelMidRunBeatsDeadline(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(1 * time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tl := &gateTool{name: "cancelable", gate: make(chan struct{}), started: make(chan struct{}, 1)}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	defer close(tl.gate)

	errc := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, "cancelable", tools.InOut{})
		errc <- err
	}()
	<-tl.started // the run passed Get and entered t.Run
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want errors.Is context.Canceled", err)
		}
		if errors.Is(err, tools.ErrRunTimeout) {
			t.Fatalf("Run error %v wraps ErrRunTimeout, want the parent cause", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after parent cancel")
	}
}

// TestToolOwnDeadlineErrorStaysUntouched pins invariant 7 against the
// indistinguishable-identity case: a tool failing fast with its own
// context.DeadlineExceeded under a much longer bound keeps its exact
// error; the backstop reclassifies nothing.
func TestToolOwnDeadlineErrorStaysUntouched(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(200 * time.Millisecond))
	if err := r.Add(selfDeadlineTool{}); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	_, err := r.Run(context.Background(), "self-deadline", tools.InOut{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want the tool's context.DeadlineExceeded", err)
	}
	if errors.Is(err, tools.ErrRunTimeout) {
		t.Fatalf("Run error %v was reclassified as ErrRunTimeout", err)
	}
}

// TestSlowApproveDoesNotConsumeBudget pins invariant 5 through the
// true RunScoped approve branch: a 60 ms Approve outruns a 20 ms
// bound untouched, because the budget starts when t.Run starts.
func TestSlowApproveDoesNotConsumeBudget(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(20 * time.Millisecond))
	tl := &profiledNapTool{
		napTool: napTool{name: "approved-fast", val: "post-approve"},
		class:   tools.ExecutionClassRead,
	}
	if err := r.Add(tl); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}
	approved := false
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist: []string{"approved-fast"},
		Approve: func(_ context.Context, call tools.ToolCall) (bool, error) {
			timer := time.NewTimer(60 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			approved = true
			if call.Name != "approved-fast" {
				return false, fmt.Errorf("approve saw wrong name %q", call.Name)
			}
			return true, nil
		},
	})
	out, err := r.RunScoped(context.Background(), "approved-fast", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped error = %v, want nil after slow approve", err)
	}
	if !approved {
		t.Fatal("Approve never ran; the test proved nothing")
	}
	if out.Value != "post-approve" {
		t.Fatalf("RunScoped Value = %v, want post-approve", out.Value)
	}
}

// TestUnknownNameUnchangedUnderBackstop guards eager resolution: an
// unknown name still returns bare ErrUnknownName from Run and
// RunScoped, with no backstop engaged.
func TestUnknownNameUnchangedUnderBackstop(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(20 * time.Millisecond))
	_, err := r.Run(context.Background(), "ghost", tools.InOut{})
	if !errors.Is(err, tools.ErrUnknownName) || err.Error() != tools.ErrUnknownName.Error() {
		t.Fatalf("Run error = %v, want the naked ErrUnknownName", err)
	}
	_, err = r.RunScoped(context.Background(), "ghost", tools.InOut{}, nil)
	if !errors.Is(err, tools.ErrUnknownName) || err.Error() != tools.ErrUnknownName.Error() {
		t.Fatalf("RunScoped error = %v, want the naked ErrUnknownName", err)
	}
}

// TestConcurrentBlockingCallsEachExpire pins invariant 8 under load:
// several blocking calls through one shared registry each get their
// own ErrRunTimeout carrying their own tool name, with no cross-talk
// across the derived contexts.
func TestConcurrentBlockingCallsEachExpire(t *testing.T) {
	r := tools.New(tools.WithDefaultRunTimeout(25 * time.Millisecond))
	const n = 8
	names := make([]string, n)
	gates := make([]chan struct{}, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("blocker-%02d", i)
		names[i], gates[i] = name, make(chan struct{})
		if err := r.Add(&gateTool{name: name, gate: gates[i]}); err != nil {
			t.Fatalf("Add(%s) error = %v, want nil", name, err)
		}
	}
	for _, g := range gates {
		defer close(g) // release any abandoned producer before exit
	}

	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, errs[i] = r.Run(context.Background(), names[i], tools.InOut{})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent calls did not return near their common bound")
	}
	for i := 0; i < n; i++ {
		if !errors.Is(errs[i], tools.ErrRunTimeout) {
			t.Fatalf("call for %s returned %v, want its own ErrRunTimeout", names[i], errs[i])
		}
		if !strings.Contains(errs[i].Error(), fmt.Sprintf("%q", names[i])) {
			t.Fatalf("error for %s carried another call's name: %v", names[i], errs[i])
		}
	}
}
