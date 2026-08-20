package a2aclient

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	a2acore "github.com/a2aproject/a2a-go/a2a"
	a2asdk "github.com/a2aproject/a2a-go/a2aclient"
)

// fakeSDKTransport implements a2asdk.Transport for the subset of
// methods grpcTransport calls. Embedding the nil interface satisfies
// the rest of the method set without a live network call.
type fakeSDKTransport struct {
	a2asdk.Transport

	sendResp   a2acore.SendMessageResult
	sendErr    error
	taskResp   *a2acore.Task
	taskErr    error
	destroyErr error
}

func (f *fakeSDKTransport) SendMessage(ctx context.Context, m *a2acore.MessageSendParams) (a2acore.SendMessageResult, error) {
	return f.sendResp, f.sendErr
}

func (f *fakeSDKTransport) GetTask(ctx context.Context, q *a2acore.TaskQueryParams) (*a2acore.Task, error) {
	return f.taskResp, f.taskErr
}

func (f *fakeSDKTransport) Destroy() error {
	return f.destroyErr
}

func TestNewGRPCTransportDialsLazilyAndCloses(t *testing.T) {
	tr, err := newGRPCTransport("dns:///agent.example.invalid:443")
	if err != nil {
		t.Fatalf("newGRPCTransport: %v", err)
	}
	if tr == nil {
		t.Fatal("newGRPCTransport returned a nil transport")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestGRPCTransportSendReturnsTaskID(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{sendResp: &a2acore.Task{ID: "task-9"}}}
	mapped := a2a.Mapped{Part: a2a.Part{Data: json.RawMessage(`{"a":1}`)}, ContextID: "ctx-1"}
	id, err := g.Send(context.Background(), mapped)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "task-9" {
		t.Fatalf("Send task id = %q, want task-9", id)
	}
}

func TestGRPCTransportSendRejectsInvalidData(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{}}
	mapped := a2a.Mapped{Part: a2a.Part{Data: json.RawMessage(`not json`)}}
	if _, err := g.Send(context.Background(), mapped); err == nil {
		t.Fatal("Send accepted malformed data")
	}
}

func TestGRPCTransportSendRejectsTransportFailure(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{sendErr: errors.New("unavailable")}}
	mapped := a2a.Mapped{Part: a2a.Part{Data: json.RawMessage(`{}`)}}
	if _, err := g.Send(context.Background(), mapped); err == nil {
		t.Fatal("Send accepted a transport failure")
	}
}

func TestGRPCTransportSendRejectsNonTaskResult(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{sendResp: &a2acore.Message{ID: "m1"}}}
	mapped := a2a.Mapped{Part: a2a.Part{Data: json.RawMessage(`{}`)}}
	_, err := g.Send(context.Background(), mapped)
	if err == nil {
		t.Fatal("Send accepted a non-task result")
	}
	if !errors.Is(err, ErrNoTask) {
		t.Fatalf("Send error = %v, want errors.Is ErrNoTask", err)
	}
}

func TestGRPCTransportStateMapsEachTaskState(t *testing.T) {
	cases := map[a2acore.TaskState]State{
		a2acore.TaskStateUnspecified:   StateUnspecified,
		a2acore.TaskStateSubmitted:     StateSubmitted,
		a2acore.TaskStateWorking:       StateWorking,
		a2acore.TaskStateCompleted:     StateCompleted,
		a2acore.TaskStateFailed:        StateFailed,
		a2acore.TaskStateCanceled:      StateCanceled,
		a2acore.TaskStateRejected:      StateRejected,
		a2acore.TaskStateAuthRequired:  StateAuthRequired,
		a2acore.TaskStateInputRequired: StateInputRequired,
		a2acore.TaskStateUnknown:       StateUnknown,
	}
	for ts, want := range cases {
		g := &grpcTransport{tr: &fakeSDKTransport{taskResp: &a2acore.Task{Status: a2acore.TaskStatus{State: ts}}}}
		got, err := g.State(context.Background(), "task-1")
		if err != nil {
			t.Fatalf("State(%s): %v", ts, err)
		}
		if got != want {
			t.Fatalf("State(%s) = %s, want %s", ts, got, want)
		}
	}
}

