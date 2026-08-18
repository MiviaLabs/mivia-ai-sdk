package flow_test

// Red-green cases for flow.Step.PayloadFrom. See payload_integration_test.go
// for the end-to-end chain. The fixtures here share the run_test.go,
// loop_test.go, and routing_test.go helpers: statusStart, statusDone,
// statusMid, statusA, statusB, triggerGo, singleTransitionMachine,
// loopMachine, and mustOutcome.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestNewRejectsBothPayloadAndPayloadFrom proves flow.New rejects a
// step that sets both Payload and PayloadFrom, naming the step. One
// source of truth per step.
func TestNewRejectsBothPayloadAndPayloadFrom(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone), Payload: "static",
			PayloadFrom: func(machine.InOut) string { return "dyn" }},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `step "a"`) {
		t.Fatalf("error %q should name the step", err.Error())
	}
	if !strings.Contains(err.Error(), "both Payload and PayloadFrom") {
		t.Fatalf("error %q should name the both-set rule", err.Error())
	}
}

// TestNewAcceptsPayloadFromWithEmptyPayload proves flow.New admits a
// step with PayloadFrom and an empty Payload. An empty static payload
// is not a second source of truth.
func TestNewAcceptsPayloadFromWithEmptyPayload(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone),
			PayloadFrom: func(machine.InOut) string { return "dyn" }},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil Definition")
	}
}

// TestNewRejectsPayloadFromOnTwoMemberPanel proves flow.New rejects a
// PayloadFrom on a member of a panel with two or more members, naming
// the step and the panel. The rejection removes a silent no-op, because
// Run never calls Confirm for a multi-member wave.
func TestNewRejectsPayloadFromOnTwoMemberPanel(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "x", To: string(statusDone), PayloadFrom: func(machine.InOut) string { return "dyn" }},
		{ID: "y", To: string(statusDone)},
	}, []flow.Panel{{"x", "y"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `step "x"`) {
		t.Fatalf("error %q should name the step", err.Error())
	}
	if !strings.Contains(err.Error(), "panel") {
		t.Fatalf("error %q should name the panel", err.Error())
	}
}

// TestPayloadFromAloneResolvesIntoConfirm proves a PayloadFrom set
// alone reaches Confirm through the Step value, and the static Payload
// stays empty.
func TestPayloadFromAloneResolvesIntoConfirm(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone), PayloadFrom: func(machine.InOut) string { return "resolved" }},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	var got flow.Step
	confirm := func(ctx context.Context, step flow.Step) error {
		got = step
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Payload != "resolved" {
		t.Fatalf("Payload = %q, want %q", got.Payload, "resolved")
	}
	if got.PayloadFrom == nil {
		t.Fatal("PayloadFrom should stay set on the Step handed to Confirm")
	}
}

// TestNeitherSetPayloadByteIdentical proves a step with a static
// Payload and no PayloadFrom hands Confirm the value exactly as
// constructed. A nil PayloadFrom keeps today's behavior byte for byte.
func TestNeitherSetPayloadByteIdentical(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone), Payload: "fixed"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	var got flow.Step
	confirm := func(ctx context.Context, step flow.Step) error {
		got = step
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Payload != "fixed" {
		t.Fatalf("Payload = %q, want the constructed %q", got.Payload, "fixed")
	}
	if got.PayloadFrom != nil {
		t.Fatal("PayloadFrom should stay nil with neither field set")
	}
}

// TestPayloadResolvesAfterOwnFire proves a step's PayloadFrom sees the
// output its own transition action writes. Confirm runs after Fire, so
// the record the transition produced is the one PayloadFrom reads.
func TestPayloadResolvesAfterOwnFire(t *testing.T) {
	t.Parallel()
	m, err := machine.New(statusStart, machine.Transition{
		From: statusStart, To: statusDone, Trigger: triggerGo,
		OnEntry: func(_ context.Context, rec *machine.InOut) error {
			rec.Output = "marker"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone), PayloadFrom: outputReader},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var got flow.Step
	confirm := func(ctx context.Context, step flow.Step) error {
		got = step
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Payload != "marker" {
		t.Fatalf("Payload = %q, want output %q", got.Payload, "marker")
	}
}

// TestPayloadTwoStepChainReadsPriorOutput proves step two resolves step
// one's output from the record through a transition action. The record
// carries one's output into two's PayloadFrom.
func TestPayloadTwoStepChainReadsPriorOutput(t *testing.T) {
	t.Parallel()
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo,
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "o1"
				return nil
			}},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusMid)},
		{ID: "b", Needs: []string{"a"}, To: string(statusDone), PayloadFrom: outputReader},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	got := map[string]string{}
	confirm := func(ctx context.Context, step flow.Step) error {
		got[step.ID] = step.Payload
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got["b"] != "o1" {
		t.Fatalf("step b Payload = %q, want prior output %q", got["b"], "o1")
	}
	if got["a"] != "" {
		t.Fatalf("step a Payload = %q, want empty (no PayloadFrom)", got["a"])
	}
}

