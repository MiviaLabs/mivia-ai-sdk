// AsTool and the depth guard.

package subagent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// ErrMaxDepth reports a spawn past the tool's depth bound.
var ErrMaxDepth = errors.New("subagent: max spawn depth reached")

// defaultMaxDepth bounds recursive spawns when ToolOptions.Depth is
// zero.
const defaultMaxDepth = 3

// ToolOptions tunes one subagent tool. Artifact names the artifact
// the call returns; it needs the runner's own Artifacts bag. Depth
// bounds recursive spawns; zero means the default three. Bus, when
// set, receives the spawned run's agent events.
type ToolOptions struct {
	Artifact  string
	Artifacts *agentrun.Artifacts
	Depth     int
	Bus       *events.Bus
}

// threadSeq numbers spawned threads uniquely per process.
var threadSeq uint64

// AsTool wraps one built runner as a tools.Tool. Each Run drives one
// full runner execution on a fresh thread; the ctx carries the spawn
// depth, so a subagent spawning subagents stops at the bound. The
// input string seeds the run's starting record; the result is the
// named artifact, or the final status when no artifact is named.
func AsTool(name string, r *agentrun.Runner, opts ToolOptions) tools.Tool {
	return &subTool{name: name, runner: r, opts: opts}
}

// subTool adapts one runner to the tools.Tool interface.
type subTool struct {
	name   string
	runner *agentrun.Runner
	opts   ToolOptions
	once   sync.Once
}

// Name returns the registry name.
func (t *subTool) Name() string { return t.name }

// Run spawns the wrapped runner once, guarding the depth bound.
func (t *subTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	max := t.opts.Depth
	if max <= 0 {
		max = defaultMaxDepth
	}
	depth := depthFrom(ctx)
	if depth >= max {
		return tools.Out{}, fmt.Errorf("subagent: %s: %w", t.name, ErrMaxDepth)
	}
	t.once.Do(t.attachForwarder)
	payload, _ := in.Value.(string)
	thread := fmt.Sprintf("%s-%d", t.name, atomic.AddUint64(&threadSeq, 1))
	status, _, err := t.runner.Run(withDepth(ctx, depth+1), thread, machine.InOut{Input: payload})
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: t.result(status)}, nil
}

// result picks the returned string: the named artifact when the bag
// holds one, else the final status.
func (t *subTool) result(status machine.Status) string {
	if t.opts.Artifact != "" && t.opts.Artifacts != nil {
		if v, ok := t.opts.Artifacts.Get(t.opts.Artifact); ok {
			return v
		}
	}
	return string(status)
}

// attachForwarder copies the spawned run's agent events onto the
// caller's bus. The runner's bus keeps its own no-op subscribers;
// the forwarder adds one handler per event name, never removed.
func (t *subTool) attachForwarder() {
	if t.opts.Bus == nil {
		return
	}
	dst := t.opts.Bus
	src := t.runner.Bus()
	forward := func(ctx context.Context, e events.Event) error {
		_ = dst.Emit(ctx, e)
		return nil
	}
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		_ = src.Subscribe(name, forward)
	}
}

// depthCtxKey is the unexported key carrying the spawn depth.
type depthCtxKey struct{}

// withDepth stores n as ctx's spawn depth.
func withDepth(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, depthCtxKey{}, n)
}

// depthFrom reads ctx's spawn depth; zero outside a spawned run.
func depthFrom(ctx context.Context) int {
	n, _ := ctx.Value(depthCtxKey{}).(int)
	return n
}
