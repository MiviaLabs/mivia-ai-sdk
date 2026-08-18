package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// pipelineWork returns a work func that drives one real agentrun
// pipeline and counts its own calls.
func pipelineWork(t *testing.T, calls *int) func(context.Context) error {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: "build", To: "built", Payload: "src"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "built", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(e2e.PrefixTool{ToolName: "build", Prefix: "built:"}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent:     e2eAgent(t, "ceremony-agent", plan),
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return func(ctx context.Context) error {
		*calls++
		status, _, err := runner.Run(ctx, "thread-ceremony", machine.InOut{})
		if err != nil {
			return err
		}
		if status != "built" {
			return errors.New("ceremony work: pipeline ended on the wrong status")
		}
		got, ok := artifacts.Get("build")
		if !ok || got != "built:src" {
			return errors.New("ceremony work: pipeline artifact missing")
		}
		return nil
	}
}

// TestTaskrunWrapsARun proves the ledger ceremony drives one full
// agentrun pipeline once, replays return the terminal sentinel
// without re-running work, and a failed dependency blocks its
// dependent.
func TestTaskrunWrapsARun(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	opts := taskrun.Options{
		Ledger: l,
		Actor:  "e2e-actor",
		Owner:  "e2e-owner",
		Lease:  time.Minute,
	}

	// The composed task runs the pipeline exactly once.
	calls := 0
	err = taskrun.Run(ctx, opts, taskrun.Task{Key: "compose", Seq: 1},
		pipelineWork(t, &calls))
	if err != nil {
		t.Fatalf("taskrun.Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("work calls = %d, want 1", calls)
	}
	st, found, err := l.State(ctx, "compose")
	if err != nil || !found || st.Status != ledger.StatusCompleted {
		t.Fatalf("ledger state = %q,%v,%v, want completed", st.Status, found, err)
	}

	// A replay of the completed key never re-runs the work.
	err = taskrun.Run(ctx, opts, taskrun.Task{Key: "compose", Seq: 2},
		pipelineWork(t, &calls))
	if !errors.Is(err, taskrun.ErrTaskDone) {
		t.Fatalf("replay = %v, want ErrTaskDone", err)
	}
	if calls != 1 {
		t.Fatalf("work calls after replay = %d, want 1", calls)
	}

	// A failed dependency blocks a dependent admitted before the
	// failure. Admit the dependent first: the ledger blocks records
	// that exist when the failure lands.
	if _, err := l.Admit(ctx, "e2e-actor", "child", 1, "child", time.Now(), "dep"); err != nil {
		t.Fatalf("Admit child: %v", err)
	}
	if _, err := l.Admit(ctx, "e2e-actor", "dep", 1, "dep", time.Now()); err != nil {
		t.Fatalf("Admit dep: %v", err)
	}
	fence, err := l.Claim(ctx, "e2e-actor", "dep", "e2e-owner", time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Claim dep: %v", err)
	}
	if err := l.Complete(ctx, "e2e-actor", "dep", "e2e-owner", fence,
		ledger.StatusFailed, time.Now()); err != nil {
		t.Fatalf("Complete dep: %v", err)
	}
	blockedCalls := 0
	err = taskrun.Run(ctx, opts,
		taskrun.Task{Key: "child", Seq: 1, Needs: []ledger.IdempotencyKey{"dep"}},
		pipelineWork(t, &blockedCalls))
	if !errors.Is(err, taskrun.ErrTaskBlocked) {
		t.Fatalf("dependent = %v, want ErrTaskBlocked", err)
	}
	if blockedCalls != 0 {
		t.Fatalf("blocked work calls = %d, want 0", blockedCalls)
	}
}