func TestGRPCTransportStatePropagatesFailure(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{taskErr: errors.New("not found")}}
	if _, err := g.State(context.Background(), "task-1"); err == nil {
		t.Fatal("State accepted a transport failure")
	}
}

func TestGRPCTransportResultFromStatusMessage(t *testing.T) {
	task := &a2acore.Task{
		ContextID: "ctx-1",
		Status: a2acore.TaskStatus{
			Message: &a2acore.Message{
				ID:    "msg-out",
				Parts: a2acore.ContentParts{a2acore.DataPart{Data: map[string]any{"payload": "hi"}}},
			},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, err := g.Result(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if mapped.MessageID != "msg-out" || mapped.ContextID != "ctx-1" {
		t.Fatalf("Result mapped = %+v", mapped)
	}
}

func TestGRPCTransportResultFromHistoryFallback(t *testing.T) {
	task := &a2acore.Task{
		ContextID: "ctx-2",
		History: []*a2acore.Message{
			{ID: "msg-1", Parts: a2acore.ContentParts{a2acore.TextPart{Text: "no data part"}}},
			{ID: "msg-2", Parts: a2acore.ContentParts{a2acore.DataPart{Data: map[string]any{"payload": "bye"}}}},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, err := g.Result(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if mapped.MessageID != "msg-2" {
		t.Fatalf("Result used message %q, want msg-2", mapped.MessageID)
	}
}

func TestGRPCTransportResultRejectsNoMessage(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: &a2acore.Task{ContextID: "ctx-3"}}}
	_, err := g.Result(context.Background(), "task-1")
	if err == nil {
		t.Fatal("Result accepted a task with no history and no status message")
	}
	if !errors.Is(err, ErrNoResultMessage) {
		t.Fatalf("Result error = %v, want errors.Is ErrNoResultMessage", err)
	}
}

func TestGRPCTransportResultRejectsNoDataPart(t *testing.T) {
	task := &a2acore.Task{
		ContextID: "ctx-4",
		Status: a2acore.TaskStatus{
			Message: &a2acore.Message{ID: "msg-1", Parts: a2acore.ContentParts{a2acore.TextPart{Text: "no data"}}},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	_, err := g.Result(context.Background(), "task-1")
	if err == nil {
		t.Fatal("Result accepted a message with no data part")
	}
	if !errors.Is(err, ErrNoDataPart) {
		t.Fatalf("Result error = %v, want errors.Is ErrNoDataPart", err)
	}
}

func TestGRPCTransportResultPropagatesFailure(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{taskErr: errors.New("unavailable")}}
	if _, err := g.Result(context.Background(), "task-1"); err == nil {
		t.Fatal("Result accepted a transport failure")
	}
}

func TestGRPCTransportCloseForwardsToDestroy(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{destroyErr: errors.New("close failed")}}
	if err := g.Close(); err == nil {
		t.Fatal("Close swallowed the transport's Destroy error")
	}
}

// TestStateTerminalMatchesUpstream proves the terminal set equals
// a2a-go's own TaskState.Terminal for every upstream constant.
func TestStateTerminalMatchesUpstream(t *testing.T) {
	upstream := []a2acore.TaskState{
		a2acore.TaskStateUnspecified,
		a2acore.TaskStateSubmitted,
		a2acore.TaskStateWorking,
		a2acore.TaskStateCompleted,
		a2acore.TaskStateFailed,
		a2acore.TaskStateCanceled,
		a2acore.TaskStateRejected,
		a2acore.TaskStateAuthRequired,
		a2acore.TaskStateInputRequired,
		a2acore.TaskStateUnknown,
	}
	for _, ts := range upstream {
		got := stateFromTaskState(ts).terminal()
		if want := ts.Terminal(); got != want {
			t.Fatalf("terminal(%s) = %t, want %t: the terminal set must equal upstream", ts, got, want)
		}
	}
}
