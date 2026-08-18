package flow

import "encoding/json"

// Encode serializes the checkpoint to JSON. It validates first. No
// registry: Record.Input and Record.Output are caller-owned any
// values. encoding/json decodes an any field back to
// map[string]interface{}, never the original concrete type. A caller
// whose Input or Output must survive a Checkpoint round-trip is
// responsible for using JSON-primitive-compatible types, or for
// re-hydrating its own concrete type after Decode.
func (c Checkpoint) Encode() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Decode parses JSON and validates the result. Mirrors machine.Decode's
// shape without its registry, since Checkpoint binds no guard or
// action.
func Decode(data []byte) (Checkpoint, error) {
	var c Checkpoint
	if err := json.Unmarshal(data, &c); err != nil {
		return Checkpoint{}, errorf("checkpoint decode: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Checkpoint{}, err
	}
	return c, nil
}
