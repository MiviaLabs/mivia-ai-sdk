// Package a2aack turns a remote A2A task round trip into the agent
// composition layer's AckWait. Wait resolves one gated step through a
// Remote: send, poll, result, verify, and ack. See docs/plans/a2aack.md
// for the contract.
package a2aack

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Options configures the AckWait poll loop.
type Options struct {
	Poll    time.Duration // between Status calls; required, positive
	Timeout time.Duration // whole-exchange deadline; at least Poll
}

// Validate checks that Poll is positive and Timeout covers at least one Poll.
func (o Options) Validate() error {
	if o.Poll <= 0 {
		return ErrNoPoll
	}
	if o.Timeout < o.Poll {
		return ErrShortTimeout
	}
	return nil
}

var (
	// ErrNoClient means Wait got a nil client.
	ErrNoClient = errors.New("a2aack: client is required")
	// ErrNoPoll means the poll interval is not positive.
	ErrNoPoll = errors.New("a2aack: poll interval must be positive")
	// ErrShortTimeout means the timeout does not cover one poll.
	ErrShortTimeout = errors.New("a2aack: timeout must cover one poll")
	// ErrRemoteFailed means the remote task ended failed or canceled.
	ErrRemoteFailed = errors.New("a2aack: remote task failed")
	// ErrTimeout means the exchange outran its deadline or ctx.
	ErrTimeout = errors.New("a2aack: remote task timed out")
)

// Remote is the remote-task round trip a2aack polls: send, status,
// result. *a2aclient.Client implements it.
type Remote interface {
	Send(ctx context.Context, msg envelope.Message) (a2aclient.TaskHandle, error)
	Status(ctx context.Context, h a2aclient.TaskHandle) (a2aclient.State, error)
	Result(ctx context.Context, h a2aclient.TaskHandle) (envelope.Message, error)
}

// Wait returns the AckWait that resolves one step through c. Wait
// validates c and opts before it returns the AckWait, never inside a
// poll tick. It returns (nil, ErrNoClient) for a nil c and (nil,
// opts.Validate()) for invalid options.
func Wait(c Remote, opts Options) (agent.AckWait, error) {
	if c == nil {
		return nil, ErrNoClient
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		deadlineCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		h, err := c.Send(deadlineCtx, msg)
		if err != nil {
			return envelope.Ack{}, timeoutWrap(err, a2aclient.StateUnspecified)
		}
		return poll(deadlineCtx, c, h, opts, msg)
	}, nil
}

// poll owns the Status ticker until a terminal state, then resolves
// the ack. It selects on the deadline ctx every tick, so cancellation
// never waits out a full interval.
func poll(ctx context.Context, c Remote, h a2aclient.TaskHandle, opts Options, msg envelope.Message) (envelope.Ack, error) {
	ticker := time.NewTicker(opts.Poll)
	defer ticker.Stop()
	last := a2aclient.StateUnspecified
	for {
		select {
		case <-ctx.Done():
			return envelope.Ack{}, fmt.Errorf("%w after %s", ErrTimeout, last)
		case <-ticker.C:
			state, err := c.Status(ctx, h)
			if err != nil {
				return envelope.Ack{}, timeoutWrap(err, last)
			}
			switch state {
			case a2aclient.StateCompleted:
				result, err := c.Result(ctx, h)
				if err != nil {
					return envelope.Ack{}, timeoutWrap(err, last)
				}
				return ackFromResult(msg, result)
			case a2aclient.StateFailed, a2aclient.StateCanceled:
				return envelope.Ack{}, fmt.Errorf("%w: %s", ErrRemoteFailed, state)
			default:
				last = state
			}
		}
	}
}

// ackFromResult verifies the result's signature and builds the
// confirmed ack that references msg. Only the result's Signer and
// Payload come from the remote; MessageID keys off msg.ID, the sent
// step's own id, not the server-minted result id.
func ackFromResult(msg, result envelope.Message) (envelope.Ack, error) {
	if err := result.VerifySignature(); err != nil {
		return envelope.Ack{}, fmt.Errorf("a2aack: result signature check failed: %w", err)
	}
	ack, err := envelope.NewAck(msg, result.Signer, result.Payload)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

// timeoutWrap maps an exchange context error onto the AckWait contract.
// A DeadlineExceeded or Canceled error from Send, Status, or Result
// becomes an ErrTimeout wrap carrying the last seen state; any other
// error propagates unwrapped, never as ErrTimeout or ErrRemoteFailed.
func timeoutWrap(err error, last a2aclient.State) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w after %s", ErrTimeout, last)
	}
	return err
}
