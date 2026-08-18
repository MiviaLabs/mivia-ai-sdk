package mcp

import (
	"net/http"
	"os/exec"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Transport is the connection a Client opens over. It is a type alias
// for the official MCP Go SDK's own Transport interface
// (mcpsdk.Transport). NewStdioTransport and NewStreamableHTTPTransport
// each build one of the SDK's shipped implementations; a caller
// needing an SDK transport this package does not wrap, such as the
// SDK's in-memory transport pair used in this package's own tests,
// builds it against the SDK directly and passes it to Connect
// unchanged.
type Transport = mcpsdk.Transport

// NewStdioTransport returns a Transport that starts name as a
// subprocess with args and speaks MCP's stdio wire form over the
// subprocess's stdin and stdout, through the SDK's own
// CommandTransport.
func NewStdioTransport(name string, args ...string) Transport {
	return &mcpsdk.CommandTransport{Command: exec.Command(name, args...)}
}

// NewStreamableHTTPTransport returns a Transport that speaks MCP's
// streamable HTTP transport against endpoint, through the SDK's own
// StreamableClientTransport. A nil httpClient uses
// http.DefaultClient. The standalone SSE stream stays enabled, at the
// SDK's own default, so a server-initiated progress notification
// reaches this package's progress handler regardless of which stream
// carries it.
func NewStreamableHTTPTransport(endpoint string, httpClient *http.Client) Transport {
	return &mcpsdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient}
}
