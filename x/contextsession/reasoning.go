package contextsession

import (
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/x/contextstate"
)

// IsReasoningEvent reports whether e.Kind == provider.ReasoningEventKind.
func IsReasoningEvent(e contextstate.SourceEvent) bool {
	return e.Kind == provider.ReasoningEventKind
}
