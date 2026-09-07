package a2a

import (
	"context"
	"encoding/json"
	"errors"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	a2asdk "github.com/a2aproject/a2a-go/a2aclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// grpcTransport implements transport over a2a-go's gRPC transport.
// It is the only production implementation of transport; New builds
// one for every Client. See docs/plans/a2a.md's design notes
// for why gRPC, and newFromTransport for the test substitute.
type grpcTransport struct {
	tr a2asdk.Transport
}

var _ transport = (*grpcTransport)(nil)

// ErrNoTask reports a Send call whose remote response was not a Task.
// Test with errors.Is.
var ErrNoTask = errors.New("a2a: send did not return a task")

// ErrNoResultMessage reports a Result call against a task that
// carries no status message and no history entry. Test with
// errors.Is.
var ErrNoResultMessage = errors.New("a2a: task carries no result message")

// ErrNoTextPart reports a Result call whose result message carries
// no TextPart and no DataPart. Test with errors.Is.
var ErrNoTextPart = errors.New("a2a: result message carries no text part")

// newGRPCTransport dials baseURL with creds and wraps the resulting
// connection in a2a-go's gRPC transport. The dial is lazy
// (grpc.NewClient does not block), so a bad address surfaces on the
// first call, not here.
func newGRPCTransport(baseURL string, creds credentials.TransportCredentials) (*grpcTransport, error) {
	conn, err := grpc.NewClient(baseURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcTransport{tr: a2asdk.NewGRPCTransport(conn)}, nil
}

// Send maps mapped onto an a2a-go message and calls SendMessage. The
// remote agent's response must be a Task; Send returns its id. Send
// reads only Part.Text and parses no part content.
func (g *grpcTransport) Send(ctx context.Context, mapped Mapped) (string, error) {
	msg := &a2acore.Message{
		ID:        a2acore.NewMessageID(),
		ContextID: mapped.ContextID,
		Role:      a2acore.MessageRoleUser,
		Parts:     a2acore.ContentParts{a2acore.TextPart{Text: mapped.Part.Text}},
	}
	result, err := g.tr.SendMessage(ctx, &a2acore.MessageSendParams{Message: msg})
	if err != nil {
		return "", err
	}
	task, ok := result.(*a2acore.Task)
	if !ok || task == nil {
		return "", ErrNoTask
	}
	return string(task.ID), nil
}

// State fetches the task named by taskID and maps its a2a-go state
// onto a State. Every a2a-go TaskState has one State; an upstream
// value added after this mapping maps to StateUnspecified.
func (g *grpcTransport) State(ctx context.Context, taskID string) (State, error) {
	task, err := g.tr.GetTask(ctx, &a2acore.TaskQueryParams{ID: a2acore.TaskID(taskID)})
	if err != nil {
		return StateUnspecified, err
	}
	return stateFromTaskState(task.Status.State), nil
}

// Result performs one GetTask and maps the task's result message onto
// a Mapped value. It returns the task's state beside the part, so the
// caller needs no second fetch to gate on a terminal state.
func (g *grpcTransport) Result(ctx context.Context, taskID string) (Mapped, State, error) {
	task, err := g.tr.GetTask(ctx, &a2acore.TaskQueryParams{ID: a2acore.TaskID(taskID)})
	if err != nil {
		return Mapped{}, StateUnspecified, err
	}
	msg := resultMessage(task)
	if msg == nil {
		return Mapped{}, StateUnspecified, ErrNoResultMessage
	}
	part, err := mappedFromParts(msg.Parts)
	if err != nil {
		return Mapped{}, StateUnspecified, err
	}
	mapped := Mapped{Part: part, ContextID: task.ContextID, MessageID: msg.ID}
	return mapped, stateFromTaskState(task.Status.State), nil
}

// Close forwards to the underlying gRPC transport's teardown call.
func (g *grpcTransport) Close() error {
	return g.tr.Destroy()
}

// mappedFromParts maps parts onto a Part. The first TextPart fills
// Part.Text. With no TextPart present, the first DataPart re-marshals
// through json.Marshal into Part.Data, so FromPart's fallback decodes
// an old peer until v0.4.0. That re-marshal passes through float64
// and cannot restore the deleted numPrefix markers; a legacy marker
// string fails the closed decode, and the sender must upgrade within
// the window. See docs/plans/a2a.md's text-carrier addendum.
func mappedFromParts(parts a2acore.ContentParts) (Part, error) {
	var data a2acore.DataPart
	hasData := false
	for _, p := range parts {
		if tp, ok := p.(a2acore.TextPart); ok {
			return Part{Text: tp.Text}, nil
		}
		if dp, ok := p.(a2acore.DataPart); ok && !hasData {
			data, hasData = dp, true
		}
	}
	if !hasData {
		return Part{}, ErrNoTextPart
	}
	body, err := json.Marshal(data.Data)
	if err != nil {
		return Part{}, err
	}
	return Part{Data: body}, nil
}

// resultMessage picks the message that carries a task's result: the
// terminal status message if the agent set one, else the last history
// entry.
func resultMessage(task *a2acore.Task) *a2acore.Message {
	if task.Status.Message != nil {
		return task.Status.Message
	}
	if n := len(task.History); n > 0 {
		return task.History[n-1]
	}
	return nil
}

// stateFromTaskState maps an a2a-go TaskState onto a State. It names
// all ten upstream constants. The default covers an upstream value
// added after this mapping and reports StateUnspecified.
func stateFromTaskState(ts a2acore.TaskState) State {
	switch ts {
	case a2acore.TaskStateUnspecified:
		return StateUnspecified
	case a2acore.TaskStateSubmitted:
		return StateSubmitted
	case a2acore.TaskStateWorking:
		return StateWorking
	case a2acore.TaskStateCompleted:
		return StateCompleted
	case a2acore.TaskStateFailed:
		return StateFailed
	case a2acore.TaskStateCanceled:
		return StateCanceled
	case a2acore.TaskStateRejected:
		return StateRejected
	case a2acore.TaskStateAuthRequired:
		return StateAuthRequired
	case a2acore.TaskStateInputRequired:
		return StateInputRequired
	case a2acore.TaskStateUnknown:
		return StateUnknown
	default:
		return StateUnspecified
	}
}
