package usage

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// ErrBlankSessionID is Record's and Reset's error when sessionID is
// empty after strings.TrimSpace, checked with errors.Is. The name and
// the TrimSpace-empty definition match the existing blank-identifier
// sentinels in this codebase: tools.ErrBlankName, trigger.ErrBlankName,
// providerregistry.ErrBlankName, and scheduler.ErrBlankID.
var ErrBlankSessionID = errors.New("usage: sessionID must not be blank")

// Accumulator holds one running provider.Usage total per session
// identifier, guarded for concurrent access. Its fields stay
// unexported; a caller reaches the state only through Record, Total,
// and Reset. Built only through New; the zero value's nil map panics
// on write.
type Accumulator struct {
	mu     sync.Mutex
	totals map[string]provider.Usage
}

// New creates an empty Accumulator ready to record.
func New() *Accumulator {
	return &Accumulator{totals: make(map[string]provider.Usage)}
}

// Record adds u's four fields onto the running total keyed by
// sessionID. Rejects a blank sessionID (empty after
// strings.TrimSpace) with ErrBlankSessionID, wrapped. Creates the
// session's total on its first Record call; every later call for the
// same sessionID adds onto the existing total. Safe to call from more
// than one goroutine for the same or different sessionID values.
func (a *Accumulator) Record(sessionID string, u provider.Usage) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("usage: sessionID %q: %w", sessionID, ErrBlankSessionID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	total := a.totals[sessionID]
	total.PromptTokens += u.PromptTokens
	total.CompletionTokens += u.CompletionTokens
	total.TotalTokens += u.TotalTokens
	total.CachedTokens += u.CachedTokens
	a.totals[sessionID] = total
	return nil
}

// Total returns the current summed provider.Usage for sessionID and
// true, or the zero provider.Usage and false when no Record call has
// ever named that sessionID.
func (a *Accumulator) Total(sessionID string) (provider.Usage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.totals[sessionID]
	return u, ok
}

// Reset clears sessionID's total back to zero, as if no Record call
// had ever named it. Rejects a blank sessionID (empty after
// strings.TrimSpace) with ErrBlankSessionID, wrapped. Reset on a
// sessionID with no prior Record call is a no-op that returns nil,
// not an error.
func (a *Accumulator) Reset(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("usage: sessionID %q: %w", sessionID, ErrBlankSessionID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.totals, sessionID)
	return nil
}
