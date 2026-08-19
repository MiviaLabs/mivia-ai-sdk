package longtermmemory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

// promoteCore saves one entry and promotes it.
func promoteCore(t *testing.T, s *longtermmemory.Store, e longtermmemory.Entry) {
	t.Helper()
	res, err := s.Save(context.Background(), e)
	if err != nil {
		t.Fatalf("Save %q: %v", e.Title, err)
	}
	if err := s.PromoteToCore(context.Background(), res.ID); err != nil {
		t.Fatalf("PromoteToCore %q: %v", e.Title, err)
	}
}

func TestCoreFrameShape(t *testing.T) {
	s := longtermmemory.New(0)
	promoteCore(t, s, distinct("First", "2026-01-01"))
	promoteCore(t, s, distinct("Second", "2026-02-01"))
	block, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame: %v", err)
	}
	if !strings.HasPrefix(block, longtermmemory.FrameOpenTag+"\n"+longtermmemory.FrameAdvisory+"\n") {
		t.Fatalf("block head malformed:\n%s", block)
	}
	if !strings.HasSuffix(block, longtermmemory.FrameCloseTag) {
		t.Fatalf("block tail malformed:\n%s", block)
	}
	if strings.Count(block, longtermmemory.FrameOpenTag) != 1 {
		t.Fatalf("open tag count = %d, want 1", strings.Count(block, longtermmemory.FrameOpenTag))
	}
	if strings.Count(block, longtermmemory.FrameCloseTag) != 1 {
		t.Fatalf("close tag count = %d, want 1", strings.Count(block, longtermmemory.FrameCloseTag))
	}
	if strings.Count(block, longtermmemory.FrameAdvisory) != 1 {
		t.Fatalf("advisory count = %d, want 1", strings.Count(block, longtermmemory.FrameAdvisory))
	}
	if !strings.Contains(block, "Second") || !strings.Contains(block, "First") {
		t.Fatalf("entries missing from the frame:\n%s", block)
	}
	if strings.Index(block, "Second") > strings.Index(block, "First") {
		t.Fatalf("entries out of created-DESC order:\n%s", block)
	}
}

func TestCoreFrameWholeEntriesUnderCap(t *testing.T) {
	s := longtermmemory.New(0)
	promoteCore(t, s, distinct("First entry", "2026-01-01"))
	promoteCore(t, s, distinct("Second entry", "2026-02-01"))
	overhead := len(longtermmemory.FrameOpenTag) + 1 + len(longtermmemory.FrameAdvisory) + 1 + len(longtermmemory.FrameCloseTag)
	trimmed, err := s.CoreFrame(context.Background(), "proj", overhead+8)
	if err != nil {
		t.Fatalf("CoreFrame under a tight cap: %v", err)
	}
	if len(trimmed) > overhead+8 {
		t.Fatalf("tight frame = %d bytes, want at most %d", len(trimmed), overhead+8)
	}
	if strings.Contains(trimmed, "Second") && strings.Contains(trimmed, "First") {
		t.Fatal("both whole entries fit a cap sized for overhead plus a fragment")
	}
	if strings.Contains(trimmed, "First") && !strings.Contains(trimmed, "Second") {
		t.Fatal("a partial-tail entry survived while the newest dropped: whole entries only")
	}
}

func TestCoreFrameEmptyCoreAndTightOverhead(t *testing.T) {
	s := longtermmemory.New(0)
	block, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame on an empty core: %v", err)
	}
	if block != "" {
		t.Fatalf("empty core frame = %q, want the empty block", block)
	}
	promoteCore(t, s, distinct("Only", "2026-01-01"))
	tight, err := s.CoreFrame(context.Background(), "proj", 4)
	if err != nil {
		t.Fatalf("CoreFrame under the overhead: %v", err)
	}
	if tight != "" {
		t.Fatalf("overhead-does-not-fit frame = %q, want the empty block", tight)
	}
}

func TestCoreFrameZeroMaxBytesUsesDefault(t *testing.T) {
	s := longtermmemory.New(0)
	for i := 0; i < 20; i++ {
		promoteCore(t, s, distinct(strings.Repeat("e", 60)+fmt.Sprintf("%03d", i), "2026-01-01"))
	}
	block, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame: %v", err)
	}
	if len(block) == 0 || len(block) > longtermmemory.DefaultFrameBytes {
		t.Fatalf("frame = %d bytes, want at most DefaultFrameBytes %d", len(block), longtermmemory.DefaultFrameBytes)
	}
}
