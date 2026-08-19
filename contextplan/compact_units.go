package contextplan

import (
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// unit is one atomic selection block: either one RoleAssistant
// message that carries ToolCalls together with the contiguous RoleTool
// replies that directly follow it, or one single message. The unit
// ends at the first reply whose ToolCallID is not one of that
// assistant's call ids; that reply is its own single-message unit.
type unit struct {
	msgs []provider.Message
}

// buildUnits splits msgs into units, in original order.
func buildUnits(msgs []provider.Message) []unit {
	units := make([]unit, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			ids := callIDSet(m.ToolCalls)
			j := i + 1
			for j < len(msgs) && msgs[j].Role == provider.RoleTool && ids[msgs[j].ToolCallID] {
				j++
			}
			units = append(units, unit{msgs: msgs[i:j]})
			i = j
			continue
		}
		units = append(units, unit{msgs: msgs[i : i+1]})
		i++
	}
	return units
}

// callIDSet indexes one tool-call list by call id.
func callIDSet(calls []provider.ToolCall) map[string]bool {
	ids := make(map[string]bool, len(calls))
	for _, c := range calls {
		ids[c.ID] = true
	}
	return ids
}

// isAssistantTool reports whether the unit starts with an assistant
// message that carries ToolCalls.
func (u unit) isAssistantTool() bool {
	return len(u.msgs) > 0 && u.msgs[0].Role == provider.RoleAssistant &&
		len(u.msgs[0].ToolCalls) > 0
}

// isComplete reports whether every call id of the unit's assistant
// message has a reply directly after it.
func (u unit) isComplete() bool {
	if !u.isAssistantTool() {
		return false
	}
	ids := callIDSet(u.msgs[0].ToolCalls)
	for _, m := range u.msgs[1:] {
		if ids[m.ToolCallID] {
			delete(ids, m.ToolCallID)
		}
	}
	return len(ids) == 0
}

// hasName reports whether any message of the unit carries one of the
// preserved names.
func (u unit) hasName(preserve map[string]bool) bool {
	for _, m := range u.msgs {
		if preserve[m.Name] {
			return true
		}
	}
	return false
}

// hasRole reports whether any message of the unit carries role.
func (u unit) hasRole(role provider.Role) bool {
	for _, m := range u.msgs {
		if m.Role == role {
			return true
		}
	}
	return false
}

// selectMandatory marks the mandatory retention set: the system
// message at index zero when its role is RoleSystem, every unit with
// a preserved name, the latest RoleUser unit, and the latest complete
// assistant-plus-tool unit.
func selectMandatory(units []unit, msgs []provider.Message, preserveNames []string) []bool {
	selected := make([]bool, len(units))
	if msgs[0].Role == provider.RoleSystem {
		selected[0] = true
	}
	preserve := make(map[string]bool, len(preserveNames))
	for _, name := range preserveNames {
		preserve[name] = true
	}
	for ui, u := range units {
		if u.hasName(preserve) {
			selected[ui] = true
		}
	}
	for ui := len(units) - 1; ui >= 0; ui-- {
		if units[ui].hasRole(provider.RoleUser) {
			selected[ui] = true
			break
		}
	}
	for ui := len(units) - 1; ui >= 0; ui-- {
		if units[ui].isComplete() {
			selected[ui] = true
			break
		}
	}
	return selected
}
