package provider_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// FuzzMessageValidate feeds arbitrary Role, ToolCallID,
// ToolCalls-length, and ReasoningContent-presence values to
// Message.Validate. It must never panic, and the result must match
// the documented pairing rule: an unknown Role always wins over any
// ToolCallID, ToolCalls, or ReasoningContent state; a known Role then
// applies the ToolCallID rule, the ToolCalls rule, and, last, the
// ReasoningContent rule.
func FuzzMessageValidate(f *testing.F) {
	roles := []string{"", "user", "system", "assistant", "tool", "bogus", "call-1", "TOOL", " tool"}
	for _, role := range roles {
		for _, id := range roles {
			f.Add(role, id, 0, false)
		}
	}
	f.Add(string(provider.RoleSystem), "", 1, false)
	f.Add(string(provider.RoleUser), "", 1, false)
	f.Add(string(provider.RoleAssistant), "", 1, false)
	f.Add(string(provider.RoleTool), "call-1", 1, false)
	f.Add(string(provider.RoleTool), "call-1", 3, false)
	f.Add(string(provider.Role("bogus")), "", 1, false)
	f.Add(string(provider.RoleAssistant), "call-1", 2, false)
	f.Add(string(provider.RoleAssistant), "", 0, true)
	f.Add(string(provider.RoleSystem), "", 0, true)
	f.Add(string(provider.RoleUser), "", 0, true)
	f.Add(string(provider.RoleTool), "call-1", 0, true)
	f.Add(string(provider.RoleTool), "", 0, true)
	f.Add(string(provider.Role("bogus")), "", 0, true)
	f.Fuzz(func(t *testing.T, role, toolCallID string, toolCallsLen int, hasReasoningContent bool) {
		if toolCallsLen < 0 {
			toolCallsLen = 0
		}
		var reasoningContent string
		if hasReasoningContent {
			reasoningContent = "chain of thought"
		}
		m := provider.Message{
			Role:             provider.Role(role),
			ToolCallID:       toolCallID,
			ToolCalls:        make([]provider.ToolCall, toolCallsLen),
			ReasoningContent: reasoningContent,
		}
		err := m.Validate()
		checkMessageValidateOracle(t, role, toolCallID, toolCallsLen, hasReasoningContent, err)
	})
}

// checkMessageValidateOracle asserts FuzzMessageValidate's documented
// precedence: an unknown Role wins over every other state; a known
// Role then applies the ToolCallID rule, the ToolCalls rule, and,
// last, the ReasoningContent rule.
func checkMessageValidateOracle(t *testing.T, role, toolCallID string, toolCallsLen int, hasReasoningContent bool, err error) {
	t.Helper()
	known := role == string(provider.RoleSystem) || role == string(provider.RoleUser) ||
		role == string(provider.RoleAssistant) || role == string(provider.RoleTool)

	if !known {
		if !errors.Is(err, provider.ErrUnknownRole) {
			t.Fatalf("Validate() with unknown role %q = %v, want errors.Is ErrUnknownRole", role, err)
		}
		return
	}
	if role == string(provider.RoleTool) {
		checkToolRoleOracle(t, toolCallID, toolCallsLen, hasReasoningContent, err)
		return
	}
	if toolCallID != "" {
		if !errors.Is(err, provider.ErrToolCallIDUnexpected) {
			t.Fatalf("Validate() role %q with ToolCallID %q = %v, want errors.Is ErrToolCallIDUnexpected", role, toolCallID, err)
		}
		return
	}
	if role == string(provider.RoleAssistant) {
		if err != nil {
			t.Fatalf("Validate() RoleAssistant, ToolCalls len %d, hasReasoningContent %v = %v, want nil", toolCallsLen, hasReasoningContent, err)
		}
		return
	}
	// RoleSystem or RoleUser with an empty ToolCallID.
	if toolCallsLen > 0 {
		if !errors.Is(err, provider.ErrToolCallsUnexpected) {
			t.Fatalf("Validate() role %q with ToolCalls len %d = %v, want errors.Is ErrToolCallsUnexpected", role, toolCallsLen, err)
		}
		return
	}
	if hasReasoningContent {
		if !errors.Is(err, provider.ErrReasoningContentUnexpected) {
			t.Fatalf("Validate() role %q with ReasoningContent set = %v, want errors.Is ErrReasoningContentUnexpected", role, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Validate() role %q, empty ToolCallID = %v, want nil", role, err)
	}
}

// checkToolRoleOracle asserts the RoleTool branch of
// checkMessageValidateOracle: ToolCallID required, then ToolCalls
// rejected, then ReasoningContent rejected.
func checkToolRoleOracle(t *testing.T, toolCallID string, toolCallsLen int, hasReasoningContent bool, err error) {
	t.Helper()
	if toolCallID == "" {
		if !errors.Is(err, provider.ErrToolCallIDRequired) {
			t.Fatalf("Validate() RoleTool empty ToolCallID = %v, want errors.Is ErrToolCallIDRequired (the ToolCallID check runs first, before the ToolCalls check)", err)
		}
		return
	}
	if toolCallsLen > 0 {
		if !errors.Is(err, provider.ErrToolCallsUnexpected) {
			t.Fatalf("Validate() RoleTool with ToolCallID %q and ToolCalls len %d = %v, want errors.Is ErrToolCallsUnexpected", toolCallID, toolCallsLen, err)
		}
		return
	}
	if hasReasoningContent {
		if !errors.Is(err, provider.ErrReasoningContentUnexpected) {
			t.Fatalf("Validate() RoleTool with ReasoningContent set = %v, want errors.Is ErrReasoningContentUnexpected", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Validate() RoleTool with ToolCallID %q = %v, want nil", toolCallID, err)
	}
}

// FuzzChunkValidate feeds arbitrary Done/Err combinations to
// Chunk.Validate. It must never panic, and must return
// ErrChunkErrDoneConflict exactly when Err is non-nil and Done is
// true, nil otherwise.
func FuzzChunkValidate(f *testing.F) {
	f.Add(true, true)
	f.Add(true, false)
	f.Add(false, true)
	f.Add(false, false)
	f.Fuzz(func(t *testing.T, hasErr, done bool) {
		c := provider.Chunk{Done: done}
		if hasErr {
			c.Err = errors.New("fuzz error")
		}
		err := c.Validate()
		if hasErr && done {
			if !errors.Is(err, provider.ErrChunkErrDoneConflict) {
				t.Fatalf("Validate() Err set and Done = %v, want errors.Is ErrChunkErrDoneConflict", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Validate() hasErr=%v done=%v = %v, want nil", hasErr, done, err)
		}
	})
}
