// Shared command plumbing for the JSON-command tools.

package subagent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// ErrBadCommand reports a command a tool could not decode or one
// naming an unknown operation.
var ErrBadCommand = errors.New("subagent: bad command")

// decodeCommand parses payload into dst, mapping any parse fault to
// the shared bad-command sentinel.
func decodeCommand(payload string, dst any) error {
	if err := json.Unmarshal([]byte(payload), dst); err != nil {
		return ErrBadCommand
	}
	return nil
}

// stringValue reads a tool input's string payload; a non-string
// value reads as empty.
func stringValue(in tools.InOut) string {
	s, _ := in.Value.(string)
	return s
}

// badCommand names the tool inside the shared sentinel.
func badCommand(name string) error {
	return fmt.Errorf("%s: %w", name, ErrBadCommand)
}
