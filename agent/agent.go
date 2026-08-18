package agent

import (
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// Sentinel errors for New and Run; test with errors.Is. A card
// validation failure wraps discovery's own error instead, because
// discovery exports no sentinel.
var (
	ErrNoIdentity = errors.New("agent: identity is required")
	ErrNoPlan     = errors.New("agent: plan is required")
)

// Agent binds one identity, one capability card, and one step plan
// into a single declarative value. Build it with New; the fields
// stay unexported.
type Agent struct {
	id   *identity.Identity
	card discovery.Card
	plan *flow.Definition
}

// New builds an Agent from an identity, a capability card, and a
// step plan. It checks id for nil, calls card.Validate(), then
// checks plan for nil, in that order, and returns the first error
// hit. New does not re-run flow's cycle check: a plan built through
// flow.New already passed it, and a zero-value plan carries no step
// for the check to reject.
func New(id *identity.Identity, card discovery.Card, plan *flow.Definition) (*Agent, error) {
	if id == nil {
		return nil, ErrNoIdentity
	}
	if err := card.Validate(); err != nil {
		return nil, fmt.Errorf("agent: invalid card: %w", err)
	}
	if plan == nil {
		return nil, ErrNoPlan
	}
	return &Agent{id: id, card: card, plan: plan}, nil
}

// Name returns the card's Name field, unchanged. It applies no trim;
// Card.Validate already rejects a name that is blank after
// TrimSpace, and Name returns the stored value as-is.
func (a *Agent) Name() string {
	return a.card.Name
}

// Capabilities returns the card's Capabilities slice: the same
// backing array Parse or the caller set, with no defensive copy.
// This matches discovery.Card, which carries the same caller-owned
// mutability.
func (a *Agent) Capabilities() []string {
	return a.card.Capabilities
}
