package durablefence_test

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// errRefNotHeld is returned by the reference implementation's Mutate
// and Release when the resource is not currently held.
var errRefNotHeld = errors.New("reference: resource is not held")

// errRefAlreadyHeld is returned by the reference implementation's
// Claim when the resource is already held.
var errRefAlreadyHeld = errors.New("reference: resource is already held")

// errRefFenced is returned by the reference implementation's Mutate
// when token no longer matches the current owner token.
var errRefFenced = errors.New("reference: token is fenced")

// referenceClaim is a small, in-memory reference claim implementation
// guarded by one mutex. It tracks the current owner token as a string,
// a monotonic counter for issuing new tokens, and the set of tokens a
// Takeover has since superseded.
type referenceClaim struct {
	mu      sync.Mutex
	held    bool
	owner   string
	counter uint64
	fenced  map[string]bool
}

// newToken issues the next monotonic token, guarded by the caller
// already holding r.mu.
func (r *referenceClaim) newToken() string {
	r.counter++
	return strconv.FormatUint(r.counter, 10)
}

// claim grants a fresh owner token when the resource is unheld.
func (r *referenceClaim) claim(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held {
		return "", errRefAlreadyHeld
	}
	r.owner = r.newToken()
	r.held = true
	return r.owner, nil
}

// takeover always succeeds and reassigns the owner regardless of the
// token passed in, matching a real fencing implementation's contract:
// a takeover does not require proof of the current owner. It marks
// the superseded owner token fenced.
func (r *referenceClaim) takeover(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner != "" {
		r.fenced[r.owner] = true
	}
	r.owner = r.newToken()
	r.held = true
	return r.owner, nil
}

// mutate rejects a call whose token does not match the current owner.
func (r *referenceClaim) mutate(_ context.Context, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held {
		return errRefNotHeld
	}
	if token != r.owner {
		return errRefFenced
	}
	return nil
}

// release clears the hold when token matches the current owner.
func (r *referenceClaim) release(_ context.Context, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held {
		return errRefNotHeld
	}
	if token != r.owner {
		return errRefFenced
	}
	r.held = false
	r.owner = ""
	return nil
}

// isHeld reports the current hold state.
func (r *referenceClaim) isHeld(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held, nil
}

// isFenced reports whether token has been superseded by a Takeover.
// A never-issued token and the current active token both report
// false; only a token a Takeover has since replaced reports true.
func (r *referenceClaim) isFenced(_ context.Context, token string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fenced[token], nil
}

// newReferenceClaim builds an empty, unheld referenceClaim.
func newReferenceClaim() *referenceClaim {
	return &referenceClaim{fenced: make(map[string]bool)}
}

// scenario wires r's methods into a durablefence.Scenario.
func (r *referenceClaim) scenario() durablefence.Scenario {
	return durablefence.Scenario{
		Claim:    r.claim,
		Takeover: r.takeover,
		Mutate:   r.mutate,
		Release:  r.release,
		IsHeld:   r.isHeld,
		IsFenced: r.isFenced,
	}
}
