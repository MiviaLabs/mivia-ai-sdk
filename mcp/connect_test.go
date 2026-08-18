package mcp

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// failingTransport is a Transport whose Connect always fails, proving
// Connect returns an error, not a partial Client, when the handshake
// never starts.
type failingTransport struct{}

func (failingTransport) Connect(context.Context) (mcpsdk.Connection, error) {
	return nil, errors.New("fixture: transport refused to connect")
}

func TestConnectFailsOnTransportError(t *testing.T) {
	c, err := Connect(context.Background(), failingTransport{}, ClientOptions{})
	if err == nil {
		t.Fatal("Connect over a failing transport returned a nil error")
	}
	if c != nil {
		t.Fatalf("Connect over a failing transport returned a non-nil Client: %v", c)
	}
}

func TestCallToolWithProgressRejectsNilHandler(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})
	defer c.Close()

	_, err := c.CallToolWithProgress(context.Background(), "echo", nil, nil)
	if err == nil {
		t.Fatal("CallToolWithProgress with a nil onProgress returned a nil error")
	}
	if !errors.Is(err, ErrNilProgressHandler) {
		t.Fatalf("CallToolWithProgress error = %v, want errors.Is ErrNilProgressHandler", err)
	}
}
