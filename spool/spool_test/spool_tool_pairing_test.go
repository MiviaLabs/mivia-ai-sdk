// SpoolTool/ReadOutputTool pairing tests: a shared *spool.Spool wires
// a SpoolTool-wrapped tool to a ReadOutputTool reading its grants
// back, and two SpoolTool-wrapped tools sharing one budget.
package spool_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestSpoolToolNilSpool checks SpoolTool's construction-time nil
// check: a nil sp fails with ErrNilSpool and returns a nil tools.Tool.
func TestSpoolToolNilSpool(t *testing.T) {
	wrapped, err := spool.SpoolTool("t", 10, nil, stringTool{name: "inner", result: "x"})
	if !errors.Is(err, spool.ErrNilSpool) {
		t.Fatalf("SpoolTool(nil sp) err = %v, want ErrNilSpool", err)
	}
	if wrapped != nil {
		t.Errorf("SpoolTool(nil sp) tool = %v, want nil", wrapped)
	}
}

// splitMoreMarker strips a trailing MoreMarker suffix from page, if
// present, and reports the next offset it names.
func splitMoreMarker(page string) (body string, next int, more bool) {
	idx := strings.Index(page, "[more: offset=")
	if idx < 0 {
		return page, 0, false
	}
	var n int
	if _, err := fmt.Sscanf(page[idx:], spool.MoreMarker, &n); err != nil {
		return page, 0, false
	}
	return page[:idx], n, true
}

// TestSpoolToolReadOutputToolRoundTrip pairs one SpoolTool-wrapped
// tool with a ReadOutputTool over the same *spool.Spool: the tool
// spools an oversized result, and the read-back tool pages the full
// body back by the ref the wrapped tool's view named. This pairing
// was unreachable before SpoolTool exposed sp to its caller.
func TestSpoolToolReadOutputToolRoundTrip(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	full := strings.Repeat("abcdefghij", 100)
	wrapped, err := spool.SpoolTool("big", 10, sp, stringTool{name: "inner", result: full})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
	readBack, err := spool.ReadOutputTool(sp, 200)
	if err != nil {
		t.Fatalf("ReadOutputTool: %v", err)
	}

	ctx := spool.WithPrincipal(context.Background(), "alice")
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("wrapped.Run: %v", err)
	}
	view, ok := out.Value.(string)
	if !ok {
		t.Fatalf("wrapped view = %v, want a string", out.Value)
	}
	ref := refFor([]byte(full))
	if !strings.Contains(view, ref) {
		t.Fatalf("view %q does not name ref %q", view, ref)
	}

	var rebuilt strings.Builder
	offset := 0
	for {
		page, err := runRead(t, readBack, "alice", fmt.Sprintf(`{"ref":"%s","offset":%d}`, ref, offset))
		if err != nil {
			t.Fatalf("readBack.Run at offset %d: %v", offset, err)
		}
		text, ok := page.Value.(string)
		if !ok {
			t.Fatalf("page.Value = %v, want a string", page.Value)
		}
		body, next, more := splitMoreMarker(text)
		rebuilt.WriteString(body)
		if !more {
			break
		}
		offset = next
	}
	if rebuilt.String() != full {
		t.Errorf("rebuilt body (len %d) does not equal the original %d-byte result", rebuilt.Len(), len(full))
	}
}

// TestSpoolToolSharedBudget shows two SpoolTool-wrapped tools sharing
// one *spool.Spool share its grant budget: spooling the second
// oversized result evicts the first tool's grant once their combined
// size passes the shared budget.
func TestSpoolToolSharedBudget(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 12)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	firstResult := strings.Repeat("a", 10)
	secondResult := strings.Repeat("b", 10)
	firstTool, err := spool.SpoolTool("first", 1, sp, stringTool{name: "inner-a", result: firstResult})
	if err != nil {
		t.Fatalf("SpoolTool(first): %v", err)
	}
	secondTool, err := spool.SpoolTool("second", 1, sp, stringTool{name: "inner-b", result: secondResult})
	if err != nil {
		t.Fatalf("SpoolTool(second): %v", err)
	}

	ctx := spool.WithPrincipal(context.Background(), "alice")
	if _, err := firstTool.Run(ctx, tools.InOut{}); err != nil {
		t.Fatalf("firstTool.Run: %v", err)
	}
	if _, err := secondTool.Run(ctx, tools.InOut{}); err != nil {
		t.Fatalf("secondTool.Run: %v", err)
	}

	firstRef := refFor([]byte(firstResult))
	secondRef := refFor([]byte(secondResult))
	if _, err := sp.Load(ctx, "alice", firstRef); !errors.Is(err, spool.ErrUnknownRef) {
		t.Errorf("Load(firstRef) err = %v, want ErrUnknownRef: the shared budget should have evicted the older grant", err)
	}
	got, err := sp.Load(ctx, "alice", secondRef)
	if err != nil || string(got) != secondResult {
		t.Errorf("Load(secondRef) = %q,%v, want the newer grant to still load", got, err)
	}
}
