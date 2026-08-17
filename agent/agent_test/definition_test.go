// Package agent_test holds the red-green unit cases for New, Name,
// and Capabilities. Each case asserted first against the empty
// package, then went green once agent.go implemented the behavior.
package agent_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// newIdentity builds a fresh Identity for a test case; a nil id
// stands in for the missing-identity case.
func newIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	return id
}

// validCard returns a Card that passes Validate.
func validCard() discovery.Card {
	return discovery.Card{Name: "Agent A", Capabilities: []string{"read", "write"}}
}

// zeroPlan returns a zero-value Definition, never built through
// flow.New. It carries no step, so it needs no cycle check.
func zeroPlan(t *testing.T) *flow.Definition {
	return &flow.Definition{}
}

// validPlan builds a two-step, no-panel Definition through flow.New.
func validPlan(t *testing.T) *flow.Definition {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a"}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	return d
}

// newCase is one New table row. id and plan are builder funcs so a
// case can supply a nil value without sharing state across cases.
type newCase struct {
	name    string
	id      func(t *testing.T) *identity.Identity
	card    discovery.Card
	plan    func(t *testing.T) *flow.Definition
	wantErr error  // checked with errors.Is when non-nil
	errSub  string // checked with strings.Contains when wantErr is nil and wantNil is false
	wantNil bool   // want a nil error and a non-nil Agent
}

// nilIdentity stands in for the missing-identity case.
func nilIdentity(t *testing.T) *identity.Identity { return nil }

// nilPlan stands in for the missing-plan case.
func nilPlan(t *testing.T) *flow.Definition { return nil }

// newCases lists the New validation order and its accept paths.
func newCases() []newCase {
	return []newCase{
		{name: "valid triple builds an Agent", id: newIdentity, card: validCard(), plan: validPlan, wantNil: true},
		{name: "nil identity is rejected", id: nilIdentity, card: validCard(), plan: validPlan, wantErr: agent.ErrNoIdentity},
		{name: "blank card name is rejected", id: newIdentity, card: discovery.Card{Name: "   ", Capabilities: []string{"read"}}, plan: validPlan, errSub: "name is required"},
		{name: "empty capability list is rejected", id: newIdentity, card: discovery.Card{Name: "Agent A", Capabilities: []string{}}, plan: validPlan, errSub: "capabilities must not be empty"},
		{name: "duplicate capability differing only in case is rejected", id: newIdentity, card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", "Read"}}, plan: validPlan, errSub: "duplicate capability"},
		{name: "whitespace-only capability entry is rejected", id: newIdentity, card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", "\t\n "}}, plan: validPlan, errSub: "capability entry must not be blank"},
		{name: "nil plan is rejected", id: newIdentity, card: validCard(), plan: nilPlan, wantErr: agent.ErrNoPlan},
		{name: "zero-value plan is accepted", id: newIdentity, card: validCard(), plan: zeroPlan, wantNil: true},
		{name: "nil identity and nil plan: identity error wins", id: nilIdentity, card: validCard(), plan: nilPlan, wantErr: agent.ErrNoIdentity},
		{name: "invalid card and nil plan: card error wins", id: newIdentity, card: discovery.Card{Name: "", Capabilities: []string{"read"}}, plan: nilPlan, errSub: "name is required"},
		{name: "nil identity and invalid card: identity error wins", id: nilIdentity, card: discovery.Card{Name: "", Capabilities: []string{"read"}}, plan: validPlan, wantErr: agent.ErrNoIdentity},
	}
}

// TestNew covers the New validation order and its accept paths.
func TestNew(t *testing.T) {
	for _, tt := range newCases() {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.id(t)
			plan := tt.plan(t)
			a, err := agent.New(id, tt.card, plan)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("New() unexpected error: %v", err)
				}
				if a == nil {
					t.Fatal("New() returned a nil Agent, want non-nil")
				}
				return
			}
			if err == nil {
				t.Fatal("New() returned a nil error, want error")
			}
			if a != nil {
				t.Fatalf("New() returned a non-nil Agent %+v on error, want nil", a)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("New() error = %v, want errors.Is match for %v", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "invalid card") {
					t.Fatalf("New() error = %v, want a sentinel error, not the wrapped card error", err)
				}
				return
			}
			if !strings.Contains(err.Error(), tt.errSub) {
				t.Fatalf("New() error = %q, want substring %q", err.Error(), tt.errSub)
			}
			if errors.Is(err, agent.ErrNoPlan) {
				t.Fatalf("New() error = %v, want the wrapped card error, not ErrNoPlan", err)
			}
		})
	}
}

