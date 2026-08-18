package mcp

import (
	"context"
	"strconv"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProgressHandler receives one progress notification for a call this
// package's Client made. token identifies the call the notification
// belongs to: CallToolWithProgress's caller supplies token's meaning
// only implicitly, by receiving it back on every notification for the
// call it started. message, progress, and total mirror the MCP
// notifications/progress fields; total is zero when the server does
// not report one.
type ProgressHandler func(ctx context.Context, token any, message string, progress, total float64)

// mintToken returns a fresh, opaque progress token for one call. The
// SDK's own SetProgressToken accepts only a string or an int-family
// value; a string survives the JSON round trip unchanged, so a
// notification's token compares equal to the token this method
// returned, letting handleProgress correlate it back to its call.
func (c *Client) mintToken() string {
	return strconv.FormatUint(c.tokenCounter.Add(1), 10)
}

// registerHandler makes token's notifications reach h. A registered
// handler stays reachable for the rest of the Client's life, not just
// the call's: the SDK dispatches an incoming notification to
// handleProgress on its own goroutine, independent of and not
// ordered against the goroutine unblocking CallTool's matching
// response, so a notification for token can still arrive after
// CallToolWithProgress has already returned to its caller. Since
// mintToken never repeats a token, an entry left behind after its
// call finishes reaches no future call; clearHandlers on Close drops
// them all at once, bounding the map's lifetime to the Client's own.
func (c *Client) registerHandler(token string, h ProgressHandler) {
	c.handlersMu.Lock()
	c.handlers[token] = h
	c.handlersMu.Unlock()
}

// clearHandlers drops every registered per-call handler. Close calls
// this once, since no further notification can reach a closed
// Client's session.
func (c *Client) clearHandlers() {
	c.handlersMu.Lock()
	c.handlers = make(map[string]ProgressHandler)
	c.handlersMu.Unlock()
}

// handleProgress is the session-wide notification handler Connect
// wires into the SDK Client. It looks up req's token in the per-call
// handler map first; a call started through CallToolWithProgress
// receives its notification there. Otherwise it falls back to
// ClientOptions.OnProgress, or drops the notification when neither is
// set.
func (c *Client) handleProgress(ctx context.Context, req *mcpsdk.ProgressNotificationClientRequest) {
	token, ok := req.Params.ProgressToken.(string)
	if !ok {
		return
	}
	c.handlersMu.RLock()
	h, found := c.handlers[token]
	c.handlersMu.RUnlock()
	if !found {
		h = c.opts.OnProgress
	}
	if h == nil {
		return
	}
	h(ctx, req.Params.ProgressToken, req.Params.Message, req.Params.Progress, req.Params.Total)
}
