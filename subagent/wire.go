// Wire-form helpers: turning a tool's typed result into the string
// every subagent tool's Run returns in tools.Out.Value.

package subagent

import "encoding/json"

// encodeToolResult JSON-encodes v into a tools.Out.Value string.
// WorkspaceListTool and WorkspaceStatTool call it so their Run
// results match every other subagent tool's string convention; see
// agentloop/wire.go's renderValue for the same string-first rule on
// the render side of this boundary.
func encodeToolResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
