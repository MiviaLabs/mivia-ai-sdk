package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestSummarizeSetsRequestMaxTokens(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := plan.NewSummarizer(f)
	want := 128
	s.MaxTokens = &want
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	if req.MaxTokens == nil {
		t.Fatal("request MaxTokens = nil, want the capped value")
	}
	if *req.MaxTokens != want {
		t.Fatalf("request MaxTokens = %d, want %d", *req.MaxTokens, want)
	}
}

func TestSummarizeLeavesMaxTokensNilByDefault(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := plan.NewSummarizer(f)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	if req.MaxTokens != nil {
		t.Fatalf("request MaxTokens = %d, want nil by default", *req.MaxTokens)
	}
}

func TestSummarizeRejectsNonPositiveMaxTokens(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{name: "zero", value: 0, wantErr: true},
		{name: "negative", value: -1, wantErr: true},
		{name: "positive control", value: 64, wantErr: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &scriptCompleter{replies: []string{validReply}}
			s, _ := plan.NewSummarizer(f)
			v := c.value
			s.MaxTokens = &v
			msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
			_, err := s.Summarize(context.Background(), msgs)
			if c.wantErr {
				if !errors.Is(err, plan.ErrMaxTokensNotPositive) {
					t.Fatalf("Summarize() error = %v, want errors.Is ErrMaxTokensNotPositive", err)
				}
				calls, _ := f.stats()
				if calls != 0 {
					t.Fatalf("completer calls = %d, want 0: the guard runs before the Completer call", calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Summarize() = %v, want nil", err)
			}
			_, req := f.stats()
			if req.MaxTokens == nil || *req.MaxTokens != c.value {
				t.Fatalf("request MaxTokens = %v, want %d", req.MaxTokens, c.value)
			}
		})
	}
}

func TestSummarizeRequestMaxTokensNotAliased(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := plan.NewSummarizer(f)
	original := 42
	s.MaxTokens = &original
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	_, req := f.stats()
	if req.MaxTokens == nil {
		t.Fatal("request MaxTokens = nil, want the capped value")
	}
	*req.MaxTokens = 999
	if *s.MaxTokens != original {
		t.Fatalf("s.MaxTokens = %d after mutating the captured request pointer, want %d unchanged", *s.MaxTokens, original)
	}
}

func TestSummarizeFreezesMaxTokensAfterFirstCall(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply, validReply}}
	s, _ := plan.NewSummarizer(f)
	first := 10
	s.MaxTokens = &first
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() first call = %v, want nil", err)
	}
	_, req1 := f.stats()
	if req1.MaxTokens == nil || *req1.MaxTokens != first {
		t.Fatalf("first request MaxTokens = %v, want %d", req1.MaxTokens, first)
	}
	second := 20
	s.MaxTokens = &second
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() second call = %v, want nil", err)
	}
	_, req2 := f.stats()
	if req2.MaxTokens == nil || *req2.MaxTokens != first {
		t.Fatalf("second request MaxTokens = %v, want the frozen first value %d", req2.MaxTokens, first)
	}
}
