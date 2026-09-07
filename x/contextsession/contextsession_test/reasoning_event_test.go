package contextsession_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/x/contextsession"
	"github.com/MiviaLabs/mivia-ai-sdk/x/contextstate"
)

func TestIsReasoningEvent(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want bool
	}{
		{"reasoning kind", provider.ReasoningEventKind, true},
		{"message kind", "message", false},
		{"tool kind", "tool_call", false},
		{"empty kind", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := contextstate.SourceEvent{Kind: tc.kind}
			if got := contextsession.IsReasoningEvent(e); got != tc.want {
				t.Fatalf("IsReasoningEvent(%q) = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}
