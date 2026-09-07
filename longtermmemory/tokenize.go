package longtermmemory

import (
	"strings"
	"unicode"
)

// stopwords is the reference tokenize.go list, copied verbatim.
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "of": {}, "to": {}, "for": {},
	"on": {}, "in": {}, "at": {}, "with": {}, "by": {}, "and": {},
	"or": {}, "from": {}, "as": {}, "is": {}, "are": {}, "was": {},
	"be": {}, "it": {}, "its": {}, "that": {},
}

// tokenize lowercases s, splits it on every rune that is neither a
// letter nor a digit, and drops every stopword and empty token.
func tokenize(s string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		token := b.String()
		b.Reset()
		if _, stop := stopwords[token]; stop {
			return
		}
		tokens = append(tokens, token)
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// tokenSet indexes one token slice.
func tokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		set[t] = struct{}{}
	}
	return set
}

// jaccard reports the Jaccard similarity of two token sets: the
// intersection over the union. An empty union scores zero.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
