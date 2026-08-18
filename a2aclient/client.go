// Package a2aclient sends an envelope.Message to a remote agent and
// polls task status and results, through the a2aproject/a2a-go client.
// Send creates the remote task; Status reads it; Result fetches the
// output and re-verifies the signature after the network hop. See
// docs/plans/a2aclient.md for the contract.
package a2aclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// transport is the remote operation set Client needs to run one task's
// lifecycle: send, poll, fetch, close. New builds the production
// transport, wrapping a2a-go's gRPC transport. newFromTransport injects
// a substitute transport instead of dialing a live network endpoint;
// this is the seam this package's own internal tests use for a
// recorded transcript, since sdk-standards.yml scopes the third-party-
// import exception to a2aclient/*.go and an external test package
// cannot import a2a-go directly. Both stay unexported: no caller
// outside this package's own tests needs them. See
// docs/plans/a2aclient.md's Verification section for the test seam.
type transport interface {
	// Send creates a new remote task carrying mapped and returns its
	// task id.
	Send(ctx context.Context, mapped a2a.Mapped) (taskID string, err error)
	// State reads the current state of the task named by taskID.
	State(ctx context.Context, taskID string) (State, error)
	// Result fetches the mapped output of the task named by taskID.
	Result(ctx context.Context, taskID string) (a2a.Mapped, error)
	// Close releases the transport's resources. Idempotent.
	Close() error
}

// Client sends envelope messages to one remote A2A agent and reads
// task status and results back. Client wraps the a2aproject/a2a-go
// client for one base URL. The caller owns the Client; a Client is
// safe for concurrent use by multiple goroutines.
type Client struct {
	baseURL   string
	transport transport

	closeOnce sync.Once
	closeErr  error
}

// New builds a Client that talks to the A2A agent at baseURL. New
// validates baseURL and opens the underlying a2a-go gRPC transport,
// which holds a persistent connection. It returns an error, not a
// partial Client, when baseURL is empty or the transport fails to
// open. The caller must call Close when done with the Client.
func New(baseURL string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("a2aclient: baseURL is required")
	}
	tr, err := newGRPCTransport(baseURL)
	if err != nil {
		return nil, fmt.Errorf("a2aclient: open transport: %w", err)
	}
	return &Client{baseURL: baseURL, transport: tr}, nil
}

// newFromTransport builds a Client around a caller-provided transport,
// skipping the network dial New performs. Production callers use New;
// this constructor exists for this package's own tests, which must
// not open a live network connection. It returns an error, not a
// partial Client, when baseURL is empty or tr is nil.
func newFromTransport(baseURL string, tr transport) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("a2aclient: baseURL is required")
	}
	if tr == nil {
		return nil, errors.New("a2aclient: transport is required")
	}
	return &Client{baseURL: baseURL, transport: tr}, nil
}

// Close releases the resources New opened. a2a-go's gRPC transport
// wraps a persistent grpc.ClientConn; Close forwards to the
// transport's own teardown call. Close is idempotent: a second call
// returns nil. A Client whose Close was never called leaks the
// underlying connection, the same way an unclosed net.Conn does.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.transport.Close()
	})
	return c.closeErr
}

// TaskHandle identifies one remote task started by Send. The caller
// passes it back into Status and Result to track the same task. The
// zero TaskHandle identifies no task; Status and Result reject it.
type TaskHandle struct {
	taskID string
}

// isZero reports whether h identifies no task.
func (h TaskHandle) isZero() bool {
	return h.taskID == ""
}

// Send maps msg to an A2A part through a2a.ToPart, then sends it to
// the remote agent as a new task. Send returns the TaskHandle
// identifying the created task. msg must already be signed; Send
// performs no signing of its own, matching a2a.ToPart's contract. A
// transport failure, a canceled ctx, or an expired ctx deadline
// returns an error and a zero TaskHandle, never a partial one.
func (c *Client) Send(ctx context.Context, msg envelope.Message) (TaskHandle, error) {
	if err := ctx.Err(); err != nil {
		return TaskHandle{}, err
	}
	mapped, err := a2a.ToPart(msg)
	if err != nil {
		return TaskHandle{}, err
	}
	taskID, err := c.transport.Send(ctx, mapped)
	if err != nil {
		return TaskHandle{}, err
	}
	if taskID == "" {
		return TaskHandle{}, errors.New("a2aclient: transport returned an empty task id")
	}
	return TaskHandle{taskID: taskID}, nil
}

// Status reads the current state of the task identified by h from
// the remote agent. Status rejects the zero TaskHandle with an error.
// A canceled ctx or an expired ctx deadline returns that ctx error,
// unwrapped, so the caller can distinguish it from a remote failure
// with errors.Is(err, context.Canceled) or context.DeadlineExceeded.
func (c *Client) Status(ctx context.Context, h TaskHandle) (State, error) {
	if h.isZero() {
		return StateUnspecified, errors.New("a2aclient: zero TaskHandle")
	}
	if err := ctx.Err(); err != nil {
		return StateUnspecified, err
	}
	return c.transport.State(ctx, h.taskID)
}

// Result fetches the output of the task identified by h and maps it
// back to an envelope.Message through a2a.FromPart. Result calls
// msg.VerifySignature on the mapped message before returning it: the
// signature must still verify after the remote hop. Result returns an
// error, not a partial Message, when the task is not yet in a
// terminal state, when FromPart fails, when the signature check
// fails, or when ctx is canceled or its deadline expires.
func (c *Client) Result(ctx context.Context, h TaskHandle) (envelope.Message, error) {
	if h.isZero() {
		return envelope.Message{}, errors.New("a2aclient: zero TaskHandle")
	}
	if err := ctx.Err(); err != nil {
		return envelope.Message{}, err
	}
	state, err := c.transport.State(ctx, h.taskID)
	if err != nil {
		return envelope.Message{}, err
	}
	if !state.terminal() {
		return envelope.Message{}, fmt.Errorf("a2aclient: task is %s, not terminal", state)
	}
	mapped, err := c.transport.Result(ctx, h.taskID)
	if err != nil {
		return envelope.Message{}, err
	}
	msg, err := a2a.FromPart(mapped)
	if err != nil {
		return envelope.Message{}, err
	}
	if err := msg.VerifySignature(); err != nil {
		return envelope.Message{}, fmt.Errorf("a2aclient: signature check failed: %w", err)
	}
	return msg, nil
}
