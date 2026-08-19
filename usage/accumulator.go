package usage

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// The sentinels below cover Record's, Reset's, and WrapCompleter's
// rejection causes, checked with errors.Is.
var (
	// ErrBlankSessionID is Record's and Reset's error when sessionID is
	// blank after trimming, and WrapCompleter's construction error for
	// the same cause.
	ErrBlankSessionID = errors.New("usage: sessionID must not be blank")
	// ErrNilAccumulator is WrapCompleter's construction error for a
	// nil Accumulator.
	ErrNilAccumulator = errors.New("usage: accumulator must not be nil")
	// ErrNilCompleter is WrapCompleter's construction error for a nil
	// Completer.
	ErrNilCompleter = errors.New("usage: completer must not be nil")
)

// Accumulator holds one running provider.Usage total per session
// identifier, guarded for concurrent access. Its fields stay
// unexported; a caller reaches the state only through Record, Total,
// and Reset. New is the constructor; the zero value also works,
// because Record initializes the map on first call.
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
	if a.totals == nil {
		a.totals = make(map[string]provider.Usage)
	}
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
