package flow

// Panel is a group of step IDs that run together in parallel.
// The runner of a later step schedules a panel as one wave.
type Panel []string
