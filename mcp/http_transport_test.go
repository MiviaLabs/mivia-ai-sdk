package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newFixtureHTTPServer serves server over a loopback httptest.Server
// using the SDK's own streamable HTTP handler, and registers cleanup.
func newFixtureHTTPServer(t *testing.T, server *mcpsdk.Server) *httptest.Server {
	t.Helper()
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	}, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestStreamableHTTPTransportRoundTrip(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	httpServer := newFixtureHTTPServer(t, server)

	tr := NewStreamableHTTPTransport(httpServer.URL, nil)
	ctx := context.Background()
	c, err := Connect(ctx, tr, ClientOptions{Info: ClientInfo{Name: "test", Version: "v1"}})
	if err != nil {
		t.Fatalf("Connect over streamable HTTP: %v", err)
	}
	defer c.Close()

	toolList, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools over streamable HTTP: %v", err)
	}
	if len(toolList) != 1 || toolList[0].Name() != "echo" {
		t.Fatalf("ListTools over streamable HTTP = %v, want one tool named echo", toolList)
	}

	out, err := c.CallTool(ctx, "echo", map[string]any{"message": "over http"})
	if err != nil {
		t.Fatalf("CallTool over streamable HTTP: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok || len(result.Content) != 1 || result.Content[0].Text != "over http" {
		t.Fatalf("CallTool over streamable HTTP result = %#v, want one text block \"over http\"", out.Value)
	}
}

// TestStreamableHTTPTransportProgress proves a progress notification
// sent before a tool call's final result still reaches
// CallToolWithProgress's handler over a real HTTP round trip, not
// only the in-memory transport client_progress_test.go exercises.
func TestStreamableHTTPTransportProgress(t *testing.T) {
	server := newFixtureServer(nil)
	addProgressTool(server, "progress", 3)
	httpServer := newFixtureHTTPServer(t, server)

	tr := NewStreamableHTTPTransport(httpServer.URL, nil)
	ctx := context.Background()
	c, err := Connect(ctx, tr, ClientOptions{})
	if err != nil {
		t.Fatalf("Connect over streamable HTTP: %v", err)
	}
	defer c.Close()

	onProgress, wait := collectProgress(t, 3)
	if _, err := c.CallToolWithProgress(ctx, "progress", map[string]any{}, onProgress); err != nil {
		t.Fatalf("CallToolWithProgress over streamable HTTP: %v", err)
	}
	wait()
}

func TestStreamableHTTPTransportNilClientUsesDefault(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	httpServer := newFixtureHTTPServer(t, server)

	tr := NewStreamableHTTPTransport(httpServer.URL, nil)
	ctx := context.Background()
	c, err := Connect(ctx, tr, ClientOptions{})
	if err != nil {
		t.Fatalf("Connect with a nil httpClient: %v", err)
	}
	defer c.Close()

	if _, err := c.CallTool(ctx, "echo", map[string]any{"message": "ok"}); err != nil {
		t.Fatalf("CallTool with a nil httpClient: %v", err)
	}
}