// TestPayloadChainedSubSeesFreshRecord proves a non-loop chained Sub's
// child starts from a fresh record. The child's PayloadFrom reads the
// fresh empty input, never the parent's live record. The parent's input
// seed stays invisible to the child.
func TestPayloadChainedSubSeesFreshRecord(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "c", To: string(statusDone),
			PayloadFrom: func(rec machine.InOut) string { return fmt.Sprintf("%v", rec.Input) }},
	}, nil)
	if err != nil {
		t.Fatalf("child flow.New: %v", err)
	}
	d, err := flow.New([]flow.Step{{ID: "p", Sub: child}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	got := map[string]string{}
	confirm := func(ctx context.Context, step flow.Step) error {
		got[step.ID] = step.Payload
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m,
		machine.InOut{Input: "seed-val"}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got["c"] != "<nil>" {
		t.Fatalf("child c Payload = %q, want the fresh nil record %q", got["c"], "<nil>")
	}
	if got["p"] != "" {
		t.Fatalf("parent p Payload = %q, want empty (no PayloadFrom)", got["p"])
	}
}

// TestPayloadLoopChildResolvesPerIteration proves a loop Sub's child
// resolves PayloadFrom per iteration against the record carried forward
// from the prior iteration. The branch step's enclosing fireFromChild
// increments the record output once per iteration, and the child step
// sees that count climb from zero.
func TestPayloadLoopChildResolvesPerIteration(t *testing.T) {
	t.Parallel()
	var parity int32
	var branchPayloads []string
	child, err := flow.New([]flow.Step{
		{ID: "branch", To: string(statusMid),
			PayloadFrom: func(rec machine.InOut) string {
				n, _ := rec.Output.(int)
				return fmt.Sprintf("out:%d", n)
			},
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if atomic.AddInt32(&parity, 1)%2 == 1 {
					return []string{"toA"}, nil
				}
				return []string{"toB"}, nil
			}},
		{ID: "toA", Needs: []string{"branch"}, To: string(statusA)},
		{ID: "toB", Needs: []string{"branch"}, To: string(statusB)},
	}, nil)
	if err != nil {
		t.Fatalf("child flow.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: child, Loop: &flow.LoopPolicy{Max: 3}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "branch" {
			branchPayloads = append(branchPayloads, step.Payload)
		}
		return nil
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"out:0", "out:1", "out:2"}
	if len(branchPayloads) != len(want) {
		t.Fatalf("branch payloads = %v, want %v", branchPayloads, want)
	}
	for i := range want {
		if branchPayloads[i] != want[i] {
			t.Fatalf("branch payloads = %v, want %v", branchPayloads, want)
		}
	}
}

// TestPayloadFallbackResolvesAfterOwnFire proves a fallback step
// admitted through a failed need resolves PayloadFrom against the
// record current after its own fire. Its transition writes the marker
// the step's own PayloadFrom then reads.
func TestPayloadFallbackResolvesAfterOwnFire(t *testing.T) {
	t.Parallel()
	const statusFb = machine.Status("fb")
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo,
			OnEntry: func(_ context.Context, _ *machine.InOut) error { return errors.New("boom") }},
		machine.Transition{From: statusStart, To: statusFb, Trigger: machine.Trigger("goFb"),
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "fb-out"
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone)},
		{ID: "fb", Needs: []string{"a"}, When: flow.AdmissionOnFailed, To: string(statusFb),
			PayloadFrom: outputReader},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var got string
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, func(ctx context.Context, step flow.Step) error {
		if step.ID == "fb" {
			got = step.Payload
		}
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "a", flow.OutcomeFailed)
	mustOutcome(t, report, "fb", flow.OutcomeSucceeded)
	if got != "fb-out" {
		t.Fatalf("fallback Payload = %q, want after-fire output %q", got, "fb-out")
	}
}

// TestPayloadResumeReResolvesAndDoneNeverReResolves proves a paused and
// resumed step re-resolves PayloadFrom on the restored record, while a
// step already in Done never re-resolves. The output counter shows the
// restored record reaches the resumed step.
func TestPayloadResumeReResolvesAndDoneNeverReResolves(t *testing.T) {
	t.Parallel()
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo,
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "a-out"
				return nil
			}},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusMid), PayloadFrom: outputReader},
		{ID: "b", Needs: []string{"a"}, To: string(statusDone), PayloadFrom: outputReader},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	counts := map[string]int{}
	payloads := map[string]string{}
	confirm := func(ctx context.Context, step flow.Step) error {
		counts[step.ID]++
		payloads[step.ID] = step.Payload
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	var cp flow.Checkpoint
	onCheckpoint := func(c flow.Checkpoint) {
		cp = c
		cancel()
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if counts["a"] != 1 || counts["b"] != 0 {
		t.Fatalf("after pause counts = %v, want a=1 b=0", counts)
	}
	if _, err := flow.Resume(context.Background(), d, m, cp, confirm, nil, nil); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if counts["a"] != 1 {
		t.Fatalf("step a PayloadFrom re-resolved after Resume: counts = %v", counts)
	}
	if counts["b"] != 1 {
		t.Fatalf("step b PayloadFrom resolved %d times, want 1", counts["b"])
	}
	if payloads["b"] != "a-out" {
		t.Fatalf("resumed b Payload = %q, want restored output %q", payloads["b"], "a-out")
	}
}

// TestPayloadOneMemberPanelResolves proves a one-member panel runs as a
// singleton step with a Confirm call, so its payload resolves like any
// gated step. New keeps the field on a one-member panel.
func TestPayloadOneMemberPanelResolves(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "x", To: string(statusDone), PayloadFrom: func(machine.InOut) string { return "one" }},
	}, []flow.Panel{{"x"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	var got flow.Step
	confirm := func(ctx context.Context, step flow.Step) error {
		got = step
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "x", flow.OutcomeSucceeded)
	if got.Payload != "one" {
		t.Fatalf("Payload = %q, want %q", got.Payload, "one")
	}
}

// outputReader is a PayloadFrom that reads the record's Output as a
// string.
func outputReader(rec machine.InOut) string {
	s, _ := rec.Output.(string)
	return s
}
