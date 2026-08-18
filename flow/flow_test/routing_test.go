package flow_test

// Red step: before phase 22 landed, Step had no Route field and Run
// had no admission or route-exclusion logic. Every case below either
// failed to compile (undefined field Route) or, once the field
// existed as a placeholder, ran every dependent unconditionally.
// admissionVerdict, applyRoute, and the New validations landed in
// flow/routing.go and flow/step.go; the cases below then passed.
//
// The New-validation cases live in routing_new_test.go, to keep this
// file at or below the 500-line structure cap.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// branchMachine builds a machine whose transitions cover a branch
// step and up to two direct dependents: start -> b (trigger goB),
// b -> l1 (trigger goL1), b -> l2 (trigger goL2).
func branchMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l1"), Trigger: machine.Trigger("goL1")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l2"), Trigger: machine.Trigger("goL2")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// keeping returns a Route that keeps exactly the named IDs.
func keeping(ids ...string) flow.Route {
	return func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
		return ids, nil
	}
}

// mustOutcome fails the test unless report resolved id with want.
func mustOutcome(t *testing.T, report flow.Report, id string, want flow.Outcome) {
	t.Helper()
	got, ok := report.Outcome(id)
	if !ok {
		t.Fatalf("step %q never resolved", id)
	}
	if got != want {
		t.Fatalf("step %q outcome = %v, want %v", id, got, want)
	}
}

// TestRouteKeepsOneDependentSkipsOther proves a branch route keeps
// one direct dependent and skips the other.
func TestRouteKeepsOneDependentSkipsOther(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping("left")},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "right", Needs: []string{"branch"}, To: "l2"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "branch", flow.OutcomeSucceeded)
	mustOutcome(t, report, "left", flow.OutcomeSucceeded)
	mustOutcome(t, report, "right", flow.OutcomeSkipped)
}

// TestRouteEmptyReturnSkipsAllDependents proves an empty Route return
// skips every direct dependent.
func TestRouteEmptyReturnSkipsAllDependents(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping()},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "right", Needs: []string{"branch"}, To: "l2"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "left", flow.OutcomeSkipped)
	mustOutcome(t, report, "right", flow.OutcomeSkipped)
}

// TestRouteDuplicateIDCollapsesToOneAdmission proves a duplicate ID
// in a Route return still admits that dependent exactly once, and
// still skips every other dependent.
func TestRouteDuplicateIDCollapsesToOneAdmission(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping("left", "left")},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "right", Needs: []string{"branch"}, To: "l2"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "left", flow.OutcomeSucceeded)
	mustOutcome(t, report, "right", flow.OutcomeSkipped)
}

