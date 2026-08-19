// Package spool_test exercises spool.SpoolTool: the tools.Tool
// wrapper half of the package. See spool_test.go for the Spool/Load
// grant-store tests and the shared fakeStore fixture.
package spool_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stringTool returns a fixed string result, or errFail if set.
type stringTool struct {
	name    string
	result  string
	errFail error
}

func (s stringTool) Name() string { return s.name }

func (s stringTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if s.errFail != nil {
		return tools.Out{}, s.errFail
	}
	return tools.Out{Value: s.result}, nil
}

func TestSpoolToolPassesThroughSmallResult(t *testing.T) {
	store := newFakeStore()
	wrapped := spool.SpoolTool("wrapper-name", 100, store, stringTool{name: "inner", result: "short"})
	if wrapped.Name() != "wrapper-name" {
		t.Errorf("Name() = %q, want the wrapper's own name", wrapped.Name())
	}
	ctx := context.Background()
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "short" {
		t.Errorf("Out.Value = %v, want unchanged inner result", out.Value)
	}
}

func TestSpoolToolTruncatesLargeResult(t *testing.T) {
	store := newFakeStore()
	full := strings.Repeat("y", 1000)
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: full})
	ctx := spool.WithPrincipal(context.Background(), "alice")
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	view, ok := out.Value.(string)
	if !ok || len(view) >= len(full) {
		t.Fatalf("Out.Value = %v, want a truncated view", out.Value)
	}
	ref := refFor([]byte(full))
	if !strings.Contains(view, ref) {
		t.Errorf("view %q does not name ref %q", view, ref)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != full {
		t.Errorf("store.Get(ref) = %q,%v, want the full inner result", got, err)
	}
}

func TestSpoolToolExactMaxBytesBoundary(t *testing.T) {
	store := newFakeStore()
	atBound := strings.Repeat("a", 10)
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: atBound})
	ctx := context.Background()
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != atBound {
		t.Errorf("Out.Value = %v, want unchanged result exactly at maxBytes (no spool needed)", out.Value)
	}
	if store.putCalls != 0 {
		t.Errorf("store.putCalls = %d, want 0 at exact boundary", store.putCalls)
	}

	overBound := strings.Repeat("b", 11)
	wrapped2 := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: overBound})
	ctx2 := spool.WithPrincipal(context.Background(), "alice")
	out2, err := wrapped2.Run(ctx2, tools.InOut{})
	if err != nil {
		t.Fatalf("Run over bound: %v", err)
	}
	view, ok := out2.Value.(string)
	if !ok || view == overBound || !strings.HasPrefix(view, overBound[:10]) {
		t.Fatalf("Out.Value = %v, want a truncated view one byte over the boundary", out2.Value)
	}
}

// profiledOnlyTool, budgetOnlyTool, privilegedOnlyTool, and their
// two-way combinations each implement exactly the optional interfaces
// their name says, so a type assertion against the wrong one fails to
// compile if the wiring is wrong and fails at runtime (via panic) if
// SpoolTool ever calls a method a case should not have wired.

type profiledOnlyTool struct{ stringTool }

func (p profiledOnlyTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}

type budgetOnlyTool struct{ stringTool }

func (b budgetOnlyTool) MaxResultBytes() int { return 42 }

type privilegedOnlyTool struct{ stringTool }

func (p privilegedOnlyTool) Privileged() bool { return true }

type profiledBudgetTool struct{ stringTool }

func (t profiledBudgetTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}
func (t profiledBudgetTool) MaxResultBytes() int { return 42 }

type profiledPrivilegedTool struct{ stringTool }

func (t profiledPrivilegedTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}
func (t profiledPrivilegedTool) Privileged() bool { return true }

type budgetPrivilegedTool struct{ stringTool }

func (t budgetPrivilegedTool) MaxResultBytes() int { return 42 }
func (t budgetPrivilegedTool) Privileged() bool    { return true }