// TestNewNilIdentityBeforePlanProvesOrder proves New checks id
// before plan: a nil identity and a nil plan together must report
// ErrNoIdentity, not ErrNoPlan.
func TestNewNilIdentityBeforePlanProvesOrder(t *testing.T) {
	_, err := agent.New(nil, validCard(), nil)
	if !errors.Is(err, agent.ErrNoIdentity) {
		t.Fatalf("New() error = %v, want errors.Is match for ErrNoIdentity", err)
	}
	if errors.Is(err, agent.ErrNoPlan) {
		t.Fatalf("New() error = %v, want no ErrNoPlan match", err)
	}
}

// TestNewInvalidCardBeforePlanProvesOrder proves New checks the card
// before the plan: an invalid card and a nil plan together must
// report the wrapped card error, not ErrNoPlan.
func TestNewInvalidCardBeforePlanProvesOrder(t *testing.T) {
	id := newIdentity(t)
	card := discovery.Card{Name: "", Capabilities: []string{"read"}}
	_, err := agent.New(id, card, nil)
	if err == nil {
		t.Fatal("New() returned a nil error, want error")
	}
	if errors.Is(err, agent.ErrNoPlan) {
		t.Fatalf("New() error = %v, want the wrapped card error, not ErrNoPlan", err)
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("New() error = %q, want substring %q", err.Error(), "name is required")
	}
}

// TestName confirms Name returns the card's Name field unchanged,
// including a case with leading and trailing whitespace, which
// Card.Validate accepts because it only rejects a name that is blank
// after TrimSpace. Name applies no trim of its own: it returns the
// stored value as-is.
func TestName(t *testing.T) {
	tests := []struct {
		name     string
		cardName string
	}{
		{name: "returns the stored name unchanged", cardName: "Agent A"},
		{name: "does not trim a padded name", cardName: " Agent A "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := newIdentity(t)
			card := discovery.Card{Name: tt.cardName, Capabilities: []string{"read"}}
			a, err := agent.New(id, card, validPlan(t))
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
			if got := a.Name(); got != tt.cardName {
				t.Fatalf("Name() = %q, want %q", got, tt.cardName)
			}
		})
	}
}

// TestCapabilitiesAliasesTheCard proves Capabilities returns the
// card's own backing array, not a defensive copy. It compares the
// addresses of the first elements, then mutates the returned slice
// and confirms the source card observed the same change.
func TestCapabilitiesAliasesTheCard(t *testing.T) {
	id := newIdentity(t)
	card := discovery.Card{Name: "Agent A", Capabilities: []string{"read", "write"}}
	a, err := agent.New(id, card, validPlan(t))
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	got := a.Capabilities()
	if len(got) == 0 {
		t.Fatal("Capabilities() returned an empty slice, want at least one entry")
	}
	if &got[0] != &card.Capabilities[0] {
		t.Fatal("Capabilities() returned a copy, want the same backing array as the source card")
	}
	got[0] = "mutated"
	if card.Capabilities[0] != "mutated" {
		t.Fatalf("card.Capabilities[0] = %q after mutating the returned slice, want %q", card.Capabilities[0], "mutated")
	}
}
