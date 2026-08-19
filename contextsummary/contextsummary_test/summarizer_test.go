package contextsummary_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestNewSummarizerNilCompleter(t *testing.T) {
	s, err := contextsummary.NewSummarizer(nil)
	if s != nil {
		t.Fatal("NewSummarizer(nil) returned a Summarizer, want nil")
	}
	if !errors.Is(err, contextsummary.ErrNilCompleter) {
		t.Fatalf("NewSummarizer(nil) error = %v, want errors.Is ErrNilCompleter", err)
	}
}

func TestSummarizeHappyPath(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, err := contextsummary.NewSummarizer(f)
	if err != nil {
		t.Fatalf("NewSummarizer() = %v, want nil", err)
	}
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "old turn"},
		{Role: provider.RoleAssistant, Content: "older answer"},
		{Role: provider.RoleUser, Content: "newest turn"},
	}
	sum, err := s.Summarize(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	if sum.Objective != "Ship the release" || sum.State != "Two tests fail" {
		t.Fatalf("Summarize() = %+v, want decoded fields", sum)
	}
	calls, req := f.stats()
	if calls != 1 {
		t.Fatalf("completer calls = %d, want 1", calls)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("first message role = %q, want system", req.Messages[0].Role)
	}
	excerpts := req.Messages[1].Content
	newAt := strings.Index(excerpts, "newest turn")
	oldAt := strings.Index(excerpts, "old turn")
	if newAt < 0 || oldAt < 0 || newAt > oldAt {
		t.Fatalf("excerpts not newest first:\n%s", excerpts)
	}
	if req.Stream {
		t.Fatal("request Stream = true, want false")
	}
	if req.Model != "" {
		t.Fatalf("request Model = %q, want empty", req.Model)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("request Tools = %d, want 0", len(req.Tools))
	}
}

func TestSummarizeNoMessages(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := contextsummary.NewSummarizer(f)
	_, err := s.Summarize(context.Background(), nil)
	if !errors.Is(err, contextsummary.ErrNoMessages) {
		t.Fatalf("Summarize(nil) error = %v, want errors.Is ErrNoMessages", err)
	}
	calls, _ := f.stats()
	if calls != 0 {
		t.Fatalf("completer calls = %d, want 0", calls)
	}
}

func TestSummarizeCallErrorWrapsErrCallFailed(t *testing.T) {
	boom := errors.New("boom")
	f := &scriptCompleter{err: boom}
	s, _ := contextsummary.NewSummarizer(f)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	_, err := s.Summarize(context.Background(), msgs)
	if !errors.Is(err, contextsummary.ErrCallFailed) {
		t.Fatalf("Summarize() error = %v, want errors.Is ErrCallFailed", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("Summarize() error = %v, want the completer error wrapped", err)
	}
	calls, _ := f.stats()
	if calls != 1 {
		t.Fatalf("completer calls = %d, want exactly 1, no retry", calls)
	}
}

func TestSummarizeInvalidReplies(t *testing.T) {
	unknownFields := `{"Objective":"o","State":"s","Extra":"x"}`
	trailingBytes := `{"Objective":"o","State":"s"} trailing`
	overBound := `{"Objective":"` + strings.Repeat("a", 3*1024) + `","State":"s"}`
	blankObjective := `{"Objective":"  ","State":"s"}`
	cases := []struct {
		name  string
		reply string
	}{
		{name: "malformed json", reply: `{"Objective":`},
		{name: "empty reply", reply: ``},
		{name: "whitespace reply", reply: `   `},
		{name: "unknown field", reply: unknownFields},
		{name: "trailing bytes", reply: trailingBytes},
		{name: "over bound objective", reply: overBound},
		{name: "blank objective", reply: blankObjective},
		{name: "two code fences", reply: "```json\n{\"Objective\":\"o\",\"State\":\"s\"}\n```\n```json\n{}"},
		{name: "unclosed code fence", reply: "```json\n{\"Objective\":\"o\",\"State\":\"s\"}"},
		{name: "fence with no body", reply: "```"},
		{name: "array reply", reply: `["Objective"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &scriptCompleter{replies: []string{c.reply}}
			s, _ := contextsummary.NewSummarizer(f)
			msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
			_, err := s.Summarize(context.Background(), msgs)
			if !errors.Is(err, contextsummary.ErrInvalidReply) {
				t.Fatalf("Summarize() error = %v, want errors.Is ErrInvalidReply", err)
			}
		})
	}
}

func TestSummarizeFencedReplyAccepted(t *testing.T) {
	fenced := "```json\n" + validReply + "\n```"
	f := &scriptCompleter{replies: []string{fenced}}
	s, _ := contextsummary.NewSummarizer(f)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	sum, err := s.Summarize(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Summarize() = %v, want nil for one fenced reply", err)
	}
	if sum.Objective != "Ship the release" {
		t.Fatalf("Summarize() objective = %q, want the decoded value", sum.Objective)
	}
}

func TestSummarizeCanceledContext(t *testing.T) {
	f := &scriptCompleter{waitCtx: true}
	s, _ := contextsummary.NewSummarizer(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	_, err := s.Summarize(ctx, msgs)
	if !errors.Is(err, contextsummary.ErrCallFailed) {
		t.Fatalf("Summarize() error = %v, want errors.Is ErrCallFailed", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Summarize() error = %v, want the caller ctx to stay the authority", err)
	}
}

func TestSummarizeAppliesTimeoutWithoutDeadline(t *testing.T) {
	f := &scriptCompleter{replies: []string{validReply}}
	s, _ := contextsummary.NewSummarizer(f)
	before := time.Now()
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	if _, err := s.Summarize(context.Background(), msgs); err != nil {
		t.Fatalf("Summarize() = %v, want nil", err)
	}
	f.mu.Lock()
	dl, hasDL := f.deadline, f.hasDL
	f.mu.Unlock()
	if !hasDL {
		t.Fatal("completer ctx carried no deadline, want the SummaryTimeout cap")
	}
	slack := time.Second
	if dl.After(before.Add(contextsummary.SummaryTimeout + slack)) {
		t.Fatalf("deadline %v exceeds the %v cap from %v", dl, contextsummary.SummaryTimeout, before)
	}
}

func TestSummarizeNoRetry(t *testing.T) {
	f := &scriptCompleter{err: errors.New("once is enough")}
	s, _ := contextsummary.NewSummarizer(f)
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
	_, err := s.Summarize(context.Background(), msgs)
	if err == nil {
		t.Fatal("Summarize() = nil, want the failure")
	}
	calls, _ := f.stats()
	if calls != 1 {
		t.Fatalf("completer calls = %d, want 1: Summarize never retries", calls)
	}
}
