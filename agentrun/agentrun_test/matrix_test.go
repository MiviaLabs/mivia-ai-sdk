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
			tr("sa", "sc", "z"),
		)
		assertMatrixPasses(t, plan, m)
	})
}

// TestValidateMatrixPanel proves a panel unions its member precedessors
// into one admission row.
func TestValidateMatrixPanel(t *testing.T) {

	t.Run("panel unions member predecessors", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "x", To: "sx"},
			{ID: "y", To: "sy"},
			{ID: "a", To: "w", Needs: []string{"x"}},
			{ID: "b", To: "w", Needs: []string{"y"}},
		}, []flow.Panel{{"a", "b"}})
		missing := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("queued", "sy", "y"),
			tr("sy", "w", "y"),
		)
		assertMatrixFails(t, plan, missing, "sx", "w")

		complete := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("queued", "sy", "y"),
			tr("sx", "w", "xa"),
			tr("sy", "w", "ya"),
		)
		assertMatrixPasses(t, plan, complete)
	})

	t.Run("union needs every member predecessor", func(t *testing.T) {
		// The mirror of the case above: sx-to-w exists, sy-to-w is
		// absent. A union reduced to the first member would pass this
		// machine; the full union must reject it.
		plan := mustFlow(t, []flow.Step{
			{ID: "x", To: "sx"},
			{ID: "y", To: "sy"},
			{ID: "a", To: "w", Needs: []string{"x"}},
			{ID: "b", To: "w", Needs: []string{"y"}},
		}, []flow.Panel{{"a", "b"}})
		mirror := mustMachine(t, "queued",
			tr("queued", "sx", "x"),
			tr("queued", "sy", "y"),
			tr("sx", "w", "xa"),
		)
		assertMatrixFails(t, plan, mirror, "sy", "w")
	})
}

// TestValidateMatrixFallback proves a fallback step admits on a failed
// predecessor through the matrix walk.
func TestValidateMatrixFallback(t *testing.T) {
	t.Run("fallback needs failed preds and finals", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "a", To: "sa"},
			{ID: "fb", To: "fbw", Needs: []string{"a"}, When: flow.AdmissionOnFailed},
		}, nil)
		ok := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("queued", "fbw", "ga"),
			tr("sa", "fbw", "ha"),
		)
		assertMatrixPasses(t, plan, ok)
		missing := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("queued", "fbw", "ga"),
		)
		assertMatrixFails(t, plan, missing, "sa", "fbw")
	})

	t.Run("fallback mixes failed and succeeded needs", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "a", To: "sa"},
			{ID: "b", To: "sb"},
			{ID: "fb", To: "fbw", Needs: []string{"a", "b"}, When: flow.AdmissionOnFailed},
		}, nil)
		ok := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("queued", "sb", "b"),
			tr("queued", "fbw", "ga"),
			tr("sa", "fbw", "ha"),
			tr("sb", "fbw", "ia"),
		)
		assertMatrixPasses(t, plan, ok)
		broken := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("queued", "sb", "b"),
			tr("queued", "fbw", "ga"),
			tr("sa", "fbw", "ha"),
		)
		assertMatrixFails(t, plan, broken, "sb", "fbw")
	})

	// Pins the pre-fire predecessor: a Fire failure leaves the need's
	// own predecessor status, so the fallback also needs a row from the
	// need's pre-fire status, not only its post-fire To.
	t.Run("fallback needs the pre-fire row", func(t *testing.T) {
		plan := mustFlow(t, []flow.Step{
			{ID: "a", To: "sa"},
			{ID: "fb", To: "fbw", Needs: []string{"a"}, When: flow.AdmissionOnFailed},
		}, nil)
		// Has the post-fire row sa->fbw but lacks the pre-fire row
		// queued->fbw.
		missingPreFire := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("sa", "fbw", "h"),
		)
		assertMatrixFails(t, plan, missingPreFire, "queued", "fbw")

		complete := mustMachine(t, "queued",
			tr("queued", "sa", "a"),
			tr("sa", "fbw", "h"),
			tr("queued", "fbw", "g"),
		)
		assertMatrixPasses(t, plan, complete)
	})
}

