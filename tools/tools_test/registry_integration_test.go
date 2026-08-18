package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRegistryIntegration registers two tools, resolves each by name,
// runs one, proves a duplicate Add fails, proves an unknown name
// fails Run, and proves a removed tool fails Run the same way an
// unknown name does.
func TestRegistryIntegration(t *testing.T) {
	r := tools.New()
	echo := &stubTool{name: "echo", result: "echoed"}
	sum := &stubTool{name: "sum", result: 42}

	if err := r.Add(echo); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	if err := r.Add(sum); err != nil {
		t.Fatalf("Add(sum) error = %v, want nil", err)
	}

	if got, ok := r.Get("echo"); !ok || got != echo {
		t.Fatalf("Get(echo) = %v, %v, want the registered echo tool", got, ok)
	}
	if got, ok := r.Get("sum"); !ok || got != sum {
		t.Fatalf("Get(sum) = %v, %v, want the registered sum tool", got, ok)
	}

	out, err := r.Run(context.Background(), "sum", tools.InOut{Value: 21})
	if err != nil {
		t.Fatalf("Run(sum) error = %v, want nil", err)
	}
	if out.Value != 42 {
		t.Fatalf("Run(sum).Value = %v, want 42", out.Value)
	}

	if err := r.Add(&stubTool{name: "echo"}); !errors.Is(err, tools.ErrDuplicateName) {
		t.Fatalf("second Add(echo) error = %v, want ErrDuplicateName", err)
	}

	if _, err := r.Run(context.Background(), "missing", tools.InOut{}); !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("Run(missing) error = %v, want ErrUnknownName", err)
	}

	if _, err := r.Run(context.Background(), "echo", tools.InOut{}); err != nil {
		t.Fatalf("Run(echo) before removal error = %v, want nil", err)
	}
	if ok := r.Remove("echo"); !ok {
		t.Fatalf("Remove(echo) = false, want true")
	}
	if _, err := r.Run(context.Background(), "echo", tools.InOut{}); !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("Run(echo) after removal error = %v, want ErrUnknownName", err)
	}
}
