package machine

// InOut carries the record a transition moves.
// Input is the caller payload. Output is the record the move writes.
type InOut struct {
	Input  any
	Output any
}
