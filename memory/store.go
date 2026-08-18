package memory

import (
	"errors"
	"fmt"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Sentinel errors for Store operations; test with errors.Is.
var (
	// ErrNoBudget is the sentinel for a non-positive maxBytes passed
	// to New.
	ErrNoBudget = errors.New("memory: maxBytes must be positive")
	// ErrBudgetExceeded is the sentinel for a blob that Put rejects
	// because it is larger than the store's budget.
	ErrBudgetExceeded = errors.New("memory: content exceeds store budget")
	// ErrUnknownRef is the sentinel for a ref that Get does not hold.
	ErrUnknownRef = errors.New("memory: unknown ref")
)

// Store holds content-addressed blobs under a fixed byte budget.
// Mutex-guarded, safe for concurrent use. The zero value is not
// usable; create a Store with New. A Put that would exceed the
// budget evicts the oldest-inserted blobs, in insertion order, until
// the new blob fits.
type Store struct {
	mu       sync.Mutex
	maxBytes int
	total    int
	blobs    map[string][]byte
	order    []string
}

// New creates a Store with a fixed byte budget. A non-positive
// maxBytes wraps ErrNoBudget.
func New(maxBytes int) (*Store, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrNoBudget, maxBytes)
	}
	return &Store{
		maxBytes: maxBytes,
		blobs:    make(map[string][]byte),
	}, nil
}

// Put computes ref as envelope.ContextRef(string(content)) and
// stores content under it. A content whose length exceeds the
// store's budget wraps ErrBudgetExceeded and stores nothing. A
// content that fits evicts the oldest-inserted blobs, in insertion
// order, until the new blob fits within the budget, then stores it.
// Putting a content whose ref already exists overwrites the stored
// bytes and refreshes its insertion order to most-recent.
func (s *Store) Put(content []byte) (ref string, err error) {
	ref = envelope.ContextRef(string(content))
	if len(content) > s.maxBytes {
		return "", fmt.Errorf("%w: %d bytes over %d budget", ErrBudgetExceeded, len(content), s.maxBytes)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.blobs[ref]; ok {
		s.removeFromOrder(ref)
		s.total -= len(existing)
		delete(s.blobs, ref)
	}
	for s.total+len(content) > s.maxBytes && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		s.total -= len(s.blobs[oldest])
		delete(s.blobs, oldest)
	}

	cp := make([]byte, len(content))
	copy(cp, content)
	s.blobs[ref] = cp
	s.order = append(s.order, ref)
	s.total += len(content)
	return ref, nil
}

// removeFromOrder drops ref from the insertion-order slice. Called
// with s.mu held.
func (s *Store) removeFromOrder(ref string) {
	for i, r := range s.order {
		if r == ref {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// Get returns a copy of the blob stored under ref. An unknown ref
// wraps ErrUnknownRef. Get does not change insertion order.
func (s *Store) Get(ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob, ok := s.blobs[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRef, ref)
	}
	cp := make([]byte, len(blob))
	copy(cp, blob)
	return cp, nil
}
