package contextplan

import (
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// IsReasoningEvent reports whether e.Kind == provider.ReasoningEventKind.
func IsReasoningEvent(e contextstate.SourceEvent) bool {
	return e.Kind == provider.ReasoningEventKind
}
