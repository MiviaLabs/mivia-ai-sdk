package contextplan_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestExcerptsTruncatePerItem(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := contextplan.NewSummarizer(f)
	long := strings.Repeat("x", contextplan.MaxFieldBytes+200)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: long}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	excerpt := req.Messages[1].Content
	if len(excerpt) > contextplan.MaxFieldBytes+1 {
		t.Fatalf("excerpt len = %d, want at most MaxFieldBytes plus newline", len(excerpt))
	}
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt is not valid UTF-8: %q", excerpt)
	}
}

func TestExcerptsTruncateInTotal(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := contextplan.NewSummarizer(f)
	msgs := make([]provider.Message, 64)
	for i := range msgs {
		msgs[i] = provider.Message{
			Role:    provider.RoleUser,
			Content: strings.Repeat("y", contextplan.MaxFieldBytes),
		}
	}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	excerpts := req.Messages[1].Content
	if len(excerpts) > contextplan.MaxExcerptTotalBytes {
		t.Fatalf("excerpt section len = %d, want at most MaxExcerptTotalBytes", len(excerpts))
	}
	promptTotal := len(req.Messages[0].Content) + len(excerpts)
	if promptTotal > len(req.Messages[0].Content)+contextplan.MaxExcerptTotalBytes {
		t.Fatalf("prompt grew past the fixed text plus the excerpt cap: %d", promptTotal)
	}
}

func TestExcerptsStopAtFirstThatDoesNotFit(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := contextplan.NewSummarizer(f)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "zzz-marker"}}
	for i := 0; i < 8; i++ {
		msgs = append(msgs, provider.Message{
			Role:    provider.RoleUser,
			Content: strings.Repeat("a", contextplan.MaxFieldBytes),
		})
	}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	excerpts := req.Messages[1].Content
	if !strings.Contains(excerpts, "aaaa") {
		t.Fatalf("filler excerpts missing:\n%s", excerpts[:64])
	}
	if strings.Contains(excerpts, "zzz-marker") {
		t.Fatalf("oldest excerpt that did not fit still included")
	}
}
