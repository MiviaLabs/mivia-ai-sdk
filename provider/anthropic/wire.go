package anthropic

import "encoding/json"

func encodeRequestBody(body *anthropicRequest) ([]byte, error) {
	return json.Marshal(body)
}
