package tools_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// profiledTool is a Tool that also implements ProfiledTool.
type profiledTool struct {
	stubTool
	profile tools.ExecutionProfile
}

func (p *profiledTool) ExecutionProfile() tools.ExecutionProfile { return p.profile }

// budgetedTool is a Tool that also implements ResultBudgetTool.
type budgetedTool struct {
	stubTool
	maxBytes int
}

func (b *budgetedTool) MaxResultBytes() int { return b.maxBytes }

// privilegedMarkerTool is a Tool that also implements PrivilegedTool.
type privilegedMarkerTool struct {
	stubTool
	privileged bool
}

func (p *privilegedMarkerTool) Privileged() bool { return p.privileged }

// TestExecutionProfileOf proves ExecutionProfileOf returns a
// ProfiledTool's published profile unchanged, and the zero profile
// for a tool that does not implement ProfiledTool.
func TestExecutionProfileOf(t *testing.T) {
	want := tools.ExecutionProfile{
		Class:       tools.ExecutionClassRead,
		ResourceKey: "res-1",
		Timeout:     5 * time.Second,
	}
	pt := &profiledTool{stubTool: stubTool{name: "profiled"}, profile: want}
	got := tools.ExecutionProfileOf(pt)
	if got != want {
		t.Fatalf("ExecutionProfileOf(profiled) = %+v, want %+v", got, want)
	}

	plain := &stubTool{name: "plain"}
	got = tools.ExecutionProfileOf(plain)
	zero := tools.ExecutionProfile{}
	if got != zero {
		t.Fatalf("ExecutionProfileOf(plain) = %+v, want zero value", got)
	}
	if got.Class != tools.ExecutionClassUnclassified {
		t.Fatalf("ExecutionProfileOf(plain).Class = %q, want ExecutionClassUnclassified", got.Class)
	}
}

// TestResultBudgetOf proves ResultBudgetOf returns a
// ResultBudgetTool's bound and true, and 0, false for a tool that
// does not implement ResultBudgetTool.
func TestResultBudgetOf(t *testing.T) {
	bt := &budgetedTool{stubTool: stubTool{name: "budgeted"}, maxBytes: 1024}
	gotBytes, gotOK := tools.ResultBudgetOf(bt)
	if gotBytes != 1024 || !gotOK {
		t.Fatalf("ResultBudgetOf(budgeted) = %d, %v, want 1024, true", gotBytes, gotOK)
	}

	plain := &stubTool{name: "plain"}
	gotBytes, gotOK = tools.ResultBudgetOf(plain)
	if gotBytes != 0 || gotOK {
		t.Fatalf("ResultBudgetOf(plain) = %d, %v, want 0, false", gotBytes, gotOK)
	}
}

// TestIsPrivileged proves IsPrivileged returns a PrivilegedTool's
// reported value, and false for a tool that does not implement
// PrivilegedTool.
func TestIsPrivileged(t *testing.T) {
	priv := &privilegedMarkerTool{stubTool: stubTool{name: "priv"}, privileged: true}
	if !tools.IsPrivileged(priv) {
		t.Fatalf("IsPrivileged(priv) = false, want true")
	}

	notPriv := &privilegedMarkerTool{stubTool: stubTool{name: "not-priv"}, privileged: false}
	if tools.IsPrivileged(notPriv) {
		t.Fatalf("IsPrivileged(not-priv) = true, want false")
	}

	plain := &stubTool{name: "plain"}
	if tools.IsPrivileged(plain) {
		t.Fatalf("IsPrivileged(plain) = true, want false")
	}
}

// TestExecutionClassValidate table-drives the accept/reject set for
// ExecutionClass.Validate.
func TestExecutionClassValidate(t *testing.T) {
	tests := []struct {
		name    string
		class   tools.ExecutionClass
		wantErr bool
	}{
		{"unclassified", tools.ExecutionClassUnclassified, false},
		{"read", tools.ExecutionClassRead, false},
		{"write", tools.ExecutionClassWrite, false},
		{"external", tools.ExecutionClassExternal, false},
		{"out-of-enum", tools.ExecutionClass("bogus"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.class.Validate()
			if tt.wantErr {
				if !errors.Is(err, tools.ErrInvalidExecutionClass) {
					t.Fatalf("Validate(%q) error = %v, want errors.Is ErrInvalidExecutionClass", tt.class, err)
				}
			} else if err != nil {
				t.Fatalf("Validate(%q) error = %v, want nil", tt.class, err)
			}
		})
	}
}

// TestExecutionProfileOfOutOfEnumClassPassesThrough proves an
// out-of-enum Class published by a ProfiledTool is returned unchanged
// by ExecutionProfileOf, and does not block RunScoped when the scope
// otherwise allows the tool.
func TestExecutionProfileOfOutOfEnumClassPassesThrough(t *testing.T) {
	badClass := tools.ExecutionClass("not-a-real-class")
	pt := &profiledTool{
		stubTool: stubTool{name: "weird", result: "ok"},
		profile:  tools.ExecutionProfile{Class: badClass},
	}
	got := tools.ExecutionProfileOf(pt)
	if got.Class != badClass {
		t.Fatalf("ExecutionProfileOf(weird).Class = %q, want %q", got.Class, badClass)
	}

	r := tools.New()
	if err := r.Add(pt); err != nil {
		t.Fatalf("Add(weird) error = %v, want nil", err)
	}
	out, err := r.RunScoped(context.Background(), "weird", tools.InOut{}, nil)
	if err != nil {
		t.Fatalf("RunScoped(weird) error = %v, want nil", err)
	}
	if out.Value != "ok" {
		t.Fatalf("RunScoped(weird).Value = %v, want ok", out.Value)
	}
}
