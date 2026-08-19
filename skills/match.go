package skills

import (
	"sort"
	"strings"
)

// Match returns every registered skill with a Triggers entry equal to
// query under strings.EqualFold. Returns nil for a blank query or no
// hit. Match never trims query; a padded query does not match an
// unpadded trigger entry, mirroring discovery.Card.Match. Match
// searches Triggers only, never Name. Results sort by Name ascending,
// so the result is deterministic across calls regardless of Go's
// unspecified map iteration order.
func (r *Registry) Match(query string) []Skill {
	if query == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []Skill
	for _, s := range r.skills {
		for _, trigger := range s.Triggers {
			if strings.EqualFold(query, trigger) {
				matches = append(matches, s)
				break
			}
		}
	}
	if matches == nil {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Name < matches[j].Name
	})
	return matches
}
