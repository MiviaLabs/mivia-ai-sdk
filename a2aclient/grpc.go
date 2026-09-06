package a2aclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	a2acore "github.com/a2aproject/a2a-go/a2a"
	a2asdk "github.com/a2aproject/a2a-go/a2aclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// grpcTransport implements transport over a2a-go's gRPC transport.
// It is the only production implementation of transport; New builds
// one for every Client. See docs/plans/a2aclient.md's design notes
// for why gRPC, and newFromTransport for the test substitute.
type grpcTransport struct {
	tr a2asdk.Transport
}

var _ transport = (*grpcTransport)(nil)

// ErrNoTask reports a Send call whose remote response was not a Task.
// Test with errors.Is.
var ErrNoTask = errors.New("a2aclient: send did not return a task")

// ErrNoResultMessage reports a Result call against a task that
// carries no status message and no history entry. Test with
// errors.Is.
var ErrNoResultMessage = errors.New("a2aclient: task carries no result message")

// ErrNoDataPart reports a Result call whose result message carries no
// DataPart. Test with errors.Is.
var ErrNoDataPart = errors.New("a2aclient: result message carries no data part")

// numPrefix marks a string that carries a JSON number the proto
// struct hop cannot hold in a float64. dataFromRaw encodes, and
// dataFromParts restores.
const numPrefix = "urn:mivia:json-number:"

// newGRPCTransport dials baseURL and wraps the resulting connection in
// a2a-go's gRPC transport. The dial is lazy (grpc.NewClient does not
// block), so a bad address surfaces on the first call, not here.
func newGRPCTransport(baseURL string) (*grpcTransport, error) {
	conn, err := grpc.NewClient(baseURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &grpcTransport{tr: a2asdk.NewGRPCTransport(conn)}, nil
}

// Send maps mapped onto an a2a-go message and calls SendMessage. The
// remote agent's response must be a Task; Send returns its id.
func (g *grpcTransport) Send(ctx context.Context, mapped a2a.Mapped) (string, error) {
	data, err := dataFromRaw(mapped.Part.Data)
	if err != nil {
		return "", err
	}
	msg := &a2acore.Message{
		ID:        a2acore.NewMessageID(),
		ContextID: mapped.ContextID,
		Role:      a2acore.MessageRoleUser,
		Parts:     a2acore.ContentParts{a2acore.DataPart{Data: data}},
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

// Result fetches the task named by taskID and maps its result message
// onto a Mapped value.
func (g *grpcTransport) Result(ctx context.Context, taskID string) (a2a.Mapped, error) {
	task, err := g.tr.GetTask(ctx, &a2acore.TaskQueryParams{ID: a2acore.TaskID(taskID)})
	if err != nil {
		return a2a.Mapped{}, err
	}
	msg := resultMessage(task)
	if msg == nil {
		return a2a.Mapped{}, ErrNoResultMessage
	}
	data, err := dataFromParts(msg.Parts)
	if err != nil {
		return a2a.Mapped{}, err
	}
	return a2a.Mapped{Part: a2a.Part{Data: data}, ContextID: task.ContextID, MessageID: msg.ID}, nil
}

// Close forwards to the underlying gRPC transport's teardown call.
func (g *grpcTransport) Close() error {
	return g.tr.Destroy()
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

// dataFromRaw unmarshals raw envelope JSON into the map[string]any
// shape a2a-go's DataPart carries. It decodes numbers as
// json.Number. A number float64 cannot hold exactly travels as a
// numPrefix string: the proto struct hop converts every value
// through float64 and would round it, and a rounded integer breaks
// the remote's signature check. Small numbers stay plain numbers.
func dataFromRaw(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	encodeInexactNumbers(m)
	return m, nil
}

// dataFromParts finds the first DataPart in parts and re-marshals its
// content back to raw JSON. It restores the numPrefix strings
// dataFromRaw encoded, so the re-marshaled bytes carry the same
// integer literals the sender signed.
func dataFromParts(parts a2acore.ContentParts) (json.RawMessage, error) {
	for _, p := range parts {
		if dp, ok := p.(a2acore.DataPart); ok {
			restoreNumbers(dp.Data)
			return json.Marshal(dp.Data)
		}
	}
	return nil, ErrNoDataPart
}

// encodeInexactNumbers walks v in place. Each json.Number whose
// literal does not survive a float64 round trip becomes a numPrefix
// string; exactly representable numbers keep their type.
func encodeInexactNumbers(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			if n, ok := e.(json.Number); ok && !float64Exact(n.String()) {
				t[k] = numPrefix + n.String()
			} else {
				encodeInexactNumbers(e)
			}
		}
	case []any:
		for i, e := range t {
			if n, ok := e.(json.Number); ok && !float64Exact(n.String()) {
				t[i] = numPrefix + n.String()
			} else {
				encodeInexactNumbers(e)
			}
		}
	}
}

// float64Exact reports whether the JSON number literal s parses to a
// float64 whose shortest exact decimal form equals s.
func float64Exact(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	return strconv.FormatFloat(f, 'f', -1, 64) == s
}

// restoreNumbers walks v in place. Each numPrefix string becomes the
// json.Number it carries. A structpb hop delivers maps and slices as
// fresh values, so the walk mutates the transport-local copy.
func restoreNumbers(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			if s, ok := e.(string); ok && strings.HasPrefix(s, numPrefix) {
				t[k] = json.Number(strings.TrimPrefix(s, numPrefix))
			} else {
				restoreNumbers(e)
			}
		}
	case []any:
		for i, e := range t {
			if s, ok := e.(string); ok && strings.HasPrefix(s, numPrefix) {
				t[i] = json.Number(strings.TrimPrefix(s, numPrefix))
			} else {
				restoreNumbers(e)
			}
		}
	}
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
