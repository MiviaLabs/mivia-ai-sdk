package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// SchemaTool is an optional interface a tools.Tool returned by
// ListTools implements. InputSchema returns the tool's input schema
// exactly as the server published it and the SDK decoded it, a
// map[string]any in the common case.
type SchemaTool interface {
	InputSchema() any
}

// mcpTool wraps one MCP tool descriptor and the Client it came from.
// Run calls back into the Client to run it.
type mcpTool struct {
	client     *Client
	descriptor *mcpsdk.Tool
}

var (
	_ tools.Tool = (*mcpTool)(nil)
	_ SchemaTool = (*mcpTool)(nil)
)

// Name returns the tool's registration name.
func (t *mcpTool) Name() string {
	return t.descriptor.Name
}

// Run calls the wrapped Client's CallTool with the descriptor's name
// and in.Value as the call's arguments.
func (t *mcpTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return t.client.CallTool(ctx, t.descriptor.Name, in.Value)
}

// InputSchema returns the tool's input schema exactly as the server
// published it.
func (t *mcpTool) InputSchema() any {
	return t.descriptor.InputSchema
}

// ListTools calls the server's tools/list method, draining every page
// through the SDK's own pagination, and maps each returned mcpsdk.Tool
// into a tools.Tool. Each returned tools.Tool calls back into c
// through CallTool when run, and implements SchemaTool.
func (c *Client) ListTools(ctx context.Context) ([]tools.Tool, error) {
	if c.isClosed() {
		return nil, ErrClosed
	}
	var out []tools.Tool
	for t, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		out = append(out, &mcpTool{client: c, descriptor: t})
	}
	return out, nil
}

// CallTool invokes name through the wrapped session's CallTool,
// sending args as the call's arguments value (any value the SDK can
// marshal to JSON; nil sends no arguments), and maps the result into
// a tools.Out wrapping a *CallResult. CallTool mints a progress token
// for the call; a notification for it reaches ClientOptions.OnProgress
// when set, and is otherwise dropped. CallTool returns a non-nil
// error only for a transport or protocol-level failure; a tool-level
// failure the server reports through the result's isError field
// surfaces as CallResult.IsError, not as a Go error. See the design
// note above.
func (c *Client) CallTool(ctx context.Context, name string, args any) (tools.Out, error) {
	return c.callTool(ctx, name, args, nil)
}

// CallToolWithProgress behaves like CallTool, except every progress
// notification for this specific call reaches onProgress, not
// ClientOptions.OnProgress, for the call's whole duration. onProgress
// must not be nil.
func (c *Client) CallToolWithProgress(ctx context.Context, name string, args any, onProgress ProgressHandler) (tools.Out, error) {
	if onProgress == nil {
		return tools.Out{}, errNilProgressHandler
	}
	return c.callTool(ctx, name, args, onProgress)
}

// callTool is the shared implementation behind CallTool and
// CallToolWithProgress. It always mints a token; onProgress, when
// non-nil, is registered under that token, so a concurrent call's
// notifications never mix with it. See registerHandler for why the
// registration outlives this call's own return.
func (c *Client) callTool(ctx context.Context, name string, args any, onProgress ProgressHandler) (tools.Out, error) {
	if c.isClosed() {
		return tools.Out{}, ErrClosed
	}
	token := c.mintToken()
	if onProgress != nil {
		c.registerHandler(token, onProgress)
	}
	params := &mcpsdk.CallToolParams{Name: name, Arguments: args}
	params.SetProgressToken(token)
	result, err := c.session.CallTool(ctx, params)
	if err != nil {
		return tools.Out{}, err
	}
	mapped, err := mapCallResult(result)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: mapped}, nil
}

// RegisterAll calls c.ListTools and adds every returned tools.Tool to
// reg through reg.Add. RegisterAll stops and returns the first error
// either call produces, leaving reg holding whichever tools were
// already added.
func RegisterAll(ctx context.Context, c *Client, reg *tools.Registry) error {
	ts, err := c.ListTools(ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if err := reg.Add(t); err != nil {
			return err
		}
	}
	return nil
}