// TestValidateMatrixSubLoops drives the sub- and loop-child final
// targets through the matrix walk.
func TestValidateMatrixSubLoops(t *testing.T) {
	t.Run("sub need targets child finals", func(t *testing.T) {
		child := mustFlow(t, []flow.Step{{ID: "inner", To: "cs"}}, nil)
		plan := mustFlow(t, []flow.Step{
			{ID: "sub1", Sub: child, To: "pt"},
			{ID: "parent", To: "done", Needs: []string{"sub1"}},
		}, nil)
		ok := mustMachine(t, "queued",
			tr("queued", "cs", "a"),
			tr("cs", "done", "b"),
		)
		assertMatrixPasses(t, plan, ok)
		missing := mustMachine(t, "queued", tr("queued", "cs", "a"))
		assertMatrixFails(t, plan, missing, "cs", "done")
	})

	t.Run("loop need targets child finals", func(t *testing.T) {
		child := mustFlow(t, []flow.Step{{ID: "inner", To: "cs"}}, nil)
		plan := mustFlow(t, []flow.Step{
			{ID: "looper", Sub: child, To: "lt", Loop: &flow.LoopPolicy{}},
			{ID: "regular", To: "done", Needs: []string{"looper"}},
		}, nil)
		ok := mustMachine(t, "queued",
			tr("queued", "cs", "a"),
			tr("cs", "done", "b"),
		)
		assertMatrixPasses(t, plan, ok)
		missing := mustMachine(t, "queued", tr("queued", "cs", "a"))
		assertMatrixFails(t, plan, missing, "cs", "done")
	})

	t.Run("deep multi-level sub chain", func(t *testing.T) {
		inner := mustFlow(t, []flow.Step{{ID: "i3", To: "deep"}}, nil)
		mid := mustFlow(t, []flow.Step{{ID: "m2", Sub: inner, To: "mm"}}, nil)
		outer := mustFlow(t, []flow.Step{{ID: "o1", Sub: mid, To: "oo"}}, nil)
		plan := mustFlow(t, []flow.Step{
			{ID: "t0", Sub: outer, To: "tt"},
			{ID: "parent", To: "done", Needs: []string{"t0"}},
		}, nil)
		ok := mustMachine(t, "queued",
			tr("queued", "deep", "a"),
			tr("deep", "done", "b"),
		)
		assertMatrixPasses(t, plan, ok)
		missing := mustMachine(t, "queued", tr("queued", "deep", "a"))
		assertMatrixFails(t, plan, missing, "deep", "done")
	})
}

// TestValidateMatrixNilGuards proves ValidateMatrix rejects a nil
// plan and a nil machine before it walks anything.
func TestValidateMatrixNilGuards(t *testing.T) {
	m := mustMachine(t, "queued", tr("queued", "done", "x"))
	if err := agentrun.ValidateMatrix(nil, m); err == nil {
		t.Fatal("ValidateMatrix(nil, m) returned nil, want a nil-plan error")
	}
	plan := mustFlow(t, []flow.Step{{ID: "s", To: "done"}}, nil)
	if err := agentrun.ValidateMatrix(plan, nil); err == nil {
		t.Fatal("ValidateMatrix(plan, nil) returned nil, want a nil-machine error")
	}
}

// TestValidateMatrixChildWithInternalNeeds proves childFinals excludes a
// child step that a sibling needs, so only the true terminal counts.
func TestValidateMatrixChildWithInternalNeeds(t *testing.T) {
	child := mustFlow(t, []flow.Step{
		{ID: "x", To: "cx"},
		{ID: "y", To: "cy", Needs: []string{"x"}},
	}, nil)
	plan := mustFlow(t, []flow.Step{
		{ID: "sub1", Sub: child, To: "pt"},
		{ID: "tail", To: "done", Needs: []string{"sub1"}},
	}, nil)

	// The child fires x from the initial status, then y from cx. The
	// parent row targets the terminal cy; cx never crosses the parent.
	ok := mustMachine(t, "queued",
		tr("queued", "cx", "c"),
		tr("cx", "cy", "d"),
		tr("queued", "cy", "a"),
		tr("cy", "done", "b"),
	)
	assertMatrixPasses(t, plan, ok)

	// A row for the internal cx instead of the terminal cy must fail.
	wrong := mustMachine(t, "queued",
		tr("queued", "cx", "a"),
		tr("cx", "done", "b"),
	)
	assertMatrixFails(t, plan, wrong, "cy")
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
