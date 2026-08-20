// Package spool_test exercises spool.SpoolTool: the tools.Tool
// wrapper half of the package. See spool_test.go for the Spool/Load
// grant-store tests and the shared fakeStore fixture.
package spool_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	wrapped, err := spool.SpoolTool("wrapper-name", 100, sp, stringTool{name: "inner", result: "short"})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	full := strings.Repeat("y", 1000)
	wrapped, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", result: full})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	atBound := strings.Repeat("a", 10)
	wrapped, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", result: atBound})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
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
	wrapped2, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", result: overBound})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
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

// TestSpoolToolClampsNegativeMaxBytes checks the constructor clamp: a
// negative maxBytes behaves exactly as zero. A non-empty result spools
// and returns the zero-budget view; an empty result passes through
// with no principal and no grant.
func TestSpoolToolClampsNegativeMaxBytes(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int
	}{
		{"negative", -1},
		{"zero", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			sp, err := spool.NewSpool(store, 1<<20)
			if err != nil {
				t.Fatalf("NewSpool: %v", err)
			}
			full := "hello"
			wrapped, err := spool.SpoolTool("t", tt.maxBytes, sp, stringTool{name: "inner", result: full})
			if err != nil {
				t.Fatalf("SpoolTool: %v", err)
			}
			ctx := spool.WithPrincipal(context.Background(), "alice")
			out, err := wrapped.Run(ctx, tools.InOut{})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			ref := refFor([]byte(full))
			if want := " [truncated, ref=" + ref + "]"; out.Value != want {
				t.Errorf("Out.Value = %v, want %q", out.Value, want)
			}
			got, err := store.Get(ref)
			if err != nil || string(got) != full {
				t.Errorf("store.Get(ref) = %q,%v, want the full inner result", got, err)
			}

			empty, err := spool.SpoolTool("t", tt.maxBytes, sp, stringTool{name: "inner", result: ""})
			if err != nil {
				t.Fatalf("SpoolTool: %v", err)
			}
			out2, err := empty.Run(context.Background(), tools.InOut{})
			if err != nil {
				t.Fatalf("Run over an empty result: %v", err)
			}
			if out2.Value != "" {
				t.Errorf("Out.Value = %v, want the empty result unchanged", out2.Value)
			}
		})
	}
}

// TestSpoolToolViewStaysValidUTF8 checks the rune-safe cut: a cut
// inside a multi-byte rune, and a cut over bytes that are not UTF-8 at
// all, both return a valid UTF-8 view. Spool.Load's store still holds
// every original byte.
func TestSpoolToolViewStaysValidUTF8(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		maxBytes int
	}{
		{"cut inside a rune", strings.Repeat("é", 10), 5},
		{"binary payload", string([]byte{0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			sp, err := spool.NewSpool(store, 1<<20)
			if err != nil {
				t.Fatalf("NewSpool: %v", err)
			}
			wrapped, err := spool.SpoolTool("t", tt.maxBytes, sp, stringTool{name: "inner", result: tt.result})
			if err != nil {
				t.Fatalf("SpoolTool: %v", err)
			}
			ctx := spool.WithPrincipal(context.Background(), "alice")
			out, err := wrapped.Run(ctx, tools.InOut{})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			view, ok := out.Value.(string)
			if !ok {
				t.Fatalf("Out.Value = %v, want a view string", out.Value)
			}
			if !utf8.ValidString(view) {
				t.Errorf("view = %q, want valid UTF-8", view)
			}
			ref := refFor([]byte(tt.result))
			if !strings.Contains(view, ref) {
				t.Errorf("view %q does not name ref %q", view, ref)
			}
			got, err := store.Get(ref)
			if err != nil || string(got) != tt.result {
				t.Errorf("store.Get(ref) = %q,%v, want the full inner result", got, err)
			}
		})
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
			sp, err := spool.NewSpool(store, 1<<20)
			if err != nil {
				t.Fatalf("NewSpool: %v", err)
			}
			wrapped, err := spool.SpoolTool("t", 100, sp, tt.inner)
			if err != nil {
				t.Fatalf("SpoolTool: %v", err)
			}

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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	small, err := spool.SpoolTool("t", 100, sp, stringTool{name: "inner", result: "short"})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
	large, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", result: strings.Repeat("z", 1000)})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
	ctx := context.Background()

	if _, err := small.Run(ctx, tools.InOut{}); err != nil {
		t.Errorf("small.Run with no principal err = %v, want nil (no grant needed)", err)
	}
	_, err = large.Run(ctx, tools.InOut{})
	if !errors.Is(err, spool.ErrNoPrincipal) {
		t.Errorf("large.Run with no principal err = %v, want ErrNoPrincipal", err)
	}
}

func TestSpoolToolInnerErrorPassesThrough(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	innerErr := errors.New("inner blew up")
	wrapped, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", errFail: innerErr})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
	_, err = wrapped.Run(context.Background(), tools.InOut{})
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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	wrapped, err := spool.SpoolTool("t", 10, sp, stringTool{name: "inner", result: strings.Repeat("q", 1000)})
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}
	ctx := spool.WithPrincipal(context.Background(), "alice")
	_, err = wrapped.Run(ctx, tools.InOut{})
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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	inner := profiledTool{stringTool: stringTool{name: "inner", result: "x"}}
	wrapped, err := spool.SpoolTool("t", 100, sp, inner)
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}

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
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	inner := stringTool{name: "inner", result: "x"}
	wrapped, err := spool.SpoolTool("t", 100, sp, inner)
	if err != nil {
		t.Fatalf("SpoolTool: %v", err)
	}

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
