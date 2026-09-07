package agentloop

import (
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// isEmptyAssistantTurn reports whether m is the shape-empty assistant
// turn: RoleAssistant, Content blank after TrimSpace, zero ToolCalls,
// and zero ReasoningBlocks. It mirrors the sibling consumer's
// DropEmptyAssistantTurns predicate in
// mivia-agent/internal/provider/api_message.go, adapted to this
// module's provider.Message: ReasoningBlocks here, ReasoningContent
// there.
func isEmptyAssistantTurn(m provider.Message) bool {
	return m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) == "" &&
		len(m.ToolCalls) == 0 && len(m.ReasoningBlocks) == 0
}

// dropEmptyAssistantTurns returns a filtered copy when msgs holds any
// empty assistant turn, and msgs unchanged otherwise. The invariant:
// the loop never carries a shape-empty assistant turn into planning or
// a request. The dropped turns carry no reasoning blocks by
// definition, so the caller never sets DisableProviderReplay for this
// filter alone.
func dropEmptyAssistantTurns(msgs []provider.Message) []provider.Message {
	needsWork := false
	for _, m := range msgs {
		if isEmptyAssistantTurn(m) {
			needsWork = true
			break
		}
	}
	if !needsWork {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		if isEmptyAssistantTurn(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}
