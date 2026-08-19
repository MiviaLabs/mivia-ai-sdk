package longtermmemory

import (
	"context"
	"sort"
	"strings"
)

// Result is one search hit or core listing row.
type Result struct {
	ID      string
	Scope   string
	Title   string
	Verdict Verdict
	Tags    []string
	Created string
	Snippet string
}

// Query is one search request. Scope is required.
type Query struct {
	Text       string
	Scope      string
	MaxResults int
}

// Search returns one scope's entries in which every query token
// matches. Hits order by Created DESC, then id ASC. Empty text fails
// with ErrQueryRequired. When tokenizing yields nothing, the whole
// trimmed query matches as one substring.
func (s *Store) Search(ctx context.Context, q Query) ([]Result, error) {
	if strings.TrimSpace(q.Scope) == "" {
		return nil, ErrScopeRequired
	}
	if strings.TrimSpace(q.Text) == "" {
		return nil, ErrQueryRequired
	}
	tokens := tokenize(q.Text)
	phrase := strings.ToLower(strings.TrimSpace(q.Text))

	s.mu.Lock()
	defer s.mu.Unlock()
	var hits []Result
	for id := range s.scopes[q.Scope] {
		e := s.rows[id].entry
		if !matches(e, tokens, phrase) {
			continue
		}
		hits = append(hits, resultOf(id, e))
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Created != hits[j].Created {
			return hits[i].Created > hits[j].Created
		}
		return hits[i].ID < hits[j].ID
	})
	limit := q.MaxResults
	if limit <= 0 {
		limit = DefaultMaxSearchResults
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// matches reports whether one entry matches the query tokens, or the
// phrase fallback when no token survived stopword removal.
func matches(e Entry, tokens []string, phrase string) bool {
	if len(tokens) == 0 {
		blob := strings.ToLower(e.Title + " " + e.Summary + " " + e.Detail)
		return strings.Contains(blob, phrase)
	}
	set := tokenSet(tokenize(e.Title + " " + e.Summary + " " + e.Detail))
	for _, t := range tokens {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}
