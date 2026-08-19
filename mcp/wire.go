package mcp

import "encoding/json"

// marshalSchema turns one MCP tool descriptor's input schema into the
// JSON bytes tools.SchemaTool.ParameterSchema publishes. ListTools
// calls it once per tool, since ParameterSchema has no error return.
func marshalSchema(v any) ([]byte, error) {
	return json.Marshal(v)
}

// unmarshalArguments decodes model-supplied argument bytes into the
// arguments value CallTool sends. See mcpTool.DecodeArguments.
func unmarshalArguments(raw []byte, out *map[string]any) error {
	return json.Unmarshal(raw, out)
}
