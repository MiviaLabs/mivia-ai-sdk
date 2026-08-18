// HeartbeatTool reports liveness from a tool call.

package subagent

import (
	"context"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// HeartbeatCommand is the JSON wire form of one heartbeat tool call.
type HeartbeatCommand struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

// Heartbeat operation constants.
const (
	// OpBeat records one beat for ID now.
	OpBeat = "beat"
	// OpAlive reports whether ID is inside the timeout.
	OpAlive = "alive"
	// OpDead lists every silent id, comma-joined.
	OpDead = "dead"
)

// HeartbeatTool returns a tool bound to one monitor.
func HeartbeatTool(name string, m *heartbeat.Monitor) tools.Tool {
	return &heartbeatTool{name: name, monitor: m}
}

// heartbeatTool adapts one monitor to the tools.Tool interface.
type heartbeatTool struct {
	name    string
	monitor *heartbeat.Monitor
}

// Name returns the registry name.
func (t *heartbeatTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *heartbeatTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd HeartbeatCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, badCommand(t.name)
	}
	now := time.Now()
	switch cmd.Op {
	case OpBeat:
		if err := t.monitor.Beat(cmd.ID, now); err != nil {
			return tools.Out{}, err
		}
		return tools.Out{Value: "beat"}, nil
	case OpAlive:
		if t.monitor.Alive(cmd.ID, now) {
			return tools.Out{Value: "true"}, nil
		}
		return tools.Out{Value: "false"}, nil
	case OpDead:
		return tools.Out{Value: strings.Join(t.monitor.Dead(now), ",")}, nil
	default:
		return tools.Out{}, badCommand(t.name)
	}
}
