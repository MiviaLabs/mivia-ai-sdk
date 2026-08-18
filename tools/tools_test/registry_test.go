package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestAdd covers the nil, blank-name, and duplicate-name rejection
// cases plus the accept path.
func TestAdd(t *testing.T) {
	t.Run("nil tool", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(nil); !errors.Is(err, tools.ErrNilTool) {
			t.Fatalf("Add(nil) error = %v, want ErrNilTool", err)
		}
	})
	t.Run("empty name", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: ""}); !errors.Is(err, tools.ErrBlankName) {
			t.Fatalf("Add(empty name) error = %v, want ErrBlankName", err)
		}
	})
	t.Run("whitespace-only name", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "   "}); !errors.Is(err, tools.ErrBlankName) {
			t.Fatalf("Add(whitespace name) error = %v, want ErrBlankName", err)
		}
	})
	t.Run("new name accepted", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "echo"}); err != nil {
			t.Fatalf("Add(echo) error = %v, want nil", err)
		}
		got, ok := r.Get("echo")
		if !ok || got == nil {
			t.Fatalf("Get(echo) = %v, %v, want a non-nil Tool and true", got, ok)
		}
	})
	t.Run("duplicate name rejected", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "echo"}); err != nil {
			t.Fatalf("first Add(echo) error = %v, want nil", err)
		}
		if err := r.Add(&stubTool{name: "echo"}); !errors.Is(err, tools.ErrDuplicateName) {
			t.Fatalf("second Add(echo) error = %v, want ErrDuplicateName", err)
		}
	})
}

// TestGet covers the present and absent lookup paths.
func TestGet(t *testing.T) {
	r := tools.New()
	want := &stubTool{name: "echo", result: "hi"}
	if err := r.Add(want); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	t.Run("registered name", func(t *testing.T) {
		got, ok := r.Get("echo")
		if !ok {
			t.Fatalf("Get(echo) ok = false, want true")
		}
		if got != want {
			t.Fatalf("Get(echo) = %v, want the registered tool", got)
		}
	})
	t.Run("unknown name", func(t *testing.T) {
		got, ok := r.Get("missing")
		if ok || got != nil {
			t.Fatalf("Get(missing) = %v, %v, want nil, false", got, ok)
		}
	})
}

// TestRun covers the resolve-and-call path, error propagation from
// the tool, and the unknown-name failure.
func TestRun(t *testing.T) {
	t.Run("registered name returns tool result", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "echo", result: "hi"}); err != nil {
			t.Fatalf("Add(echo) error = %v, want nil", err)
		}
		out, err := r.Run(context.Background(), "echo", tools.InOut{Value: "in"})
		if err != nil {
			t.Fatalf("Run(echo) error = %v, want nil", err)
		}
		if out.Value != "hi" {
			t.Fatalf("Run(echo).Value = %v, want hi", out.Value)
		}
	})
	t.Run("tool error propagates unchanged", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "fails", failErr: errBoom}); err != nil {
			t.Fatalf("Add(fails) error = %v, want nil", err)
		}
		_, err := r.Run(context.Background(), "fails", tools.InOut{})
		if !errors.Is(err, errBoom) {
			t.Fatalf("Run(fails) error = %v, want errBoom", err)
		}
	})
	t.Run("unknown name", func(t *testing.T) {
		r := tools.New()
		_, err := r.Run(context.Background(), "missing", tools.InOut{})
		if !errors.Is(err, tools.ErrUnknownName) {
			t.Fatalf("Run(missing) error = %v, want ErrUnknownName", err)
		}
	})
}

// TestRemove covers the present, absent, and post-removal cases.
func TestRemove(t *testing.T) {
	t.Run("present name", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "echo"}); err != nil {
			t.Fatalf("Add(echo) error = %v, want nil", err)
		}
		if ok := r.Remove("echo"); !ok {
			t.Fatalf("Remove(echo) = false, want true")
		}
		if _, ok := r.Get("echo"); ok {
			t.Fatalf("Get(echo) ok = true after Remove, want false")
		}
		if _, err := r.Run(context.Background(), "echo", tools.InOut{}); !errors.Is(err, tools.ErrUnknownName) {
			t.Fatalf("Run(echo) after Remove error = %v, want ErrUnknownName", err)
		}
	})
	t.Run("absent name leaves registry unchanged", func(t *testing.T) {
		r := tools.New()
		if err := r.Add(&stubTool{name: "echo"}); err != nil {
			t.Fatalf("Add(echo) error = %v, want nil", err)
		}
		if ok := r.Remove("missing"); ok {
			t.Fatalf("Remove(missing) = true, want false")
		}
		if _, ok := r.Get("echo"); !ok {
			t.Fatalf("Get(echo) ok = false after Remove(missing), want true")
		}
	})
}
