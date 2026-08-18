package ledger

import (
	"encoding/json"
	"fmt"
)

// Encode validates the snapshot, then marshals it to JSON.
func (s Snapshot) Encode() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// Decode parses JSON into a Snapshot and validates the result. It
// rejects malformed JSON and an out-of-range Status on any entry.
func Decode(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("ledger: decode: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}
