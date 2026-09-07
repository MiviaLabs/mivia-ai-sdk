package run_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// eqStatuses lists every status the equivalence fixture uses. The
// first entry is the initial status.
var eqStatuses = []machine.Status{"queued", "sx", "sy", "gathered", "routed", "done"}

// statusPair names one ordered transition row by its endpoints.
type statusPair struct {
	from machine.Status
	to   machine.Status
}

// eqCompleteMachine builds one row for every ordered pair of distinct
// statuses in eqStatuses, minus the pairs named in skip. Each row gets
// a distinct trigger, so machine.New accepts the table. Every status
// stays reachable from the initial status, so dropping one row still
// builds. A complete machine constrains nothing, so flow.Run picks its
// own path and the recorded order is flow's, not the fixture's.
func eqCompleteMachine(t *testing.T, skip ...statusPair) *machine.Definition {
	t.Helper()
	dropped := make(map[statusPair]bool, len(skip))
	for _, p := range skip {
		dropped[p] = true
	}
	var rows []machine.Transition
	for _, from := range eqStatuses {
		for _, to := range eqStatuses {
			if from == to || dropped[statusPair{from, to}] {
				continue
			}
			rows = append(rows, tr(string(from), string(to), fmt.Sprintf("%s-to-%s", from, to)))
		}
	}
	return mustMachine(t, eqStatuses[0], rows...)
}

// eqPlan builds the equivalence fixture. Two roots with no needs are
// ready at the same time, so declaration order alone decides which
// runs first: a total order would pin nothing. panelA and panelB share
// the gathered target and form the one wave. The route sits on router,
// below the panel, because flow.New rejects a panel member that is a
// direct dependent of a routed step, and it returns every direct
// dependent because the simulator walks the all-run path.
func eqPlan(t *testing.T) *flow.Definition {
	t.Helper()
	route := func(_ context.Context, _ machine.Status, _ machine.InOut) ([]string, error) {
		return []string{"finish"}, nil
	}
	return mustFlow(t, []flow.Step{
		{ID: "root", To: "sx"},
		{ID: "root2", To: "sy"},
		{ID: "panelA", To: "gathered", Needs: []string{"root"}},
		{ID: "panelB", To: "gathered", Needs: []string{"root2"}},
		{ID: "router", To: "routed", Needs: []string{"panelA", "panelB"}, Route: route},
		{ID: "finish", To: "done", Needs: []string{"router"}},
	}, []flow.Panel{{"panelA", "panelB"}})
}

// TestValidateMatrixMatchesRunOrder proves workflow/run's simulator demands
// exactly the set of transition rows flow.Run consumes, attributed to
// the same units. workflow/run/matrix.go re-implements flow's
// declaration-order scan, and nothing else compares the two.
//
// Set equality is the claim. These are the gaps it leaves open:
//   - Nothing here pins the two scans' relative ordering beyond what
//     the row set forces. Two walk orders with identical demand sets
//     are indistinguishable to these assertions.
//   - walkSim's walk is machine-independent. It reads only m.Initial();
//     the machine affects checkRow alone.
//   - Route exclusions stay outside the comparison. So do skipped
//     units. The fixture's route excludes nothing.
func TestValidateMatrixMatchesRunOrder(t *testing.T) {
	plan := eqPlan(t)
	complete := eqCompleteMachine(t)

	var confirmed []string
	confirm := func(_ context.Context, step flow.Step) error {
		confirmed = append(confirmed, step.ID)
		return nil
	}
	var chain []machine.Status
	onCheckpoint := func(c flow.Checkpoint) {
		chain = append(chain, c.Status)
	}
	if _, err := flow.Run(context.Background(), plan, complete, machine.InOut{}, confirm, nil, onCheckpoint); err != nil {
		t.Fatalf("flow.Run: %v", err)
	}

	// Assertion 1: Run skips Confirm for a wave of two or more
	// members, so the panel's two members never appear here.
	wantConfirmed := []string{"root", "root2", "router", "finish"}
	if !reflect.DeepEqual(confirmed, wantConfirmed) {
		t.Fatalf("Confirm order = %v, want %v", confirmed, wantConfirmed)
	}
	wantChain := []machine.Status{"sx", "sy", "gathered", "routed", "done"}
	if !reflect.DeepEqual(chain, wantChain) {
		t.Fatalf("checkpoint chain = %v, want %v", chain, wantChain)
	}

	// Assertion 2: a machine holding only the recorded chain satisfies
	// the simulator, so the simulator demands no row the run left
	// unconsumed.
	links := chainLinks(complete.Initial(), chain)
	var chainRows []machine.Transition
	for _, l := range links {
		chainRows = append(chainRows, tr(string(l.from), string(l.to), fmt.Sprintf("%s-to-%s", l.from, l.to)))
	}
	assertMatrixPasses(t, plan, mustMachine(t, complete.Initial(), chainRows...))

	// Assertion 3: dropping any one recorded row fails the simulator,
	// so the simulator demands every row the run consumed. The unit
	// label is the load-bearing half: "panelA panelB" pins that the
	// simulator attributes the wave's row to the wave, not to a member.
	wantUnit := map[statusPair]string{
		{"queued", "sx"}:       "root",
		{"sx", "sy"}:           "root2",
		{"sy", "gathered"}:     "panelA panelB",
		{"gathered", "routed"}: "router",
		{"routed", "done"}:     "finish",
	}
	if len(wantUnit) != len(links) {
		t.Fatalf("chain has %d links, want %d labelled", len(links), len(wantUnit))
	}
	for _, l := range links {
		unit, ok := wantUnit[l]
		if !ok {
			t.Fatalf("chain link %v has no expected unit label", l)
		}
		t.Run(fmt.Sprintf("drop %s to %s", l.from, l.to), func(t *testing.T) {
			assertMatrixFails(t, plan, eqCompleteMachine(t, l), string(l.from), string(l.to), unit)
		})
	}
}

// chainLinks turns an initial status and a recorded checkpoint chain
// into the ordered transition rows the run consumed.
func chainLinks(initial machine.Status, chain []machine.Status) []statusPair {
	links := make([]statusPair, 0, len(chain))
	prev := initial
	for _, s := range chain {
		links = append(links, statusPair{prev, s})
		prev = s
	}
	return links
}
