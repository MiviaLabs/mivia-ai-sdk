package machine

import "errors"

// ErrNoTransition reports that no transition row matches the current
// status and trigger. Fire wraps it with the status and the trigger.
var ErrNoTransition = errors.New("machine: no transition")

// ErrGuardRejected reports that a matched row's guard returned false.
// Fire wraps it with the status and the trigger.
var ErrGuardRejected = errors.New("machine: guard rejected move")
