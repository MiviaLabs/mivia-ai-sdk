package contextstate

// Limits is the caller-owned set of volume bounds for one store. A
// zero field means uncapped, matching contextbudget.Limits. The
// MemStore enforces these at write time; Validate stays shape-only.
type Limits struct {
	CheckpointBytes  int
	CommitEvents     int
	CommitEventBytes int
}

// Validate rejects a negative field and names it. CheckpointBytes is
// checked first, then CommitEvents, then CommitEventBytes.
func (l Limits) Validate() error {
	if l.CheckpointBytes < 0 {
		return invalid("limits.checkpoint_bytes", "must not be negative")
	}
	if l.CommitEvents < 0 {
		return invalid("limits.commit_events", "must not be negative")
	}
	if l.CommitEventBytes < 0 {
		return invalid("limits.commit_event_bytes", "must not be negative")
	}
	return nil
}

// exceeds reports whether size breaks an enabled bound. A zero or
// negative bound is uncapped, so a zero never reads as "allow
// nothing".
func exceeds(size, bound int) bool { return bound > 0 && size > bound }
