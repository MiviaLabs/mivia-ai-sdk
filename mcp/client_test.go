package mcp

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// echoArgs is the input type for the fixture "echo" tool. Message is
// optional so a fixture tool call can omit arguments entirely.
type echoArgs struct {
	Message string `json:"message,omitempty"`
}

// newFixtureServer builds an mcpsdk.Server named "fixture", with no
// tools registered yet.
func newFixtureServer(opts *mcpsdk.ServerOptions) *mcpsdk.Server {
	return mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "v0"}, opts)
}

// addEchoTool registers a tool that echoes in.Message back as one
// TextContent block.
func addEchoTool(server *mcpsdk.Server, name string) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: name, Description: name + " tool"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoArgs) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: in.Message}},
			}, nil, nil
		})
}

// addFailingTool registers a tool whose handler always returns an
// error, so the SDK's ToolHandlerFor wrapping sets IsError and puts
// the error text in Content.
func addFailingTool(server *mcpsdk.Server, name string) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: name, Description: name + " tool"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoArgs) (*mcpsdk.CallToolResult, any, error) {
			return nil, nil, errors.New("fixture: tool failure")
		})
}

// connectFixture connects server and this package's Client over a
// fresh pair of in-memory transports, and registers cleanup for both
// sessions.
func connectFixture(t testing.TB, server *mcpsdk.Server, opts ClientOptions) *Client {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	c, err := Connect(ctx, clientTransport, opts)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return c
}

func TestConnectReturnsNonNilClient(t *testing.T) {
	server := newFixtureServer(nil)
	c := connectFixture(t, server, ClientOptions{Info: ClientInfo{Name: "test", Version: "v1"}})
	if c == nil {
		t.Fatal("Connect returned a nil Client")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestListToolsMapsFixtureTools(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo1")
	addEchoTool(server, "echo2")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	ctx := context.Background()
	got, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListTools returned %d tools, want 2", len(got))
	}
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
		st, ok := tool.(SchemaTool)
		if !ok {
			t.Fatalf("tool %q does not implement SchemaTool", tool.Name())
		}
		if st.InputSchema() == nil {
			t.Fatalf("tool %q InputSchema() returned nil", tool.Name())
		}
	}
	if !names["echo1"] || !names["echo2"] {
		t.Fatalf("ListTools names = %v, want echo1 and echo2", names)
	}
}

func TestListToolsDrainsMultiplePages(t *testing.T) {
	server := newFixtureServer(&mcpsdk.ServerOptions{PageSize: 1})
	addEchoTool(server, "page1")
	addEchoTool(server, "page2")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	got, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListTools with PageSize 1 returned %d tools, want 2 across pages", len(got))
	}
}

// TestListToolsPropagatesIteratorError proves ListTools returns the
// error the SDK's own pagination iterator yields, instead of silently
// stopping and returning whatever tools were already collected. A
// wrong-but-passing implementation that ignored the iterator's error
// value (the "for t, err := range ...; ok, no err check" bug) would
// still pass every other ListTools test in this file, since none of
// them puts the server in a state where a page fetch fails.
func TestListToolsPropagatesIteratorError(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.ListTools(ctx)
	if err == nil {
		t.Fatal("ListTools with an already-canceled context returned a nil error")
	}
}

func TestCallToolTextResult(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	out, err := c.CallTool(context.Background(), "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok {
		t.Fatalf("out.Value is %T, want *CallResult", out.Value)
	}
	if result.IsError {
		t.Fatal("result.IsError = true, want false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("len(result.Content) = %d, want 1", len(result.Content))
	}
	block := result.Content[0]
	if block.Type != "text" {
		t.Fatalf("block.Type = %q, want text", block.Type)
	}
	if block.Text != "hello" {
		t.Fatalf("block.Text = %q, want hello", block.Text)
	}
}

func TestCallToolIsErrorResult(t *testing.T) {
	server := newFixtureServer(nil)
	addFailingTool(server, "fails")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	out, err := c.CallTool(context.Background(), "fails", map[string]any{"message": "x"})
	if err != nil {
		t.Fatalf("CallTool returned an error: %v, want nil (tool-level failure)", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok {
		t.Fatalf("out.Value is %T, want *CallResult", out.Value)
	}
	if !result.IsError {
		t.Fatal("result.IsError = false, want true")
	}
}

func TestCallToolUnknownName(t *testing.T) {
	server := newFixtureServer(nil)
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	_, err := c.CallTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("CallTool against an unregistered tool returned a nil error")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	server := newFixtureServer(nil)
	c := connectFixture(t, server, ClientOptions{})

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// errCloseConnection wraps a real Connection and returns wantErr from
// Close, after closing the real connection so the test does not leak.
type errCloseConnection struct {
	mcpsdk.Connection
	wantErr error
}

func (c *errCloseConnection) Close() error {
	_ = c.Connection.Close()
	return c.wantErr
}

// errCloseTransport wraps a Transport and returns an errCloseConnection
// around the real connection so the first session Close fails.
type errCloseTransport struct {
	mcpsdk.Transport
	wantErr error
}

func (t *errCloseTransport) Connect(ctx context.Context) (mcpsdk.Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &errCloseConnection{Connection: conn, wantErr: t.wantErr}, nil
}

// TestCloseIdempotentAfterError proves that only the Close call that
// actually closes the session returns its error; a second call returns
// nil.
func TestCloseIdempotentAfterError(t *testing.T) {
	server := newFixtureServer(nil)
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	wantErr := errors.New("fixture: close failed")
	wrapped := &errCloseTransport{Transport: clientTransport, wantErr: wantErr}
	c, err := Connect(context.Background(), wrapped, ClientOptions{})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := c.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("first Close: err = %v, want %v", err, wantErr)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: err = %v, want nil", err)
	}
}

func TestOperationsAfterCloseReturnErrClosed(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx := context.Background()
	if _, err := c.ListTools(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("ListTools after Close: err = %v, want ErrClosed", err)
	}
	if _, err := c.CallTool(ctx, "echo", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("CallTool after Close: err = %v, want ErrClosed", err)
	}
}

func TestRegisterAll(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	reg := tools.New()
	ctx := context.Background()
	if err := RegisterAll(ctx, c, reg); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	got, ok := reg.Get("echo")
	if !ok {
		t.Fatal("Registry.Get(\"echo\") reported false after RegisterAll")
	}
	if got.Name() != "echo" {
		t.Fatalf("got.Name() = %q, want echo", got.Name())
	}
	out, err := reg.Run(ctx, "echo", tools.InOut{Value: map[string]any{"message": "hi"}})
	if err != nil {
		t.Fatalf("Registry.Run: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok {
		t.Fatalf("out.Value is %T, want *CallResult", out.Value)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hi" {
		t.Fatalf("result.Content = %+v, want one text block \"hi\"", result.Content)
	}
}

func TestRegisterAllStopsOnListToolsError(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reg := tools.New()
	err := RegisterAll(context.Background(), c, reg)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("RegisterAll after Close: err = %v, want ErrClosed", err)
	}
}

func TestRegisterAllStopsOnAddError(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	reg := tools.New()
	if err := reg.Add(&mcpTool{client: c, descriptor: &mcpsdk.Tool{Name: "echo"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	err := RegisterAll(context.Background(), c, reg)
	if !errors.Is(err, tools.ErrDuplicateName) {
		t.Fatalf("RegisterAll with a name already registered: err = %v, want ErrDuplicateName", err)
	}
}
