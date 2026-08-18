package a2aclient

// State is the state of a remote task, mirrored from the a2a-go task
// state enum. See the State constants for the closed set of values.
type State int

// The states a remote task passes through. A2A tasks move from
// submitted through working to one terminal state: completed,
// failed, or canceled.
const (
	StateUnspecified State = iota
	StateSubmitted
	StateWorking
	StateCompleted
	StateFailed
	StateCanceled
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
	default:
		return "unknown"
	}
}

// terminal reports whether a task in state s accepts no further
// transitions: Result only fetches output for a terminal state.
func (s State) terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCanceled:
		return true
	default:
		return false
	}
}
