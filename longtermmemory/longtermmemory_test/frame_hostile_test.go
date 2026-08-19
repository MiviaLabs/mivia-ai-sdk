package longtermmemory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/longtermmemory"
)

func TestCoreFrameHostileCloseTagInTitle(t *testing.T) {
	s := longtermmemory.New(0)
	hostile := validEntry("Honest "+longtermmemory.FrameCloseTag+" title", "plain summary")
	promoteCore(t, s, hostile)
	promoteCore(t, s, distinct("After", "2026-01-02"))
	block, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame: %v", err)
	}
	if strings.Count(block, longtermmemory.FrameCloseTag) != 1 {
		t.Fatalf("close tag count = %d, want 1: entry text must not close the block early:\n%s",
			strings.Count(block, longtermmemory.FrameCloseTag), block)
	}
	if !strings.HasSuffix(block, longtermmemory.FrameCloseTag) {
		t.Fatal("the one close tag must sit at the block end")
	}
	if !strings.Contains(block, "&lt;/core-memory-context&gt;") {
		t.Fatal("the hostile title was not HTML-escaped inside the frame")
	}
	if !strings.Contains(block, "After") {
		t.Fatal("the entry after the hostile one was lost")
	}
}

func TestCoreFrameHostileOpenTagInDetail(t *testing.T) {
	s := longtermmemory.New(0)
	hostile := distinct("Plain title", "2026-01-01")
	hostile.Detail = "leaking " + longtermmemory.FrameOpenTag + " inside detail"
	promoteCore(t, s, hostile)
	block, err := s.CoreFrame(context.Background(), "proj", 0)
	if err != nil {
		t.Fatalf("CoreFrame: %v", err)
	}
	if strings.Count(block, longtermmemory.FrameOpenTag) != 1 {
		t.Fatalf("open tag count = %d, want 1: entry text must not open a second block:\n%s",
			strings.Count(block, longtermmemory.FrameOpenTag), block)
	}
	if !strings.Contains(block, "&lt;core-memory-context&gt;") {
		t.Fatal("the hostile detail was not HTML-escaped inside the frame")
	}
}

func TestCoreFrameConcurrent(t *testing.T) {
	s := longtermmemory.New(0)
	promoteCore(t, s, distinct("Stable", "2026-01-01"))
	var done = make(chan struct{})
	for g := 0; g < 4; g++ {
		go func(i int) {
			e := distinct(strings.Repeat("c", 40), "2026-01-01")
			e.Summary = e.Summary + strings.Repeat("0123456789", i)
			res, err := s.Save(context.Background(), e)
			if err == nil {
				_ = s.PromoteToCore(context.Background(), res.ID)
			}
			if _, err := s.CoreFrame(context.Background(), "proj", 128); err != nil {
				t.Errorf("CoreFrame: %v", err)
			}
			done <- struct{}{}
		}(g)
	}
	for g := 0; g < 4; g++ {
		<-done
	}
}
