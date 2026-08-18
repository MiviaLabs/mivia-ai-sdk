package agentrun_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// twoStepChild returns a child workflow whose first step fires
// queued-to-mid and whose second step fires mid-to-final.
func twoStepChild(t *testing.T) *flow.Definition {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: "mid", Payload: "a"},
		{ID: "c2", To: "final", Needs: []string{"c1"}, Payload: "b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	return child
}

// TestValidateMatrixSubChildRows proves the walk checks a Sub child's
// own rows, not only the parent-level row to the child's finals.
func TestValidateMatrixSubChildRows(t *testing.T) {
	plan, err := flow.New([]flow.Step{{ID: "outer", Sub: twoStepChild(t)}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	t.Run("complete machine passes", func(t *testing.T) {
		m := mustMachine(t, "queued",
			tr("queued", "final", "p"),
			tr("queued", "mid", "c"),
			tr("mid", "final", "d"),
		)
		assertMatrixPasses(t, plan, m)
	})
	t.Run("missing parent-level row fails", func(t *testing.T) {
		m := mustMachine(t, "queued",
			tr("queued", "mid", "c"),
			tr("mid", "final", "d"),
		)
		assertMatrixFails(t, plan, m, "outer", "queued", "final")
	})
	t.Run("missing child row fails naming the child step", func(t *testing.T) {
		// Only the parent-level row exists. The child's own queued-to-mid
		// row is absent, so a run aborts on step c1 at New, not mid-run.
		m := mustMachine(t, "queued",
			tr("queued", "final", "p"),
		)
		assertMatrixFails(t, plan, m, "c1", "queued", "mid")
	})
}

// multiNeedChild returns a child whose terminal step needs two root
// steps, so childFinals must filter the roots out of the final set.
func multiNeedChild(t *testing.T) *flow.Definition {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{ID: "r1", To: "r1s", Payload: "a"},
		{ID: "r2", To: "r2s", Payload: "b"},
		{ID: "c2", To: "final", Needs: []string{"r1", "r2"}, Payload: "c"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	return child
}

// TestValidateMatrixSubNeedsOnlyTerminalRows proves a non-root Sub step
// requires rows to the child's terminal statuses only. The child's
// root rows already cover the initial status, so requiring them again
// from the parent's predecessor would be wrong.
func TestValidateMatrixSubNeedsOnlyTerminalRows(t *testing.T) {
	child := multiNeedChild(t)
	plan, err := flow.New([]flow.Step{
		{ID: "pre", To: "preS"},
		{ID: "sub1", Sub: child, Needs: []string{"pre"}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	// No preS-to-r1s or preS-to-r2s row exists, by design.
	m := mustMachine(t, "queued",
		tr("queued", "preS", "p"),
		tr("preS", "final", "q"),
		tr("queued", "r1s", "r"),
		tr("queued", "r2s", "s"),
		tr("r1s", "final", "t"),
		tr("r2s", "final", "u"),
	)
	assertMatrixPasses(t, plan, m)
}

// twoFinalChild returns a child workflow with two terminal statuses:
// c2 and c3 both need c1 and nobody needs them.
func twoFinalChild(t *testing.T) *flow.Definition {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: "cs1", Payload: "a"},
		{ID: "c2", To: "cs2", Needs: []string{"c1"}, Payload: "b"},
		{ID: "c3", To: "cs3", Needs: []string{"c1"}, Payload: "c"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	return child
}

// loopMachine builds the machine a two-final loop plan targets, with
// or without the re-entry rows between the two child finals.
func loopMachine(t *testing.T, reentry bool) *machine.Definition {
	t.Helper()
	rows := []machine.Transition{
		tr("queued", "cs1", "a"),
		tr("cs1", "cs2", "b"),
		tr("cs1", "cs3", "c"),
		tr("queued", "cs2", "p"),
		tr("queued", "cs3", "q"),
	}
	if reentry {
		rows = append(rows,
			tr("cs2", "cs3", "r"),
			tr("cs3", "cs2", "s"),
		)
	}
	return mustMachine(t, "queued", rows...)
}

// TestValidateMatrixLoopReentry proves a loop that can run a second
// iteration needs a re-entry row between every pair of distinct child
// finals. machine.New forbids a self row, so a child that lands the
// same final twice always faults at runtime; that limit stays
// disclosed, not checked.
func TestValidateMatrixLoopReentry(t *testing.T) {
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	plan, err := flow.New([]flow.Step{
		{ID: "looper", Sub: twoFinalChild(t), Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	t.Run("missing re-entry row fails", func(t *testing.T) {
		assertMatrixFails(t, plan, loopMachine(t, false), "looper", "cs2", "cs3")
	})
	t.Run("re-entry rows present pass", func(t *testing.T) {
		assertMatrixPasses(t, plan, loopMachine(t, true))
	})
	t.Run("max one iteration needs no re-entry row", func(t *testing.T) {
		once, err := flow.New([]flow.Step{
			{ID: "looper", Sub: twoFinalChild(t), Loop: &flow.LoopPolicy{Max: 1}},
		}, nil)
		if err != nil {
			t.Fatalf("flow.New: %v", err)
		}
		assertMatrixPasses(t, once, loopMachine(t, false))
	})
	t.Run("single-final child demands no self row", func(t *testing.T) {
		// machine.New rejects a cs-to-cs row, so the walker must not
		// demand one for a child with one terminal status.
		child, err := flow.New([]flow.Step{{ID: "inner", To: "cs", Payload: "p"}}, nil)
		if err != nil {
			t.Fatalf("flow.New child: %v", err)
		}
		once, err := flow.New([]flow.Step{
			{ID: "looper", Sub: child, Loop: &flow.LoopPolicy{Guard: twice}},
		}, nil)
		if err != nil {
			t.Fatalf("flow.New: %v", err)
		}
		m := mustMachine(t, "queued",
			tr("queued", "cs", "a"),
			tr("cs", "done", "b"),
		)
		assertMatrixPasses(t, once, m)
	})
}
