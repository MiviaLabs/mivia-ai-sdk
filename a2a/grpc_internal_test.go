package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	a2asdk "github.com/a2aproject/a2a-go/a2aclient"
	"google.golang.org/grpc/credentials/insecure"
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
	tr, err := newGRPCTransport("dns:///agent.example.invalid:443", insecure.NewCredentials())
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
	mapped := Mapped{Part: Part{Text: "{}"}, ContextID: "ctx-1"}
	id, err := g.Send(context.Background(), mapped)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "task-9" {
		t.Fatalf("Send task id = %q, want task-9", id)
	}
}

func TestGRPCTransportSendRejectsTransportFailure(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{sendErr: errors.New("unavailable")}}
	mapped := Mapped{Part: Part{Text: "{}"}}
	if _, err := g.Send(context.Background(), mapped); err == nil {
		t.Fatal("Send accepted a transport failure")
	}
}

func TestGRPCTransportSendRejectsNonTaskResult(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{sendResp: &a2acore.Message{ID: "m1"}}}
	mapped := Mapped{Part: Part{Text: "{}"}}
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
				Parts: a2acore.ContentParts{a2acore.TextPart{Text: "{}"}},
			},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, _, err := g.Result(context.Background(), "task-1")
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
			{ID: "msg-1", Parts: a2acore.ContentParts{a2acore.TextPart{Text: "earlier"}}},
			{ID: "msg-2", Parts: a2acore.ContentParts{a2acore.TextPart{Text: "{}"}}},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, _, err := g.Result(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if mapped.MessageID != "msg-2" {
		t.Fatalf("Result used message %q, want msg-2", mapped.MessageID)
	}
}

// TestGRPCTransportResultReadsTextPart proves Result maps the first
// TextPart into Part.Text and leaves Part.Data empty, even when a
// DataPart precedes it: Text wins at the transport layer. FromPart
// then decodes the envelope.
func TestGRPCTransportResultReadsTextPart(t *testing.T) {
	signed := signedMessage(t)
	mappedIn, err := ToPart(signed)
	if err != nil {
		t.Fatalf("ToPart: %v", err)
	}
	task := &a2acore.Task{
		ContextID: "ctx-1",
		Status: a2acore.TaskStatus{
			State: a2acore.TaskStateCompleted,
			Message: &a2acore.Message{
				ID: "msg-out",
				Parts: a2acore.ContentParts{
					a2acore.DataPart{Data: map[string]any{"payload": "decoy"}},
					a2acore.TextPart{Text: mappedIn.Part.Text},
				},
			},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, state, err := g.Result(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if state != StateCompleted {
		t.Fatalf("Result state = %s, want %s", state, StateCompleted)
	}
	if mapped.Part.Text == "" {
		t.Fatal("Part.Text is empty, want the TextPart's bytes")
	}
	if mapped.Part.Text != mappedIn.Part.Text {
		t.Fatal("Part.Text differs from the encoded envelope bytes")
	}
	if len(mapped.Part.Data) != 0 {
		t.Fatal("Part.Data is set, want empty: Text wins over Data")
	}
	got, err := FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if got.Payload != signed.Payload {
		t.Fatalf("FromPart payload = %q, want %q", got.Payload, signed.Payload)
	}
}

// TestGRPCTransportResultReadsDataPartFallback proves a DataPart-only
// message maps into Part.Data, so FromPart's fallback decodes an old
// peer. The fallback ships for one release and dies in v0.4.0.
func TestGRPCTransportResultReadsDataPartFallback(t *testing.T) {
	signed := signedMessage(t)
	raw, err := signed.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	task := &a2acore.Task{
		ContextID: "ctx-1",
		Status: a2acore.TaskStatus{
			Message: &a2acore.Message{
				ID:    "msg-out",
				Parts: a2acore.ContentParts{a2acore.DataPart{Data: decoded}},
			},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	mapped, _, err := g.Result(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if mapped.Part.Text != "" {
		t.Fatal("Part.Text is set, want empty")
	}
	if len(mapped.Part.Data) == 0 {
		t.Fatal("Part.Data is empty, want the re-marshaled data")
	}
	got, err := FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart on the Data fallback: %v", err)
	}
	if got.Payload != signed.Payload {
		t.Fatalf("FromPart payload = %q, want %q", got.Payload, signed.Payload)
	}
}

func TestGRPCTransportResultRejectsNoMessage(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: &a2acore.Task{ContextID: "ctx-3"}}}
	_, _, err := g.Result(context.Background(), "task-1")
	if err == nil {
		t.Fatal("Result accepted a task with no history and no status message")
	}
	if !errors.Is(err, ErrNoResultMessage) {
		t.Fatalf("Result error = %v, want errors.Is ErrNoResultMessage", err)
	}
}

// TestGRPCTransportResultRejectsNoTextPart proves a result message
// with no parts at all fails with ErrNoTextPart: Result requires a
// text part.
func TestGRPCTransportResultRejectsNoTextPart(t *testing.T) {
	task := &a2acore.Task{
		ContextID: "ctx-4",
		Status: a2acore.TaskStatus{
			Message: &a2acore.Message{ID: "msg-1"},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	_, _, err := g.Result(context.Background(), "task-1")
	if err == nil {
		t.Fatal("Result accepted a message with no text part")
	}
	if !errors.Is(err, ErrNoTextPart) {
		t.Fatalf("Result error = %v, want errors.Is ErrNoTextPart", err)
	}
}

// TestGRPCTransportResultRejectsFilePartOnly proves a part that is
// neither a TextPart nor a DataPart counts as no usable part: a
// FilePart-only message fails with ErrNoTextPart instead of mapping a
// degenerate data part.
func TestGRPCTransportResultRejectsFilePartOnly(t *testing.T) {
	task := &a2acore.Task{
		ContextID: "ctx-5",
		Status: a2acore.TaskStatus{
			Message: &a2acore.Message{
				ID:    "msg-1",
				Parts: a2acore.ContentParts{a2acore.FilePart{}},
			},
		},
	}
	g := &grpcTransport{tr: &fakeSDKTransport{taskResp: task}}
	_, _, err := g.Result(context.Background(), "task-1")
	if err == nil {
		t.Fatal("Result accepted a file-part-only message")
	}
	if !errors.Is(err, ErrNoTextPart) {
		t.Fatalf("Result error = %v, want errors.Is ErrNoTextPart", err)
	}
}

func TestGRPCTransportResultPropagatesFailure(t *testing.T) {
	g := &grpcTransport{tr: &fakeSDKTransport{taskErr: errors.New("unavailable")}}
	if _, _, err := g.Result(context.Background(), "task-1"); err == nil {
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