// TestSpoolToolPartialInterfaceCombinations exercises the 6 of 8
// SpoolTool wrapper-type switch branches TestSpoolToolForwardsOptionalInterfaces
// and TestSpoolToolNoOptionalInterfacesReportsUnimplemented leave
// untested: exactly one or exactly two of ProfiledTool, ResultBudgetTool,
// and PrivilegedTool. Each case asserts the declared interfaces forward
// inner's values and the undeclared ones report the not-implemented
// zero value, so a swapped capability struct in the switch fails.
func TestSpoolToolPartialInterfaceCombinations(t *testing.T) {
	tests := []struct {
		name  string
		inner tools.Tool
	}{
		{"profiledOnly", profiledOnlyTool{stringTool{name: "inner", result: "x"}}},
		{"budgetOnly", budgetOnlyTool{stringTool{name: "inner", result: "x"}}},
		{"privilegedOnly", privilegedOnlyTool{stringTool{name: "inner", result: "x"}}},
		{"profiledBudget", profiledBudgetTool{stringTool{name: "inner", result: "x"}}},
		{"profiledPrivileged", profiledPrivilegedTool{stringTool{name: "inner", result: "x"}}},
		{"budgetPrivileged", budgetPrivilegedTool{stringTool{name: "inner", result: "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			wrapped := spool.SpoolTool("t", 100, store, tt.inner)

			wantProfile := tools.ExecutionProfileOf(tt.inner)
			if gotProfile := tools.ExecutionProfileOf(wrapped); gotProfile != wantProfile {
				t.Errorf("ExecutionProfileOf(wrapper) = %+v, want %+v", gotProfile, wantProfile)
			}

			wantBudget, wantOK := tools.ResultBudgetOf(tt.inner)
			gotBudget, gotOK := tools.ResultBudgetOf(wrapped)
			if gotBudget != wantBudget || gotOK != wantOK {
				t.Errorf("ResultBudgetOf(wrapper) = %d,%v, want %d,%v", gotBudget, gotOK, wantBudget, wantOK)
			}

			if got, want := tools.IsPrivileged(wrapped), tools.IsPrivileged(tt.inner); got != want {
				t.Errorf("IsPrivileged(wrapper) = %v, want %v", got, want)
			}
		})
	}
}

func TestSpoolToolNoPrincipal(t *testing.T) {
	store := newFakeStore()
	small := spool.SpoolTool("t", 100, store, stringTool{name: "inner", result: "short"})
	large := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: strings.Repeat("z", 1000)})
	ctx := context.Background()

	if _, err := small.Run(ctx, tools.InOut{}); err != nil {
		t.Errorf("small.Run with no principal err = %v, want nil (no grant needed)", err)
	}
	_, err := large.Run(ctx, tools.InOut{})
	if !errors.Is(err, spool.ErrNoPrincipal) {
		t.Errorf("large.Run with no principal err = %v, want ErrNoPrincipal", err)
	}
}

func TestSpoolToolInnerErrorPassesThrough(t *testing.T) {
	store := newFakeStore()
	innerErr := errors.New("inner blew up")
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", errFail: innerErr})
	_, err := wrapped.Run(context.Background(), tools.InOut{})
	if !errors.Is(err, innerErr) {
		t.Errorf("Run err = %v, want inner error passed through", err)
	}
	if store.putCalls != 0 {
		t.Errorf("store.putCalls = %d, want 0 (no spooling on error)", store.putCalls)
	}
}

func TestSpoolToolStorePutFailure(t *testing.T) {
	store := newFakeStore()
	store.putFail = errors.New("store is full")
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: strings.Repeat("q", 1000)})
	ctx := spool.WithPrincipal(context.Background(), "alice")
	_, err := wrapped.Run(ctx, tools.InOut{})
	if !errors.Is(err, store.putFail) {
		t.Errorf("Run err = %v, want the store's Put failure", err)
	}
}

// profiledTool implements tools.ProfiledTool, tools.ResultBudgetTool,
// and tools.PrivilegedTool with fixed values.
type profiledTool struct {
	stringTool
}

func (p profiledTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}

func (p profiledTool) MaxResultBytes() int { return 42 }

func (p profiledTool) Privileged() bool { return true }

func TestSpoolToolForwardsOptionalInterfaces(t *testing.T) {
	store := newFakeStore()
	inner := profiledTool{stringTool: stringTool{name: "inner", result: "x"}}
	wrapped := spool.SpoolTool("t", 100, store, inner)

	wantProfile := tools.ExecutionProfileOf(inner)
	gotProfile := tools.ExecutionProfileOf(wrapped)
	if gotProfile != wantProfile {
		t.Errorf("ExecutionProfileOf(wrapper) = %+v, want %+v", gotProfile, wantProfile)
	}

	wantBudget, wantOK := tools.ResultBudgetOf(inner)
	gotBudget, gotOK := tools.ResultBudgetOf(wrapped)
	if gotBudget != wantBudget || gotOK != wantOK {
		t.Errorf("ResultBudgetOf(wrapper) = %d,%v, want %d,%v", gotBudget, gotOK, wantBudget, wantOK)
	}

	if tools.IsPrivileged(wrapped) != tools.IsPrivileged(inner) {
		t.Errorf("IsPrivileged(wrapper) = %v, want %v", tools.IsPrivileged(wrapped), tools.IsPrivileged(inner))
	}
}

func TestSpoolToolNoOptionalInterfacesReportsUnimplemented(t *testing.T) {
	store := newFakeStore()
	inner := stringTool{name: "inner", result: "x"}
	wrapped := spool.SpoolTool("t", 100, store, inner)

	gotBudget, gotOK := tools.ResultBudgetOf(wrapped)
	if gotOK {
		t.Errorf("ResultBudgetOf(wrapper over non-implementing inner) = %d,%v, want _,false", gotBudget, gotOK)
	}
	if gotBudget != 0 {
		t.Errorf("ResultBudgetOf(wrapper) budget = %d, want 0", gotBudget)
	}

	gotProfile := tools.ExecutionProfileOf(wrapped)
	if gotProfile != (tools.ExecutionProfile{}) {
		t.Errorf("ExecutionProfileOf(wrapper) = %+v, want zero value", gotProfile)
	}

	if tools.IsPrivileged(wrapped) {
		t.Errorf("IsPrivileged(wrapper) = true, want false")
	}
}
