package machine

import (
	"encoding/json"
	"fmt"
)

// Registry holds the named guards and actions that Decode rebinds.
// Guard names and action names are separate namespaces.
type Registry struct {
	Actions map[string]Action
	Guards  map[string]Guard
}

// NewRegistry builds an empty Registry ready for Decode.
func NewRegistry() Registry {
	return Registry{
		Actions: make(map[string]Action),
		Guards:  make(map[string]Guard),
	}
}

// transName records the wire name of each bound guard and action.
// A function cannot be reverse-mapped to a name, so Decode stores the
// name it read and Encode reads it back from the same index.
type transName struct {
	guard   string
	onExit  string
	onEntry string
}

// wireDefinition is the JSON form of a Definition.
// It is unexported; guards and actions serialize as names.
type wireDefinition struct {
	Initial     string           `json:"initial"`
	Transitions []wireTransition `json:"transitions"`
}

// wireTransition is one JSON row of the transition table.
// Guard, OnExit, and OnEntry are pointers; nil means absent.
type wireTransition struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	Trigger string  `json:"trigger"`
	Guard   *string `json:"guard,omitempty"`
	OnExit  *string `json:"on_exit,omitempty"`
	OnEntry *string `json:"on_entry,omitempty"`
}

// Encode serializes the definition to JSON. It validates first. Each
// bound guard and action must carry a wire name recorded by Decode; an
// anonymous function that was never decoded has no name and returns an
// error. Every emitted name must resolve in reg, so the same registry
// can decode the bytes back.
func (d *Definition) Encode(reg Registry) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	w := wireDefinition{
		Initial:     string(d.initial),
		Transitions: make([]wireTransition, 0, len(d.transitions)),
	}
	for i, t := range d.transitions {
		guard, err := wireName(reg.Guards, entryOf(d.names, i).guard, t.Guard == nil, "guard")
		if err != nil {
			return nil, fmt.Errorf("machine: transition %d: %w", i, err)
		}
		onExit, err := wireName(reg.Actions, entryOf(d.names, i).onExit, t.OnExit == nil, "action")
		if err != nil {
			return nil, fmt.Errorf("machine: transition %d: %w", i, err)
		}
		onEntry, err := wireName(reg.Actions, entryOf(d.names, i).onEntry, t.OnEntry == nil, "action")
		if err != nil {
			return nil, fmt.Errorf("machine: transition %d: %w", i, err)
		}
		w.Transitions = append(w.Transitions, wireTransition{
			From:    string(t.From),
			To:      string(t.To),
			Trigger: string(t.Trigger),
			Guard:   guard,
			OnExit:  onExit,
			OnEntry: onEntry,
		})
	}
	return json.Marshal(w)
}

// entryOf returns the recorded names for transition i, or a zero entry.
func entryOf(entries []transName, i int) transName {
	if i < len(entries) {
		return entries[i]
	}
	return transName{}
}

// wireName returns a pointer to name, or nil when absent.
// A bound function must carry a name; an anonymous function cannot
// serialize. Every emitted name must still resolve in m, so the same
// registry can decode the bytes back.
func wireName[T any](m map[string]T, name string, absent bool, kind string) (*string, error) {
	if absent {
		return nil, nil
	}
	if name == "" {
		return nil, fmt.Errorf("%v is anonymous and has no wire name", kind)
	}
	if _, ok := m[name]; !ok {
		return nil, fmt.Errorf("%v %q is not registered", kind, name)
	}
	n := name
	return &n, nil
}

// Decode parses JSON and validates the result. Each name in the wire
// form rebinds through reg; a missing or empty name returns an error.
// Unknown fields are ignored. The read names are stored so Encode can
// reproduce them on the round trip.
func Decode(data []byte, reg Registry) (Definition, error) {
	var w wireDefinition
	if err := json.Unmarshal(data, &w); err != nil {
		return Definition{}, fmt.Errorf("machine decode: %w", err)
	}
	d := Definition{
		initial:     Status(w.Initial),
		transitions: make([]Transition, 0, len(w.Transitions)),
		names:       make([]transName, 0, len(w.Transitions)),
	}
	for _, t := range w.Transitions {
		guard, guardName, err := bindGuard(reg, t.Guard)
		if err != nil {
			return Definition{}, err
		}
		onExit, exitName, err := bindAction(reg, t.OnExit)
		if err != nil {
			return Definition{}, err
		}
		onEntry, entryName, err := bindAction(reg, t.OnEntry)
		if err != nil {
			return Definition{}, err
		}
		d.transitions = append(d.transitions, Transition{
			From:    Status(t.From),
			To:      Status(t.To),
			Trigger: Trigger(t.Trigger),
			Guard:   guard,
			OnExit:  onExit,
			OnEntry: onEntry,
		})
		d.names = append(d.names, transName{
			guard:   guardName,
			onExit:  exitName,
			onEntry: entryName,
		})
	}
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	return d, nil
}

// bindGuard resolves a wire name to a bound guard. A nil name means absent.
func bindGuard(reg Registry, name *string) (Guard, string, error) {
	if name == nil {
		return nil, "", nil
	}
	if *name == "" {
		return nil, "", fmt.Errorf("machine: guard name must not be empty")
	}
	g, ok := reg.Guards[*name]
	if !ok {
		return nil, "", fmt.Errorf("machine: guard %q is not registered", *name)
	}
	return g, *name, nil
}

// bindAction resolves a wire name to a bound action. A nil name means absent.
func bindAction(reg Registry, name *string) (Action, string, error) {
	if name == nil {
		return nil, "", nil
	}
	if *name == "" {
		return nil, "", fmt.Errorf("machine: action name must not be empty")
	}
	a, ok := reg.Actions[*name]
	if !ok {
		return nil, "", fmt.Errorf("machine: action %q is not registered", *name)
	}
	return a, *name, nil
}
