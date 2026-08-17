package machine

import "context"

// Guard decides whether a transition may fire.
// A nil Guard is allowed; it means no check.
type Guard func(ctx context.Context) (bool, error)

// Transition is one row in the transition table.
// From is the source status. To is the target status.
// Trigger selects this row. Guard is the optional check.
type Transition struct {
	From    Status
	To      Status
	Trigger Trigger
	Guard   Guard
}
