package anthropic

import "encoding/json"

func encodeRequestBody(body *anthropicRequest) ([]byte, error) {
	return json.Marshal(body)
}

// countTokensBody carries only the fields the count_tokens endpoint
// accepts; a full Messages body would add max_tokens and friends,
// which the endpoint may reject.
type countTokensBody struct {
	Model    string             `json:"model"`
	Messages []anthropicMessage `json:"messages"`
	System   []anthropicSystem  `json:"system,omitempty"`
	Tools    []anthropicTool    `json:"tools,omitempty"`
}

func encodeCountTokensBody(body countTokensBody) ([]byte, error) {
	return json.Marshal(body)
}
