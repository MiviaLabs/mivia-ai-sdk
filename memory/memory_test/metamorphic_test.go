package memory_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

// blobOfSize returns a content-unique blob of exactly n bytes: a
// distinct fill byte per index keeps every returned ref distinct.
func blobOfSize(index, n int) []byte {
	fill := byte('A' + index%26)
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	// Stamp the index into the first bytes (when the blob is long
	// enough) so same-size, same-fill-letter blobs at different table
	// indices still differ; blobOfSize is called with index < 26 in
	// every table below, so the fill byte alone already guarantees
	// distinctness, and the stamp is a defensive belt.
	stamp := []byte(fmt.Sprintf("%d", index))
	copy(b, stamp)
	return b
}

// TestMetamorphicPutNeverExceedsBudget pins the property: a Put that
// triggers eviction never leaves the store's total size above its
// configured budget. Confirmed true against store.go: Put rejects an
// over-budget blob before storing anything, and its eviction loop
// runs while s.total+len(content) > s.maxBytes, so a stored blob
// never leaves s.total over s.maxBytes.
func TestMetamorphicPutNeverExceedsBudget(t *testing.T) {
	cases := map[string]struct {
		budget int
		sizes  []int
	}{
		"no eviction, sizes under budget": {
			budget: 20,
			sizes:  []int{3, 4, 5},
		},
		"single eviction": {
			budget: 10,
			sizes:  []int{5, 5, 5},
		},
		"multiple evictions from one put": {
			budget: 9,
			sizes:  []int{3, 3, 3, 6},
		},
		"repeated single evictions": {
			budget: 6,
			sizes:  []int{3, 3, 3, 3, 3},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := memory.New(tc.budget)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			var refs []string
			for i, size := range tc.sizes {
				ref, err := s.Put(blobOfSize(i, size))
				if err != nil {
					t.Fatalf("Put %d (size %d): %v", i, size, err)
				}
				refs = append(refs, ref)

				total := 0
				for _, r := range refs {
					blob, err := s.Get(r)
					if err == nil {
						total += len(blob)
					} else if !errors.Is(err, memory.ErrUnknownRef) {
						t.Fatalf("Get(%s): unexpected error %v", r, err)
					}
				}
				if total > tc.budget {
					t.Fatalf("after Put %d: live total %d exceeds budget %d", i, total, tc.budget)
				}
			}
		})
	}
}

// TestMetamorphicGetEvictedFailsYoungerAnswers pins the property: a
// Get of an evicted ref fails while a younger, non-evicted ref still
// answers. Confirmed true against store.go: the eviction loop drops
// s.order[0] first, oldest-inserted, and never touches a later entry
// that already fits.
func TestMetamorphicGetEvictedFailsYoungerAnswers(t *testing.T) {
	cases := map[string]struct {
		budget int
		sizes  []int
		// evictedIdx names the indices the final Put must have driven
		// out; survivorIdx names indices still resolvable afterward.
		evictedIdx  []int
		survivorIdx []int
	}{
		"single eviction depth": {
			budget:      8,
			sizes:       []int{4, 4, 4},
			evictedIdx:  []int{0},
			survivorIdx: []int{1, 2},
		},
		"two-deep eviction": {
			budget:      12,
			sizes:       []int{3, 3, 3, 3, 9},
			evictedIdx:  []int{0, 1, 2},
			survivorIdx: []int{3, 4},
		},
		"three-deep eviction": {
			budget:      10,
			sizes:       []int{2, 2, 2, 2, 2, 8},
			evictedIdx:  []int{0, 1, 2, 3},
			survivorIdx: []int{4, 5},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := memory.New(tc.budget)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			refs := make([]string, len(tc.sizes))
			blobs := make([][]byte, len(tc.sizes))
			for i, size := range tc.sizes {
				blob := blobOfSize(i, size)
				blobs[i] = blob
				ref, err := s.Put(blob)
				if err != nil {
					t.Fatalf("Put %d (size %d): %v", i, size, err)
				}
				refs[i] = ref
			}

			for _, idx := range tc.evictedIdx {
				if _, err := s.Get(refs[idx]); !errors.Is(err, memory.ErrUnknownRef) {
					t.Fatalf("Get(evicted index %d) = %v, want ErrUnknownRef", idx, err)
				}
			}
			for _, idx := range tc.survivorIdx {
				got, err := s.Get(refs[idx])
				if err != nil {
					t.Fatalf("Get(survivor index %d): %v", idx, err)
				}
				if string(got) != string(blobs[idx]) {
					t.Fatalf("Get(survivor index %d) = %q, want %q", idx, got, blobs[idx])
				}
			}
		})
	}
}
