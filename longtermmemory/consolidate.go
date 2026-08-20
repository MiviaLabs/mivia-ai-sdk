package longtermmemory

// nearDuplicateThreshold is the Jaccard similarity at or above which
// two rows merge, matching the reference threshold.
const nearDuplicateThreshold = 0.82

// consolidateLocked runs one fixed consolidation over a scope: one
// merge pass over near-duplicate rows, then oldest-archive eviction in
// a loop until the scope holds fewer than maxEntries rows or nothing
// evictable remains. With exactly one core side, the core row survives
// the merge. A core row is never evicted. Called with s.mu held.
func (s *Store) consolidateLocked(scope string) {
	s.mergePassLocked(scope)
	s.evictArchiveLocked(scope)
}

// mergePassLocked walks the scope's rows in created-then-id order and
// merges each near-duplicate pair exactly once. The survivor keeps
// the union of both tag lists, deduplicated, in first-seen order,
// capped at maxTags. When exactly one side is core, that side survives
// regardless of creation order; when both sides share a tier, the
// earlier Created row survives, with the lower id breaking a tie. The
// merged tags change the content address, so the pass re-keys every
// survivor after the walk ends. A re-key inside the walk would leave
// the walk's own id list pointing at deleted rows.
func (s *Store) mergePassLocked(scope string) {
	ids := s.scopeIDsLocked(scope, func(*row) bool { return true })
	dead := make(map[string]struct{})
	var merged []string
	for i, idA := range ids {
		if _, gone := dead[idA]; gone {
			continue
		}
		for j := i + 1; j < len(ids); j++ {
			idB := ids[j]
			if _, gone := dead[idB]; gone {
				continue
			}
			if !nearDuplicate(s.rows[idA].entry, s.rows[idB].entry) {
				continue
			}
			keepID, dropID := idA, idB
			if s.rows[idB].core && !s.rows[idA].core {
				keepID, dropID = idB, idA
			}
			s.mergeRows(keepID, dropID)
			merged = append(merged, keepID)
			dead[dropID] = struct{}{}
			if dropID == idA {
				break
			}
		}
	}
	s.rekeyMergedLocked(merged)
}

// mergeRows folds dropID's tags into keepID's row, capped at maxTags,
// and deletes dropID. Called with s.mu held.
func (s *Store) mergeRows(keepID, dropID string) {
	keep := s.rows[keepID]
	drop := s.rows[dropID]
	keep.entry.Tags = unionTags(keep.entry.Tags, drop.entry.Tags, maxTags)
	s.removeFromScope(drop.entry.Scope, dropID)
	delete(s.rows, dropID)
}

// rekeyMergedLocked moves each merged survivor to its new content
// address, in pass order. It skips an id a later merge of the same
// pass deleted, and an id whose recomputed value is unchanged. A
// recomputed id that already exists holds identical content, so the
// moved row replaces it. The row pointer moves whole, so the core flag
// follows the survivor. Called with s.mu held.
func (s *Store) rekeyMergedLocked(ids []string) {
	for _, oldID := range ids {
		r, ok := s.rows[oldID]
		if !ok {
			continue
		}
		newID := entryID(r.entry)
		if newID == oldID {
			continue
		}
		s.rows[newID] = r
		s.addToScope(r.entry.Scope, newID)
		delete(s.rows, oldID)
		s.removeFromScope(r.entry.Scope, oldID)
	}
}

// nearDuplicate reports whether two entries' title-plus-summary
// token sets reach the near-duplicate threshold.
func nearDuplicate(a, b Entry) bool {
	tokensA := tokenSet(tokenize(a.Title + " " + a.Summary))
	tokensB := tokenSet(tokenize(b.Title + " " + b.Summary))
	return jaccard(tokensA, tokensB) >= nearDuplicateThreshold
}

// unionTags merges two tag lists, deduplicated, first-seen order,
// and stops at limit tags. The keep list fills the result first, so
// the drop list loses its tags when the union is over the cap.
func unionTags(keep, drop []string, limit int) []string {
	seen := make(map[string]struct{}, len(keep)+len(drop))
	out := make([]string, 0, limit)
	for _, tags := range [][]string{keep, drop} {
		for _, tag := range tags {
			if len(out) >= limit {
				return out
			}
			if _, dup := seen[tag]; dup {
				continue
			}
			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}

// evictArchiveLocked drops the oldest archive rows, by Created then
// id, until the scope holds fewer than maxEntries rows or no archive
// row remains. Called with s.mu held.
func (s *Store) evictArchiveLocked(scope string) {
	for len(s.scopes[scope]) >= s.maxEntries {
		oldest := ""
		for id := range s.scopes[scope] {
			r := s.rows[id]
			if r.core {
				continue
			}
			if oldest == "" || olderThan(r.entry, s.rows[oldest].entry, id, oldest) {
				oldest = id
			}
		}
		if oldest == "" {
			return
		}
		s.removeFromScope(scope, oldest)
		delete(s.rows, oldest)
	}
}

// olderThan reports whether a sorts before b under the eviction
// order: Created, then id, oldest first.
func olderThan(a, b Entry, idA, idB string) bool {
	if a.Created != b.Created {
		return a.Created < b.Created
	}
	return idA < idB
}
