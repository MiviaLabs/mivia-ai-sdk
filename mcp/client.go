package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClientInfo names the caller to the MCP server during the initialize
// handshake Connect performs through the SDK.
type ClientInfo struct {
	Name    string
	Version string
}

// ClientOptions configures Connect.
type ClientOptions struct {
	// Info names this client to the server.
	Info ClientInfo
	// OnProgress, when non-nil, receives a progress notification for
	// any call this Client makes that has no more specific handler
	// registered through CallToolWithProgress. A call made through
	// the mapped tools.Tool returned by ListTools, or through
	// Registry.Run, only ever reaches this session-wide handler.
	OnProgress ProgressHandler
}

// ErrClosed is CallTool's, CallToolWithProgress's, and ListTools's
// error when the Client's Close already ran. Test with errors.Is.
var ErrClosed = errors.New("mcp: client is closed")

// ErrNilProgressHandler is CallToolWithProgress's error for a nil
// onProgress argument. Test with errors.Is.
var ErrNilProgressHandler = errors.New("mcp: onProgress must not be nil")

// Client is one connection to one MCP server, wrapping the official
// MCP Go SDK's Client and ClientSession. The caller owns the Client
// and must call Close when done with it. Client is safe for
// concurrent ListTools, CallTool, and CallToolWithProgress calls from
// multiple goroutines.
type Client struct {
	opts    ClientOptions
	session *mcpsdk.ClientSession

	tokenCounter atomic.Uint64

	handlersMu sync.RWMutex
	handlers   map[string]ProgressHandler

	closed    atomic.Bool
	closeOnce sync.Once
}

// Connect opens a Client over t: it builds an SDK Client configured
// with opts.Info and a progress-notification handler wired to this
// Client's per-call correlation, then calls the SDK's own Connect,
// which performs the MCP initialize handshake. Connect returns an
// error, not a partial Client, when the handshake fails.
func Connect(ctx context.Context, t Transport, opts ClientOptions) (*Client, error) {
	c := &Client{
		opts:     opts,
		handlers: make(map[string]ProgressHandler),
	}
	impl := &mcpsdk.Implementation{
		Name:    opts.Info.Name,
		Version: opts.Info.Version,
	}
	sdkClient := mcpsdk.NewClient(impl, &mcpsdk.ClientOptions{
		ProgressNotificationHandler: c.handleProgress,
	})
	session, err := sdkClient.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect: %w", err)
	}
	c.session = session
	return c, nil
}

// Close closes the underlying session. Close is idempotent: a second
// call returns nil.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		err = c.session.Close()
		c.clearHandlers()
	})
	return err
}

// isClosed reports whether Close already ran.
func (c *Client) isClosed() bool {
	return c.closed.Load()
}
