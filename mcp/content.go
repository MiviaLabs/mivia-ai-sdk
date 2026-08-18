package mcp

import (
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ContentBlock is one block of an MCP tool call result. Type names
// the block's kind: "text", "image", "audio", "resource_link",
// "resource", or another value this package does not model beyond
// Raw. Text carries a text block's content. Data carries an image or
// audio block's binary payload. MimeType names Data's media type.
// Raw carries the block's own JSON encoding for a kind this package
// models by Type alone, without further fields.
type ContentBlock struct {
	Type     string
	Text     string
	Data     []byte
	MimeType string
	Raw      json.RawMessage
}

// CallResult is the mapped result of one tools/call invocation.
// IsError reports a tool-level failure the server signaled through
// the result object; Content still carries whatever the server
// returned alongside that failure.
type CallResult struct {
	Content []ContentBlock
	IsError bool
}

// contentBlockType is the wire shape this package reads to recover a
// content block's own "type" field when its Go type is not one this
// package decomposes further.
type contentBlockType struct {
	Type string `json:"type"`
}

// mapContentBlock maps one SDK content value into a ContentBlock.
// Text, image, and audio blocks decompose into their typed fields; any
// other block, such as an EmbeddedResource or a ResourceLink, is kept
// as Raw, read from the block's own MarshalJSON method, with Type read
// back out of that same encoding.
func mapContentBlock(c mcpsdk.Content) (ContentBlock, error) {
	switch v := c.(type) {
	case *mcpsdk.TextContent:
		return ContentBlock{Type: "text", Text: v.Text}, nil
	case *mcpsdk.ImageContent:
		return ContentBlock{Type: "image", Data: v.Data, MimeType: v.MIMEType}, nil
	case *mcpsdk.AudioContent:
		return ContentBlock{Type: "audio", Data: v.Data, MimeType: v.MIMEType}, nil
	default:
		raw, err := c.MarshalJSON()
		if err != nil {
			return ContentBlock{}, err
		}
		var kind contentBlockType
		if err := json.Unmarshal(raw, &kind); err != nil {
			return ContentBlock{}, err
		}
		return ContentBlock{Type: kind.Type, Raw: json.RawMessage(raw)}, nil
	}
}

// mapCallResult maps the SDK's CallToolResult into a CallResult.
func mapCallResult(r *mcpsdk.CallToolResult) (*CallResult, error) {
	blocks := make([]ContentBlock, 0, len(r.Content))
	for _, c := range r.Content {
		block, err := mapContentBlock(c)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return &CallResult{Content: blocks, IsError: r.IsError}, nil
}
