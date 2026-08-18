package agentrun_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestValidateMatrixRows drives the admission-row cases of the
// transition-matrix walk.
func TestValidateMatrixRows(t *testing.T) {
	t.Run("root step missing row", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{{ID: "s", To: "done"}}, nil)
		m := mustMachine(t, "queued", tr("queued", "other", "x"))
		assertMatrixFails(t, plan, m, "done", "queued")
	})

	t.Run("dependent missing row", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "a", To: "sa"},
			{ID: "b", To: "sb", Needs: []string{"a"}},
		}, nil)
		m := mustMachine(t, "queued", tr("queued", "sa", "x"))
		assertMatrixFails(t, plan, m, "sb", "sa")
	})

	t.Run("ambiguous pair of rows", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{{ID: "s", To: "done"}}, nil)
		m := mustMachine(t, "queued",
			tr("queued", "done", "x"),
			tr("queued", "done", "y"),
		)
		err := agentrun.ValidateMatrix(plan, m)
		if err == nil {
			t.Fatal("ValidateMatrix returned nil, want ambiguous error")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("error %q lacks %q", err, "ambiguous")
		}
	})

	t.Run("route exclusion still passes", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "a", To: "sa", Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				return []string{"c"}, nil
			}},
			{ID: "b", To: "sb", Needs: []string{"a"}},
			{ID: "c", To: "sc", Needs: []string{"a"}},
		}, nil)
		m := mustMachine(t, "queued",
			tr("queued", "sa", "x"),
			tr("sa", "sb", "y"),
			tr("sb", "sc", "z"),
		)
		assertMatrixPasses(t, plan, m)
	})
}

// TestValidateMatrixPanel proves a panel unions its member precedessors
// into one admission row.
func TestValidateMatrixPanel(t *testing.T) {

	t.Run("wave fires from the standing set", func(t *testing.T) {
		// The roots run in declaration order: x lands on sx, then y
		// fires from sx. The wave fires once, from sy.
		plan := mustFlow(t, []flow.Step{
			{ID: "x", To: "sx"},
			{ID: "y", To: "sy"},
			{ID: "a", To: "w", Needs: []string{"x"}},
			{ID: "b", To: "w", Needs: []string{"y"}},
		}, []flow.Panel{{"a", "b"}})
		ok := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("sx", "sy", "y"),
			tr("sy", "w", "w"),
		)
		assertMatrixPasses(t, plan, ok)
		noWave := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("sx", "sy", "y"),
		)
		assertMatrixFails(t, plan, noWave, "sy", "w")
	})

	t.Run("sibling roots chain before the wave", func(t *testing.T) {
		// y fires from sx, not from the initial status: the walk keeps
		// one shared status and scans in declaration order.
		plan := mustFlow(t, []flow.Step{
			{ID: "x", To: "sx"},
			{ID: "y", To: "sy"},
			{ID: "a", To: "w", Needs: []string{"x"}},
			{ID: "b", To: "w", Needs: []string{"y"}},
		}, []flow.Panel{{"a", "b"}})
		stalled := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("queued", "sy", "y"),
			tr("sy", "w", "w"),
		)
		assertMatrixFails(t, plan, stalled, "sx", "sy")
	})
}

// TestValidateMatrixOneMemberPanel proves a one-member panel is not
// treated as a wave: its single member validates as a normal step.
func TestValidateMatrixOneMemberPanel(t *testing.T) {
	plan := mustFlow(t, []flow.Step{{ID: "s", To: "done"}}, []flow.Panel{{"s"}})
	ok := mustMachine(t, "queued", tr("queued", "done", "x"))
	assertMatrixPasses(t, plan, ok)

	missing := mustMachine(t, "queued", tr("queued", "other", "x"))
	assertMatrixFails(t, plan, missing, "done", "queued")
}

// assertMatrixPasses requires ValidateMatrix to return nil.
func assertMatrixPasses(t *testing.T, plan *flow.Definition, m *machine.Definition) {
	t.Helper()
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
}

// assertMatrixFails requires ValidateMatrix to error and to name every
// given substring.
func assertMatrixFails(t *testing.T, plan *flow.Definition, m *machine.Definition, subs ...string) {
	t.Helper()
	err := agentrun.ValidateMatrix(plan, m)
	if err == nil {
		t.Fatal("ValidateMatrix returned nil, want an error")
	}
	for _, s := range subs {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q lacks %q", err, s)
		}
	}
}

// mustFlow builds a flow.Definition over steps and panels, failing on
// error.
func mustFlow(t *testing.T, steps []flow.Step, panels []flow.Panel) *flow.Definition {
	t.Helper()
	p, err := flow.New(steps, panels)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// tr builds one transition row with a distinct trigger.
func tr(from, to, trig string) machine.Transition {
	return machine.Transition{From: machine.Status(from), To: machine.Status(to), Trigger: machine.Trigger(trig)}
}