// TestRouteNamingNonDependentAborts proves a Route return naming a
// step that is not a direct dependent aborts the run with the pinned
// message.
func TestRouteNamingNonDependentAborts(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping("nope")},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "branch": route named "nope", not a direct dependent`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if _, ok := report.Outcome("left"); ok {
		t.Fatal("\"left\" resolved despite the aborted route: applyRoute must mark no dependent on this error")
	}
}

// TestRouteErrorMarksBranchFailedAborts proves a Route error marks the
// branch step OutcomeFailed and aborts the run with the pinned wrap.
func TestRouteErrorMarksBranchFailedAborts(t *testing.T) {
	t.Parallel()
	routeErr := errors.New("route boom")
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
			return nil, routeErr
		}},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, routeErr) {
		t.Fatalf("error does not wrap the route error: %v", err)
	}
	want := `flow: step "branch": route: route boom`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	mustOutcome(t, report, "branch", flow.OutcomeFailed)
	if _, ok := report.Outcome("left"); ok {
		t.Fatal("\"left\" resolved despite the route error: applyRoute must mark no dependent on this error")
	}
}

// subscribeCount subscribes to StepCompletedEvent on bus and returns a
// function reporting the number of events received so far.
func subscribeCount(t *testing.T, bus *events.Bus) func() int {
	t.Helper()
	var mu sync.Mutex
	count := 0
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

// TestSkippedStepEmitsNoEventViaRouteOrAdmission proves neither a
// route-excluded step nor an admission-skipped step fires
// StepCompletedEvent; only the steps that actually run do.
func TestSkippedStepEmitsNoEventViaRouteOrAdmission(t *testing.T) {
	t.Parallel()
	bus := events.New()
	count := subscribeCount(t, bus)

	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping()},
		{ID: "routeSkipped", Needs: []string{"branch"}, To: "l1"},
		{ID: "admissionSkipped", Needs: []string{"routeSkipped"}, When: flow.AdmissionOnSucceeded, To: "l2"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, branchMachine(t), machine.InOut{}, noopConfirm, bus, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "routeSkipped", flow.OutcomeSkipped)
	mustOutcome(t, report, "admissionSkipped", flow.OutcomeSkipped)
	if got := count(); got != 1 {
		t.Fatalf("event count = %d, want 1 (only branch runs)", got)
	}
}

// TestSkippedPanelEmitsNoEvent proves a whole-panel skip fires no
// StepCompletedEvent for any member: the third skip producer, next to
// route exclusion and admission, covered above.
func TestSkippedPanelEmitsNoEvent(t *testing.T) {
	t.Parallel()
	bus := events.New()
	count := subscribeCount(t, bus)

	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping("upstream")},
		{ID: "upstream", Needs: []string{"branch"}, To: "u"},
		{ID: "sideSkip", Needs: []string{"branch"}, To: "s"},
		{ID: "panelA", Needs: []string{"upstream"}, To: "x"},
		{ID: "panelB", Needs: []string{"sideSkip"}, When: flow.AdmissionOnSucceeded, To: "x"},
	}, []flow.Panel{{"panelA", "panelB"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("u"), Trigger: machine.Trigger("goU")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, bus, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "sideSkip", flow.OutcomeSkipped)
	mustOutcome(t, report, "panelA", flow.OutcomeSkipped)
	mustOutcome(t, report, "panelB", flow.OutcomeSkipped)
	if got := count(); got != 2 {
		t.Fatalf("event count = %d, want 2 (branch and upstream run; sideSkip and the panel skip)", got)
	}
}

// TestRouteExclusionFinalDespiteSecondPendingParent proves a route
// exclusion skips a dependent at once, even while that dependent's
// other parent has not yet resolved, and the exclusion holds
// regardless of the other parent's later outcome.
func TestRouteExclusionFinalDespiteSecondPendingParent(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping()},
		{ID: "other", To: "o"},
		{ID: "dep", Needs: []string{"branch", "other"}, To: "d"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("o"), Trigger: machine.Trigger("goO")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "branch", flow.OutcomeSucceeded)
	mustOutcome(t, report, "other", flow.OutcomeSucceeded)
	mustOutcome(t, report, "dep", flow.OutcomeSkipped)
}

// TestDefaultAdmissionAdmitsSkippedNeed proves the zero-value
// AdmissionOnFinished admits a step whose need ended OutcomeSkipped.
func TestDefaultAdmissionAdmitsSkippedNeed(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping("left")},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "right", Needs: []string{"branch"}, To: "l2"},
		{ID: "join", Needs: []string{"right"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l1"), Trigger: machine.Trigger("goL1")},
		machine.Transition{From: machine.Status("l1"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "right", flow.OutcomeSkipped)
	mustOutcome(t, report, "join", flow.OutcomeSucceeded)
}

// TestAdmissionOnSucceededCascadeStopsAtDefaultAdmission proves two
// things about the admission cascade: AdmissionOnSucceeded propagates
// a skip across one hop, and the API-pinned default rule
// (AdmissionOnFinished admits OutcomeSucceeded or OutcomeSkipped)
// stops the cascade at the next hop rather than propagating it
// further. See the Admission doc comment in flow/step.go: default
// admission "admits" a skipped need; it does not skip in turn.
func TestAdmissionOnSucceededCascadeStopsAtDefaultAdmission(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping()},
		{ID: "hop1", Needs: []string{"branch"}, To: "h1"},
		{ID: "hop2", Needs: []string{"hop1"}, When: flow.AdmissionOnSucceeded, To: "h2"},
		{ID: "hop3", Needs: []string{"hop2"}, To: "h3"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("h3"), Trigger: machine.Trigger("goH3")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "hop1", flow.OutcomeSkipped)
	mustOutcome(t, report, "hop2", flow.OutcomeSkipped)
	mustOutcome(t, report, "hop3", flow.OutcomeSucceeded)
}

// TestPanelSkipsWholeGroupWhenOneMemberUnadmitted proves one
// unadmitted panel member skips every member of that panel.
func TestPanelSkipsWholeGroupWhenOneMemberUnadmitted(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: keeping()},
		{ID: "upstream", Needs: []string{"branch"}, To: "u"},
		{ID: "panelA", Needs: []string{"upstream"}, To: "x"},
		{ID: "panelB", Needs: []string{"upstream"}, When: flow.AdmissionOnSucceeded, To: "x"},
	}, []flow.Panel{{"panelA", "panelB"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("x"), Trigger: machine.Trigger("goX")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "upstream", flow.OutcomeSkipped)
	mustOutcome(t, report, "panelA", flow.OutcomeSkipped)
	mustOutcome(t, report, "panelB", flow.OutcomeSkipped)
}

// TestPanelSkipDecisionWaitsAcrossIterations proves a three-member
// panel's skip decision waits for every member's needs to reach
// terminal, even when two members resolve in one loop iteration and
// the third member's upstream step resolves only in a later
// iteration, before it skips the whole panel.
func TestPanelSkipDecisionWaitsAcrossIterations(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "fastRoot", To: "f"},
		{ID: "preGate", To: "p"},
		{ID: "m1", Needs: []string{"fastRoot"}, To: "x"},
		{ID: "m2", Needs: []string{"fastRoot"}, To: "x"},
		{ID: "gateBranch", Needs: []string{"preGate"}, To: "gb", Route: keeping()},
		{ID: "gate", Needs: []string{"gateBranch"}, To: "gd"},
		{ID: "m3", Needs: []string{"gate"}, When: flow.AdmissionOnSucceeded, To: "x"},
	}, []flow.Panel{{"m1", "m2", "m3"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("p"), Trigger: machine.Trigger("goP")},
		machine.Transition{From: machine.Status("p"), To: machine.Status("gb"), Trigger: machine.Trigger("goGB")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "gate", flow.OutcomeSkipped)
	mustOutcome(t, report, "m1", flow.OutcomeSkipped)
	mustOutcome(t, report, "m2", flow.OutcomeSkipped)
	mustOutcome(t, report, "m3", flow.OutcomeSkipped)
}

// TestRouteReceivesPostStepStatusAndRecord proves Route receives the
// branch step's post-fire status and record.
func TestRouteReceivesPostStepStatusAndRecord(t *testing.T) {
	t.Parallel()
	var gotStatus machine.Status
	var gotOutput any
	route := func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
		gotStatus = cur
		gotOutput = rec.Output
		return []string{"left"}, nil
	}
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: route},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "right", Needs: []string{"branch"}, To: "l2"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				rec.Output = "branch-ran"
				return nil
			}},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l1"), Trigger: machine.Trigger("goL1")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotStatus != machine.Status("b") {
		t.Fatalf("Route saw cur = %q, want %q", gotStatus, "b")
	}
	if gotOutput != "branch-ran" {
		t.Fatalf("Route saw rec.Output = %v, want %q", gotOutput, "branch-ran")
	}
}
