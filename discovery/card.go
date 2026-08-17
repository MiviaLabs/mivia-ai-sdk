package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Card holds a parsed capability card: an agent's name, an optional
// description, and its capability list. Capabilities is an exported
// slice; Parse does not defensively copy it. This matches
// envelope.Message's exported slice fields, which carry the same
// caller-owned mutability with no defensive copy.
type Card struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities"`
}

// Parse unmarshals data into a Card, then calls Validate. A JSON
// decode error, syntax or type mismatch, wraps the decode error with
// context. An invariant failure returns the Validate error unchanged.
// Parse ignores an unknown JSON field, matching envelope.Decode's
// forward-compatibility rule.
func Parse(data []byte) (Card, error) {
	var c Card
	if err := json.Unmarshal(data, &c); err != nil {
		return Card{}, fmt.Errorf("discovery: decode card: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Card{}, err
	}
	return c, nil
}

// Validate checks the card invariants. It rejects a blank Name after
// TrimSpace. It rejects an empty Capabilities list. It applies
// TrimSpace to each capability entry before the next two checks. It
// rejects a capability entry that is blank after trim, including a
// whitespace-only entry. It rejects a duplicate entry, compared with
// strings.EqualFold after trim: the same fold Match uses, so a
// Validate pass guarantees Match never hides a second, equal entry.
func (c Card) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("discovery: name is required")
	}
	if len(c.Capabilities) == 0 {
		return errors.New("discovery: capabilities must not be empty")
	}
	seen := make([]string, 0, len(c.Capabilities))
	for _, capability := range c.Capabilities {
		trimmed := strings.TrimSpace(capability)
		if trimmed == "" {
			return errors.New("discovery: capability entry must not be blank")
		}
		for _, prior := range seen {
			if strings.EqualFold(trimmed, prior) {
				return fmt.Errorf("discovery: duplicate capability %q", trimmed)
			}
		}
		seen = append(seen, trimmed)
	}
	return nil
}

// Match compares need against each capability with strings.EqualFold.
// It returns the matched capability and true on a hit. It returns an
// empty string and false when need is blank or no entry matches.
// Match does not trim need: a padded need, such as a leading space,
// does not match an entry with no padding. Match never calls
// Validate: on a Card with a duplicate-case capability entry, it
// returns the first slice-order match.
func (c Card) Match(need string) (string, bool) {
	if need == "" {
		return "", false
	}
	for _, capability := range c.Capabilities {
		if strings.EqualFold(need, capability) {
			return capability, true
		}
	}
	return "", false
}
