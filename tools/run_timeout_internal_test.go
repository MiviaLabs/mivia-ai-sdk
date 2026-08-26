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

// TestEffectiveRunTimeout table-drives the resolution precedence: a
// positive declared Timeout wins in both directions; a negative one
// beats every configuration; an undeclared tool falls through to the
// configured value; zero or negative configuration falls further to
// DefaultRunTimeout or none.
func TestEffectiveRunTimeout(t *testing.T) {
	tests := []struct {
		name       string
		declared   time.Duration
		hasProfile bool
		configured time.Duration
		want       time.Duration
	}{
		{"undeclared-unset-defaults", 0, false, 0, DefaultRunTimeout},
		{"positive-beats-smaller-configured", 80 * time.Millisecond, true, 20 * time.Millisecond, 80 * time.Millisecond},
		{"positive-beats-larger-configured", 30 * time.Millisecond, true, 90 * time.Millisecond, 30 * time.Millisecond},
		{"negative-profile-none", TimeoutNone, true, 20 * time.Millisecond, 0},
		{"negative-configured-none-undeclared", 0, false, TimeoutNone, 0},
		{"zero-configured-defaults", 0, false, 0, DefaultRunTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tool Tool = bareRunTool{}
			if tt.hasProfile {
				tool = profileOnlyTool{timeout: tt.declared}
			}
			if got := effectiveRunTimeout(tool, tt.configured); got != tt.want {
				t.Fatalf("effectiveRunTimeout(declared=%v, configured=%v) = %v, want %v",
					tt.declared, tt.configured, got, tt.want)
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
	r := New(WithDefaultRunTimeout(15 * time.Millisecond))
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
