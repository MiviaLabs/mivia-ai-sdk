// LedgerTool records subagent tasks in the durable ledger.

package subagent

import (
	"context"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// LedgerCommand is the JSON wire form of one ledger tool call. Op
// selects the operation; the remaining fields feed it.
type LedgerCommand struct {
	Op          string                  `json:"op"`
	Key         string                  `json:"key"`
	Seq         uint64                  `json:"seq"`
	Description string                  `json:"description"`
	Needs       []ledger.IdempotencyKey `json:"needs"`
}

// Ledger operation constants.
const (
	// OpRun records one completed task: admit, claim, complete.
	OpRun = "run"
	// OpState reports a key's current status, or absent.
	OpState = "state"
)

// absentState reports a key with no ledger record.
const absentState = "absent"

// LedgerTool returns a tool bound to one ledger and actor. OpRun
// wraps the full taskrun ceremony around a no-op work function,
// landing the key completed; a blocked or replayed key fails the
// call with the ceremony's own sentinel. OpState reports the key's
// status, or absent when no record exists.
func LedgerTool(name string, l *ledger.Ledger, actor ledger.Actor, lease time.Duration) tools.Tool {
	return &ledgerTool{name: name, ledger: l, actor: actor, lease: lease}
}

// ledgerTool adapts one ledger to the tools.Tool interface.
type ledgerTool struct {
	name   string
	ledger *ledger.Ledger
	actor  ledger.Actor
	lease  time.Duration
}

// Name returns the registry name.
func (t *ledgerTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *ledgerTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd LedgerCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, badCommand(t.name)
	}
	switch cmd.Op {
	case OpRun:
		return t.run(ctx, cmd)
	case OpState:
		return t.state(ctx, cmd)
	default:
		return tools.Out{}, badCommand(t.name)
	}
}

// run records one completed task through the full ceremony.
func (t *ledgerTool) run(ctx context.Context, cmd LedgerCommand) (tools.Out, error) {
	opts := taskrun.Options{
		Ledger: t.ledger,
		Actor:  t.actor,
		Owner:  ledger.OwnerID(t.actor),
		Lease:  t.lease,
	}
	task := taskrun.Task{
		Key:         ledger.IdempotencyKey(cmd.Key),
		Seq:         ledger.Sequence(cmd.Seq),
		Description: cmd.Description,
		Needs:       cmd.Needs,
	}
	work := func(context.Context) error { return nil }
	if err := taskrun.Run(ctx, opts, task, work); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: string(ledger.StatusCompleted)}, nil
}

// state reports one key's status.
func (t *ledgerTool) state(ctx context.Context, cmd LedgerCommand) (tools.Out, error) {
	st, found, err := t.ledger.State(ctx, ledger.IdempotencyKey(cmd.Key))
	if err != nil {
		return tools.Out{}, err
	}
	if !found {
		return tools.Out{Value: absentState}, nil
	}
	return tools.Out{Value: string(st.Status)}, nil
}
