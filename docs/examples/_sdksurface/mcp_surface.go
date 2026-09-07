package main

import (
	"fmt"
	"net/http"

	"github.com/MiviaLabs/mivia-ai-sdk/mcp"
)

// mcpSurface builds the two transports a client composition picks
// between: a subprocess speaking MCP over stdio pipes, and an HTTP
// endpoint speaking streamable HTTP. Both are lazy; neither dials
// here.
func mcpSurface() error {
	stdio := mcp.NewStdioTransport("mcp-server", "--stdio")
	if stdio == nil {
		return fmt.Errorf("NewStdioTransport returned nil")
	}
	httpTransport := mcp.NewStreamableHTTPTransport(
		"http://127.0.0.1:9/mcp", http.DefaultClient)
	if httpTransport == nil {
		return fmt.Errorf("NewStreamableHTTPTransport returned nil")
	}
	return nil
}
