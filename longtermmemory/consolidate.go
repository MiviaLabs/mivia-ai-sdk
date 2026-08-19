package longtermmemory

// nearDuplicateThreshold is the Jaccard similarity at or above which
// two rows merge, matching the reference threshold.
const nearDuplicateThreshold = 0.82

// consolidateLocked runs one fixed consolidation over a scope: one
// merge pass over near-duplicate rows, then oldest-archive eviction in
// a loop until the scope holds fewer than maxEntries rows or nothing
// evictable remains. A core row is never the deleted side of a merge
// and never evicted. Called with s.mu held.
func (s *Store) consolidateLocked(scope string) {
	s.mergePassLocked(scope)
	s.evictArchiveLocked(scope)
}

// mergePassLocked walks the scope's rows in created-then-id order and
// merges each near-duplicate pair exactly once. The survivor keeps
// the union of both tag lists, deduplicated, in first-seen order.
// When exactly one side is core, that side survives regardless of
// creation order; when both sides share a tier, the earlier Created
// row survives, with the lower id breaking a tie.
func (s *Store) mergePassLocked(scope string) {
	ids := s.scopeIDsLocked(scope, func(*row) bool { return true })
	dead := make(map[string]struct{})
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
			dead[dropID] = struct{}{}
			if dropID == idA {
				break
			}
		}
	}
}

// mergeRows folds dropID's tags into keepID's row and deletes
// dropID. Called with s.mu held.
func (s *Store) mergeRows(keepID, dropID string) {
	keep := s.rows[keepID]
	drop := s.rows[dropID]
	keep.entry.Tags = unionTags(keep.entry.Tags, drop.entry.Tags)
	s.removeFromScope(drop.entry.Scope, dropID)
	delete(s.rows, dropID)
}

// nearDuplicate reports whether two entries' title-plus-summary
// token sets reach the near-duplicate threshold.
func nearDuplicate(a, b Entry) bool {
	tokensA := tokenSet(tokenize(a.Title + " " + a.Summary))
	tokensB := tokenSet(tokenize(b.Title + " " + b.Summary))
	return jaccard(tokensA, tokensB) >= nearDuplicateThreshold
}

// unionTags merges two tag lists, deduplicated, first-seen order.
func unionTags(keep, drop []string) []string {
	seen := make(map[string]struct{}, len(keep)+len(drop))
	out := make([]string, 0, len(keep)+len(drop))
	for _, tags := range [][]string{keep, drop} {
		for _, tag := range tags {
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
