package agentloop

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestToolCallContextRoundTrip(t *testing.T) {
	call := provider.ToolCall{
		ID:        "call_123",
		Name:      "test_tool",
		Arguments: []byte(`{"param":"value"}`),
	}

	ctx := WithToolCall(context.Background(), call)
	got, ok := ToolCallFromContext(ctx)
	if !ok {
		t.Fatal("expected ToolCallFromContext to return true")
	}
	if got.ID != call.ID || got.Name != call.Name || string(got.Arguments) != string(call.Arguments) {
		t.Fatalf("got %+v, want %+v", got, call)
	}

	// Nil context or context without value
	if _, ok := ToolCallFromContext(nil); ok {
		t.Fatal("expected ToolCallFromContext(nil) to return false")
	}
	if _, ok := ToolCallFromContext(context.Background()); ok {
		t.Fatal("expected ToolCallFromContext(empty ctx) to return false")
	}
}

func TestToolCallKey(t *testing.T) {
	tests := []struct {
		name string
		call provider.ToolCall
		want string
	}{
		{
			name: "non-empty ID with name",
			call: provider.ToolCall{
				ID:   "call_abc123",
				Name: "read_file",
			},
			want: "call_abc123",
		},
		{
			name: "non-empty ID with empty name",
			call: provider.ToolCall{
				ID:   "call_xyz789",
				Name: "",
			},
			want: "call_xyz789",
		},
		{
			name: "empty ID with name fallback",
			call: provider.ToolCall{
				ID:   "",
				Name: "exec_command",
			},
			want: "exec_command",
		},
		{
			name: "both ID and name empty",
			call: provider.ToolCall{
				ID:   "",
				Name: "",
			},
			want: "",
		},
		{
			name: "parity vector: provider stream without ID delta",
			call: provider.ToolCall{
				Name:      "browser_click",
				Arguments: []byte(`{"x":10,"y":20}`),
			},
			want: "browser_click",
		},
		{
			name: "parity vector: full provider tool call with args and index",
			call: provider.ToolCall{
				Index:     2,
				ID:        "call_stream_42",
				Name:      "browser_click",
				Arguments: []byte(`{"x":10,"y":20}`),
			},
			want: "call_stream_42",
		},
		{
			name: "collapse hazard: non-empty ID with Name does not collapse to Name",
			call: provider.ToolCall{
				ID:   "call_a",
				Name: "tool",
			},
			want: "call_a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolCallKey(tc.call)
			if got != tc.want {
				t.Fatalf("ToolCallKey(%+v) = %q, want %q", tc.call, got, tc.want)
			}
		})
	}

	if withID, withoutID := ToolCallKey(provider.ToolCall{ID: "call_a", Name: "tool"}), ToolCallKey(provider.ToolCall{Name: "tool"}); withID == withoutID {
		t.Fatalf("collapse hazard: ToolCallKey with ID and without ID collided: %q == %q", withID, withoutID)
	}
}
