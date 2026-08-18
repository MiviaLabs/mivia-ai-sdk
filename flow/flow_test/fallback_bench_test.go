package flow_test

// Baseline, measured on the phase 22 code before this phase landed,
// on a two-step graph with no fallback declared (risky, join), same
// hardware and run as below (AMD Ryzen 9 9900X, go test -bench,
// -benchtime=200000x): 214.1 ns/op, 480 B/op, 6 allocs/op.
//
// This phase's all-success benchmark runs the same graph shape plus
// the unused fallback step (risky, fallback, join), same hardware,
// same run. The failure-plus-fallback benchmark runs the same
// Definition, with a Guard that always rejects risky's transition, so
// every run takes the fallback path. Error wrapping and the context
// value injection (withFailure) vary with whether a run takes the
// fallback path, so this file reports the allocs/op ratio between the
// two benchmarks instead of a fixed allocation budget, matching
// PHASES.md's guidance for a route-closure-shaped variance.
//
// Measured (same hardware, same run, -benchtime=200000x):
//   BenchmarkFallbackAllSuccessPath:          339.2 ns/op,  544 B/op,  6 allocs/op
//   BenchmarkFallbackFailurePlusFallbackPath: 985.8 ns/op, 1225 B/op, 18 allocs/op
// Ratio: 2.91x time, 2.25x bytes, 3.00x allocs. The all-success path
// costs a little more than the phase 22 baseline (339.2 vs 214.1
// ns/op) because every admission check now runs one extra branch for
// AdmissionOnFailed, and the graph carries an unused third step.

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchFallbackGraph builds a three-step Definition (risky, fallback,
// join) and its matching machine Definition. The machine covers both
// the all-success path (risky succeeds, fallback skips, join fires
// from "r") and the failure-plus-fallback path (risky fails, fallback
// fires from "start", join fires from "f").
func benchFallbackGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR")},
		machine.Transition{From: machine.Status("r"), To: machine.Status("j"), Trigger: machine.Trigger("goJ1")},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("j"), Trigger: machine.Trigger("goJ2")},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// benchFallbackGraphFailing is benchFallbackGraph with risky's Guard
// always rejecting, so every run takes the fallback path.
func benchFallbackGraphFailing(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	riskyErr := errors.New("bench risky boom")
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: func(ctx context.Context) (bool, error) { return false, riskyErr }},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("j"), Trigger: machine.Trigger("goJ2")},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// BenchmarkFallbackAllSuccessPath measures Run on the three-step
// fallback graph when risky always succeeds: the fallback step skips.
func BenchmarkFallbackAllSuccessPath(b *testing.B) {
	d, m := benchFallbackGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkFallbackFailurePlusFallbackPath measures Run on the same
// graph when risky always fails: the fallback catches it and the run
// completes.
func BenchmarkFallbackFailurePlusFallbackPath(b *testing.B) {
	d, m := benchFallbackGraphFailing(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
