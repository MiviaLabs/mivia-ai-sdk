// Package mcp wraps the official Model Context Protocol Go SDK's
// client (github.com/modelcontextprotocol/go-sdk/mcp) and maps a
// remote MCP server's tools onto this module's tools.Tool and
// tools.Registry. Connect opens a session over a Transport; ListTools
// and CallTool map tools/list and tools/call; CallToolWithProgress
// correlates a notifications/progress stream to the call that
// requested it. See ../docs/plans/mcp.md for the design contract.
//
// Map: transport.go = Transport, NewStdioTransport,
// NewStreamableHTTPTransport; client.go = ClientInfo, ClientOptions,
// Client, Connect, Close, ErrClosed; progress.go = ProgressHandler and
// the per-call token correlation; tools.go = ListTools, CallTool,
// CallToolWithProgress, RegisterAll; content.go =
// ContentBlock, CallResult, and the mapping from the SDK's own result
// type. Contribution rules: ../AGENTS.md.
package mcp
