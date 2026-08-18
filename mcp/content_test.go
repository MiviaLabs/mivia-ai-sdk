package mcp

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMapContentBlockText(t *testing.T) {
	block, err := mapContentBlock(&mcpsdk.TextContent{Text: "hi"})
	if err != nil {
		t.Fatalf("mapContentBlock: %v", err)
	}
	if block.Type != "text" || block.Text != "hi" {
		t.Fatalf("block = %+v, want type text, text hi", block)
	}
}

func TestMapContentBlockImage(t *testing.T) {
	block, err := mapContentBlock(&mcpsdk.ImageContent{Data: []byte("img"), MIMEType: "image/png"})
	if err != nil {
		t.Fatalf("mapContentBlock: %v", err)
	}
	if block.Type != "image" || string(block.Data) != "img" || block.MimeType != "image/png" {
		t.Fatalf("block = %+v, want type image, data img, mimeType image/png", block)
	}
}

func TestMapContentBlockAudio(t *testing.T) {
	block, err := mapContentBlock(&mcpsdk.AudioContent{Data: []byte("snd"), MIMEType: "audio/wav"})
	if err != nil {
		t.Fatalf("mapContentBlock: %v", err)
	}
	if block.Type != "audio" || string(block.Data) != "snd" || block.MimeType != "audio/wav" {
		t.Fatalf("block = %+v, want type audio, data snd, mimeType audio/wav", block)
	}
}

func TestMapContentBlockResourceLinkFallsBackToRaw(t *testing.T) {
	block, err := mapContentBlock(&mcpsdk.ResourceLink{URI: "file:///a", Name: "a"})
	if err != nil {
		t.Fatalf("mapContentBlock: %v", err)
	}
	if block.Type != "resource_link" {
		t.Fatalf("block.Type = %q, want resource_link", block.Type)
	}
	if block.Text != "" || block.Data != nil {
		t.Fatalf("block = %+v, want Text and Data unset for a block this package does not decompose", block)
	}
	if len(block.Raw) == 0 {
		t.Fatal("block.Raw is empty, want the block's own JSON encoding")
	}
}

func TestMapContentBlockEmbeddedResourceFallsBackToRaw(t *testing.T) {
	block, err := mapContentBlock(&mcpsdk.EmbeddedResource{
		Resource: &mcpsdk.ResourceContents{URI: "file:///b", MIMEType: "text/plain"},
	})
	if err != nil {
		t.Fatalf("mapContentBlock: %v", err)
	}
	if block.Type != "resource" {
		t.Fatalf("block.Type = %q, want resource", block.Type)
	}
	if len(block.Raw) == 0 {
		t.Fatal("block.Raw is empty, want the block's own JSON encoding")
	}
}

func TestMapCallResultEmptyContent(t *testing.T) {
	got, err := mapCallResult(&mcpsdk.CallToolResult{})
	if err != nil {
		t.Fatalf("mapCallResult: %v", err)
	}
	if got == nil {
		t.Fatal("mapCallResult returned a nil *CallResult")
	}
	if len(got.Content) != 0 {
		t.Fatalf("len(got.Content) = %d, want 0", len(got.Content))
	}
	if got.IsError {
		t.Fatal("got.IsError = true, want false")
	}
}
