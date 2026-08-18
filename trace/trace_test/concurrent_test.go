package trace_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestConcurrentStartSharesRootParent fans one shared root ctx out to
// many goroutines, the way a parallel wave does. Run under -race: the
// race detector staying silent is part of the assertion.
func TestConcurrentStartSharesRootParent(t *testing.T) {
	const count = 16
	tr := trace.New()
	rootCtx, root := tr.Start(context.Background(), "root")

	spans := make([]*trace.Span, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(slot int) {
			defer wg.Done()
			_, spans[slot] = tr.Start(rootCtx, fmt.Sprintf("child-%d", slot))
		}(i)
	}
	wg.Wait()

	seen := make(map[trace.SpanID]bool, count)
	for i, s := range spans {
		if seen[s.ID] {
			t.Fatalf("children %d and earlier share ID %d", i, s.ID)
		}
		seen[s.ID] = true
		if s.ParentID != root.ID {
			t.Fatalf("child %d ParentID = %d, want root ID %d", i, s.ParentID, root.ID)
		}
	}
}

// TestConcurrentSetAttributeAndEndOnOneSpan hammers one shared span
// from several goroutines. Run under -race: a data race fails the
// test run. Every goroutine sets its own key, so the final attribute
// count also proves each write landed.
func TestConcurrentSetAttributeAndEndOnOneSpan(t *testing.T) {
	const count = 8
	s := newSpan(t)

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(slot int) {
			defer wg.Done()
			s.SetAttribute(fmt.Sprintf("key-%d", slot), fmt.Sprintf("value-%d", slot))
			s.End()
		}(i)
	}
	wg.Wait()

	if s.EndTime().IsZero() {
		t.Fatal("EndTime() after concurrent End calls = zero, want recorded")
	}
	if got := len(s.Attributes()); got != count {
		t.Fatalf("len(Attributes()) after concurrent sets = %d, want %d", got, count)
	}
}
