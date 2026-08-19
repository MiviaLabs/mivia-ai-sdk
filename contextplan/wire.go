package contextplan

import (
	"encoding/json"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// This file holds Compact's canonical fingerprint bytes, the one
// place contextplan serializes JSON for hashing. See agentloop/wire.go
// for the same exclusion the marshal rule grants a wire file.

// fingerprintCall is one tool call inside one fingerprint record.
type fingerprintCall struct {
	ID        string `json:"id"`
	Arguments []byte `json:"arguments"`
}

// fingerprintMessage is one kept message inside the fingerprint.
type fingerprintMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCallID string            `json:"tool_call_id"`
	Name       string            `json:"name"`
	ToolCalls  []fingerprintCall `json:"tool_calls,omitempty"`
}

// fingerprintDoc is the canonical JSON fingerprint Mint hashes: the
// algorithm, the budget, both token thresholds, and one record per
// kept message. No map appears, so encoding/json output is
// deterministic.
type fingerprintDoc struct {
	Algorithm     string               `json:"algorithm"`
	Budget        int                  `json:"budget"`
	TriggerTokens int                  `json:"trigger_tokens"`
	TargetTokens  int                  `json:"target_tokens"`
	Messages      []fingerprintMessage `json:"messages"`
}

// compactionKey builds the idempotency key: CompactionAlgorithm, a
// colon, then contextstate.Mint over the canonical JSON fingerprint.
// Repeated Compact calls on equal inputs return equal keys.
func compactionKey(kept []provider.Message, budget, trigger, target int) string {
	doc := fingerprintDoc{
		Algorithm:     CompactionAlgorithm,
		Budget:        budget,
		TriggerTokens: trigger,
		TargetTokens:  target,
		Messages:      make([]fingerprintMessage, 0, len(kept)),
	}
	for _, m := range kept {
		record := fingerprintMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, c := range m.ToolCalls {
			record.ToolCalls = append(record.ToolCalls,
				fingerprintCall{ID: c.ID, Arguments: c.Arguments})
		}
		doc.Messages = append(doc.Messages, record)
	}
	// The document holds only strings, ints, and byte slices, so
	// Marshal cannot fail; a nil payload keeps the key well-formed.
	payload, _ := json.Marshal(doc)
	return CompactionAlgorithm + ":" + contextstate.Mint(payload)
}
