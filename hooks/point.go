package hooks

import "fmt"

// Point names a lifecycle point a Registry groups handlers under.
type Point int

const (
	// pointUnset is the zero value. A Point a caller never set stays
	// invalid, the same way a caller must name a real machine.Status.
	pointUnset Point = iota
	// PointPreTool fires before a tool call runs.
	PointPreTool
	// PointPostTool fires after a tool call runs, success or failure.
	PointPostTool
	// PointStop fires at a run's stop.
	PointStop
)

// Validate rejects pointUnset and any value outside the three named
// constants.
func (p Point) Validate() error {
	switch p {
	case PointPreTool, PointPostTool, PointStop:
		return nil
	default:
		return fmt.Errorf("hooks: invalid point %d", int(p))
	}
}

// String returns a short label for each named constant, used in
// Fire's wrapped error messages. It returns "unknown" for an invalid
// value; it never panics.
func (p Point) String() string {
	switch p {
	case PointPreTool:
		return "pre-tool"
	case PointPostTool:
		return "post-tool"
	case PointStop:
		return "stop"
	default:
		return "unknown"
	}
}
