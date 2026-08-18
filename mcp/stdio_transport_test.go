package mcp

import (
	"context"
	"os"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpStdioServerEnv, when set in the test binary's own environment,
// tells TestMain to run a fixture MCP server over stdio instead of
// running the test suite. stdioServerMain re-executes this same test
// binary with that variable set, the self-exec pattern os/exec's own
// tests use, so NewStdioTransport starts a real subprocess speaking
// real MCP over real pipes.
const mcpStdioServerEnv = "MIVIA_MCP_STDIO_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(mcpStdioServerEnv) == "1" {
		runStdioServer()
		return
	}
	os.Exit(m.Run())
}

// runStdioServer runs a fixture server with one echo tool over
// mcpsdk.StdioTransport, blocking until the client side closes the
// connection.
func runStdioServer() {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	_ = server.Run(context.Background(), &mcpsdk.StdioTransport{})
}

// selfExecTransport builds a Transport that starts this same test
// binary as a subprocess, re-executed with mcpStdioServerEnv set, so
// it runs runStdioServer instead of the test suite.
func selfExecTransport(t *testing.T) Transport {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tr := NewStdioTransport(exe)
	cmd, ok := tr.(*mcpsdk.CommandTransport)
	if !ok {
		t.Fatalf("NewStdioTransport returned %T, want *mcpsdk.CommandTransport", tr)
	}
	cmd.Command.Env = append(os.Environ(), mcpStdioServerEnv+"=1")
	return tr
}

func TestStdioTransportRoundTrip(t *testing.T) {
	tr := selfExecTransport(t)
	ctx := context.Background()
	c, err := Connect(ctx, tr, ClientOptions{Info: ClientInfo{Name: "test", Version: "v1"}})
	if err != nil {
		t.Fatalf("Connect over stdio: %v", err)
	}

	toolList, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools over stdio: %v", err)
	}
	if len(toolList) != 1 || toolList[0].Name() != "echo" {
		t.Fatalf("ListTools over stdio = %v, want one tool named echo", toolList)
	}

	out, err := c.CallTool(ctx, "echo", map[string]any{"message": "over stdio"})
	if err != nil {
		t.Fatalf("CallTool over stdio: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok || len(result.Content) != 1 || result.Content[0].Text != "over stdio" {
		t.Fatalf("CallTool over stdio result = %#v, want one text block \"over stdio\"", out.Value)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStdioTransportCloseTerminatesSubprocess proves Close ends the
// subprocess NewStdioTransport started, not just the client-side
// pipes: the SDK's own CommandTransport waits for the subprocess to
// exit as part of Close, so a non-nil, exited ProcessState afterward
// proves termination.
func TestStdioTransportCloseTerminatesSubprocess(t *testing.T) {
	tr := selfExecTransport(t)
	cmd, ok := tr.(*mcpsdk.CommandTransport)
	if !ok {
		t.Fatalf("NewStdioTransport returned %T, want *mcpsdk.CommandTransport", tr)
	}

	ctx := context.Background()
	c, err := Connect(ctx, tr, ClientOptions{})
	if err != nil {
		t.Fatalf("Connect over stdio: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if cmd.Command.ProcessState == nil {
		t.Fatal("subprocess ProcessState is nil after Close, want a terminated process")
	}
	if !cmd.Command.ProcessState.Exited() {
		t.Fatal("subprocess did not exit after Close")
	}
}
