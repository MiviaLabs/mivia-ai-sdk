package tools_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunScopedBuildsToolCallForProfiledTool proves RunScoped builds
// a ToolCall with the resolved tool's name, the caller's In value
// unchanged, and ExecutionProfileOf(t) as Profile, for a tool that
// implements ProfiledTool.
func TestRunScopedBuildsToolCallForProfiledTool(t *testing.T) {
	wantProfile := tools.ExecutionProfile{Class: tools.ExecutionClassWrite, ResourceKey: "res-1"}
	pt := &profiledTool{
		stubTool: stubTool{name: "write-file", result: "written"},
		profile:  wantProfile,
	}
	r := tools.New()
	if err := r.Add(pt); err != nil {
		t.Fatalf("Add(write-file) error = %v, want nil", err)
	}

	var gotCall tools.ToolCall
	var called bool
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, call tools.ToolCall) (bool, error) {
			called = true
			gotCall = call
			return true, nil
		},
	})

	wantIn := tools.InOut{Value: "payload"}
	out, err := r.RunScoped(context.Background(), "write-file", wantIn, scope)
	if err != nil {
		t.Fatalf("RunScoped(write-file) error = %v, want nil", err)
	}
	if out.Value != "written" {
		t.Fatalf("RunScoped(write-file).Value = %v, want written", out.Value)
	}
	if !called {
		t.Fatalf("Approve was not called")
	}
	if gotCall.Name != "write-file" {
		t.Fatalf("ToolCall.Name = %q, want write-file", gotCall.Name)
	}
	if gotCall.In != wantIn {
		t.Fatalf("ToolCall.In = %+v, want %+v", gotCall.In, wantIn)
	}
	if gotCall.Profile != wantProfile {
		t.Fatalf("ToolCall.Profile = %+v, want %+v", gotCall.Profile, wantProfile)
	}
}

// TestRunScopedBuildsToolCallForUnprofiledTool proves RunScoped
// builds a ToolCall with the zero ExecutionProfile, Class ==
// ExecutionClassUnclassified, for a tool that does not implement
// ProfiledTool.
func TestRunScopedBuildsToolCallForUnprofiledTool(t *testing.T) {
	plain := &stubTool{name: "plain", result: "ok"}
	r := tools.New()
	if err := r.Add(plain); err != nil {
		t.Fatalf("Add(plain) error = %v, want nil", err)
	}

	var gotCall tools.ToolCall
	var called bool
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, call tools.ToolCall) (bool, error) {
			called = true
			gotCall = call
			return true, nil
		},
	})

	out, err := r.RunScoped(context.Background(), "plain", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(plain) error = %v, want nil", err)
	}
	if out.Value != "ok" {
		t.Fatalf("RunScoped(plain).Value = %v, want ok", out.Value)
	}
	if !called {
		t.Fatalf("Approve was not called")
	}
	if gotCall.Name != "plain" {
		t.Fatalf("ToolCall.Name = %q, want plain", gotCall.Name)
	}
	zero := tools.ExecutionProfile{}
	if gotCall.Profile != zero {
		t.Fatalf("ToolCall.Profile = %+v, want zero value", gotCall.Profile)
	}
	if gotCall.Profile.Class != tools.ExecutionClassUnclassified {
		t.Fatalf("ToolCall.Profile.Class = %q, want ExecutionClassUnclassified", gotCall.Profile.Class)
	}
}
