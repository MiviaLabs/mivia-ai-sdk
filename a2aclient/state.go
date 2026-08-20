package a2aclient

// State is the state of a remote task, mirrored from the a2a-go task
// state enum. Every a2a-go TaskState has one State. See the State
// constants for the closed set of values.
type State int

// The states a remote task passes through. A2A tasks move from
// submitted through working to one terminal state: completed, failed,
// canceled, or rejected. Auth-required and input-required both wait
// for client action. Unspecified and unknown both mean the state is
// not yet determined. StateUnknown's String is "unknown", the same
// text a State outside the declared range returns, so that text names
// either the upstream indeterminate state or an out-of-range value.
const (
	StateUnspecified State = iota
	StateSubmitted
	StateWorking
	StateCompleted
	StateFailed
	StateCanceled
	StateRejected
	StateAuthRequired
	StateInputRequired
	StateUnknown
)

// String returns the constant name for state, or "unknown" for a
// value outside the declared range.
func (s State) String() string {
	switch s {
	case StateUnspecified:
		return "unspecified"
	case StateSubmitted:
		return "submitted"
	case StateWorking:
		return "working"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	case StateRejected:
		return "rejected"
	case StateAuthRequired:
		return "auth-required"
	case StateInputRequired:
		return "input-required"
	default:
		return "unknown"
	}
}

// terminal reports whether a task in state s accepts no further
// transitions: Result only fetches output for a terminal state. The
// set equals a2a-go's own TaskState.Terminal.
func (s State) terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCanceled, StateRejected:
		return true
	default:
		return false
	}
}
