package envelope

import (
	"errors"
	"fmt"
)

// VerifyThread checks an ordered thread: every message is valid, all
// share one ThreadID, and each PrevHash links to the Hash of the
// previous message. The first message must not claim a parent. A gap,
// reorder, insertion, or fork fails the check.
func VerifyThread(msgs []Message) error {
	if len(msgs) == 0 {
		return errors.New("thread is empty")
	}
	thread := msgs[0].ThreadID
	for i, m := range msgs {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("message %d (%s): %w", i, m.ID, err)
		}
		if m.ThreadID != thread {
			return fmt.Errorf("message %d (%s): thread %q, want %q", i, m.ID, m.ThreadID, thread)
		}
		if i == 0 {
			if m.PrevHash != "" {
				return fmt.Errorf("first message %s must not have prev_hash", m.ID)
			}
			continue
		}
		if want := msgs[i-1].Hash(); m.PrevHash != want {
			return fmt.Errorf("message %d (%s): prev_hash does not match its parent", i, m.ID)
		}
	}
	return nil
}
