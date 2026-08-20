package longtermmemory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

// row is one stored entry plus its tier.
type row struct {
	entry Entry
	core  bool
}

// Store is the in-memory tiered entry store. Safe for concurrent use
// through one mutex. The zero value is not usable; create one with
// New.
type Store struct {
	mu         sync.Mutex
	maxEntries int
	rows       map[string]*row
	scopes     map[string]map[string]struct{}
}

// New builds a Store. A non-positive maxEntries means
// DefaultMaxEntries.
func New(maxEntries int) *Store {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &Store{
		maxEntries: maxEntries,
		rows:       make(map[string]*row),
		scopes:     make(map[string]map[string]struct{}),
	}
}

// Save validates and stores one entry. An identical re-save is
// idempotent. At the consolidation load factor it consolidates first,
// then refuses with ErrStoreFull when the scope is still full.
// Consolidation can mint the id this call carries, so Save repeats the
// idempotency check after it and never overwrites a live row.
func (s *Store) Save(ctx context.Context, e Entry) (Result, error) {
	if err := e.Validate(); err != nil {
		return Result{}, err
	}
	if e.Created == "" {
		e.Created = time.Now().Format(dateLayout)
	}
	id := entryID(e)

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.rows[id]; ok {
		return resultOf(id, existing.entry), nil
	}
	scope := e.Scope
	if float64(len(s.scopes[scope]))/float64(s.maxEntries) >= ConsolidateLoadFactor {
		s.consolidateLocked(scope)
		if existing, ok := s.rows[id]; ok {
			return resultOf(id, existing.entry), nil
		}
		if len(s.scopes[scope]) >= s.maxEntries {
			return Result{}, ErrStoreFull
		}
	}
	s.rows[id] = &row{entry: e}
	s.addToScope(scope, id)
	return resultOf(id, e), nil
}

// Count returns the row count of one scope.
func (s *Store) Count(ctx context.Context, scope string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.scopes[scope]), nil
}

// Delete removes one entry by id. Unknown id fails ErrEntryNotFound.
// Consolidation never deletes core rows; only this call can.
func (s *Store) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrEntryNotFound
	}
	s.removeFromScope(r.entry.Scope, id)
	delete(s.rows, id)
	return nil
}

// PromoteToCore marks one entry core. Already-core is a no-op.
// ErrEntryNotFound and ErrCoreTierFull are the failures.
func (s *Store) PromoteToCore(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrEntryNotFound
	}
	if r.core {
		return nil
	}
	if s.coreCountLocked(r.entry.Scope) >= CoreTierCap {
		return ErrCoreTierFull
	}
	r.core = true
	return nil
}

// CoreEntries returns core rows of one scope, ordered created DESC,
// title ASC, id ASC, capped at CoreTierCap.
func (s *Store) CoreEntries(ctx context.Context, scope string) ([]Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.scopeIDsLocked(scope, func(r *row) bool { return r.core })
	sort.Slice(ids, func(i, j int) bool {
		return coreLess(s.rows[ids[i]], s.rows[ids[j]], ids[i], ids[j])
	})
	if len(ids) > CoreTierCap {
		ids = ids[:CoreTierCap]
	}
	out := make([]Result, 0, len(ids))
	for _, id := range ids {
		out = append(out, resultOf(id, s.rows[id].entry))
	}
	return out, nil
}

// coreLess orders two core rows: created DESC, then title ASC, then
// id ASC.
func coreLess(a, b *row, idA, idB string) bool {
	if a.entry.Created != b.entry.Created {
		return a.entry.Created > b.entry.Created
	}
	if a.entry.Title != b.entry.Title {
		return a.entry.Title < b.entry.Title
	}
	return idA < idB
}

// coreCountLocked counts core rows in one scope. Called with s.mu
// held.
func (s *Store) coreCountLocked(scope string) int {
	n := 0
	for id := range s.scopes[scope] {
		if s.rows[id].core {
			n++
		}
	}
	return n
}

// scopeIDsLocked lists one scope's ids whose row passes keep, in
// created-then-id order. Called with s.mu held.
func (s *Store) scopeIDsLocked(scope string, keep func(*row) bool) []string {
	var ids []string
	for id := range s.scopes[scope] {
		if keep(s.rows[id]) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		ri, rj := s.rows[ids[i]], s.rows[ids[j]]
		if ri.entry.Created != rj.entry.Created {
			return ri.entry.Created < rj.entry.Created
		}
		return ids[i] < ids[j]
	})
	return ids
}

// addToScope registers id under scope. Called with s.mu held.
func (s *Store) addToScope(scope, id string) {
	set, ok := s.scopes[scope]
	if !ok {
		set = make(map[string]struct{})
		s.scopes[scope] = set
	}
	set[id] = struct{}{}
}

// removeFromScope unregisters id from scope. Called with s.mu held.
func (s *Store) removeFromScope(scope, id string) {
	set, ok := s.scopes[scope]
	if !ok {
		return
	}
	delete(set, id)
	if len(set) == 0 {
		delete(s.scopes, scope)
	}
}

// entryID builds the content address of one stored entry: SHA-256
// hex over every field, so only an identical re-save dedupes.
func entryID(e Entry) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		e.Scope, e.Title, string(e.Verdict),
		strings.Join(e.Tags, "\x1e"), e.Created, e.Summary, e.Detail,
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// resultOf projects one stored row onto its Result form.
func resultOf(id string, e Entry) Result {
	return Result{
		ID:      id,
		Scope:   e.Scope,
		Title:   e.Title,
		Verdict: e.Verdict,
		Tags:    append([]string(nil), e.Tags...),
		Created: e.Created,
		Snippet: e.Summary,
	}
}
