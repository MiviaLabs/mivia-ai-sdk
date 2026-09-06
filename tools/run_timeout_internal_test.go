package tools

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// profileOnlyTool publishes exactly one Timeout through ProfiledTool.
type profileOnlyTool struct {
	timeout time.Duration
}

func (t profileOnlyTool) Name() string { return "profile-only" }

func (t profileOnlyTool) Run(_ context.Context, _ InOut) (Out, error) {
	return Out{}, nil
}

func (t profileOnlyTool) ExecutionProfile() ExecutionProfile {
	return ExecutionProfile{Timeout: t.timeout}
}

// TestEffectiveRunTimeout table-drives the resolution precedence from
// the tool alone. A positive declared Timeout binds verbatim, longer
// or shorter than DefaultRunTimeout. A negative one never caps. An
// undeclared or zero Timeout falls through to DefaultRunTimeout.
func TestEffectiveRunTimeout(t *testing.T) {
	tests := []struct {
		name       string
		declared   time.Duration
		hasProfile bool
		want       time.Duration
	}{
		{"undeclared-defaults", 0, false, DefaultRunTimeout},
		{"declared-zero-falls-through", 0, true, DefaultRunTimeout},
		{"declared-positive-verbatim", 80 * time.Millisecond, true, 80 * time.Millisecond},
		{"declared-longer-than-default", 20 * time.Minute, true, 20 * time.Minute},
		{"declared-negative-none", TimeoutNone, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tool Tool = bareRunTool{}
			if tt.hasProfile {
				tool = profileOnlyTool{timeout: tt.declared}
			}
			if got := effectiveRunTimeout(tool); got != tt.want {
				t.Fatalf("effectiveRunTimeout(declared=%v) = %v, want %v",
					tt.declared, got, tt.want)
			}
		})
	}
}

// bareRunTool implements only Tool; it stands in for any undeclared
// tool above.
type bareRunTool struct{}

func (bareRunTool) Name() string { return "bare" }

func (bareRunTool) Run(context.Context, InOut) (Out, error) { return Out{}, nil }

// lateProducerTool blocks on release until released and ignores its
// context on purpose, modeling the failure class this change closes:
// a tool that never selects on ctx.Done. Its produced channel is
// built with make(chan Out, 1), the same one-buffered construction
// runBounded's internal handoff uses, so receiving from it after
// expiry demonstrates that a producer holding no live receiver
// completes its buffered send without block.
type lateProducerTool struct {
	release  chan struct{}
	payload  string
	produced chan Out
}

func (t *lateProducerTool) Name() string { return "late-producer" }

func (t *lateProducerTool) ExecutionProfile() ExecutionProfile {
	return ExecutionProfile{Timeout: 15 * time.Millisecond}
}

func (t *lateProducerTool) Run(context.Context, InOut) (Out, error) {
	<-t.release
	out := Out{Value: t.payload}
	select {
	case t.produced <- out:
	default:
	}
	return out, nil // the return feeds runBounded's own one-buffered send
}

// TestRunBoundedLateProducerBufferedSend proves the channel-safety
// contract directly against runBounded: a tight bound expires while
// the producer blocks; releasing it afterward lets both its own
// one-buffered send and the abandoned internal handoff complete with
// no panic and no send-block, and no value leaks into the caller.
// Completion is proved by absence, not by fixture channels alone:
// after release, no goroutine may remain parked inside runBounded,
// so an unbuffered handoff regression strands exactly this test.
func TestRunBoundedLateProducerBufferedSend(t *testing.T) {
	r := New()
	tl := &lateProducerTool{
		release:  make(chan struct{}),
		payload:  "late-value",
		produced: make(chan Out, 1),
	}

	errc := make(chan error, 1)
	var gotOut Out
	go func() {
		out, err := r.runBounded(context.Background(), tl.Name(), tl, InOut{})
		gotOut = out
		errc <- err
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, ErrRunTimeout) {
			t.Fatalf("runBounded error = %v, want ErrRunTimeout before release", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runBounded did not return at the deadline")
	}
	if gotOut.Value != nil {
		t.Fatalf("expired call returned Value %v, want nil", gotOut)
	}

	close(tl.release)
	select {
	case out := <-tl.produced:
		if out.Value != "late-value" {
			t.Fatalf("late producer sent %v, want late-value", out.Value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late producer never completed its one-buffered send")
	}

	// The abandoned producer must exit once it completes its buffered
	// send. Absence of any runBounded frame across all stacks is the
	// direct observation. One probe reads it immediately; if the
	// producer was still mid-handoff, the exact two-second deadline
	// passes before the confirming probe.
	waitCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	buf := make([]byte, 1<<16)
	settled := func() bool {
		n := runtime.Stack(buf, true)
		return !strings.Contains(string(buf[:n]), "runBounded")
	}
	if !settled() {
		<-waitCtx.Done()
	}
	if !settled() {
		t.Fatal("abandoned producer still parked inside runBounded")
	}
}
