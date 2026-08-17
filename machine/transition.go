package machine

import "context"

// Guard decides whether a transition may fire.
// A nil Guard is allowed; it means no check.
type Guard func(ctx context.Context) (bool, error)

// Action runs an entry or exit side effect on a move.
// rec is the record the move carries. The action may write rec.Output.
// A nil Action is allowed; it means no side effect.
type Action func(ctx context.Context, rec *InOut) error

// Transition is one row in the transition table.
// From is the source status. To is the target status.
// Trigger selects this row. Guard is the optional check.
// OnExit and OnEntry are the optional move actions.
type Transition struct {
	From    Status
	To      Status
	Trigger Trigger
	Guard   Guard
	OnExit  Action
	OnEntry Action
}
