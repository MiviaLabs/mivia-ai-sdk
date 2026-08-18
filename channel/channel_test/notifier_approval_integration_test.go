// Package channel_test also holds the tool-approval integration test.
// It wires the shipped NewNDJSONNotifier into
// tools.ScopeOptions.Approve, closing the gap
// notifier_integration_test.go left open for that field. See
// docs/plans/agents/phase47_concurrency_integration_suite.md.
package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// approvalToolName is the registration name of the write-class tool
// the approval scope gates.
const approvalToolName = "deploy"

// approvalRecipient is the fixed identity string the adapter closure
// supplies for Question.Recipient, a field tools.ToolCall lacks.
const approvalRecipient = "operator-console"

// deployTool is a write-class tool. It counts every Run call, so a
// declined approval proves the gate blocked the call before the
// registry reached the tool.
type deployTool struct {
	calls atomic.Int64
}

// Name identifies this tool as approvalToolName in a tools.Registry.
func (d *deployTool) Name() string { return approvalToolName }

// ExecutionProfile reports the write class, so a scope with a write
// approval threshold gates every call.
func (d *deployTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite, ResourceKey: approvalToolName}
}

// Run counts the call and echoes its input.
func (d *deployTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	d.calls.Add(1)
	return tools.Out{Value: in.Value}, nil
}

// approveOverNDJSON adapts an NDJSON-backed channel.Notifier to
// tools.ScopeOptions.Approve's exact (bool, error) signature. It
// compiles only if the two shapes really compose, which is the point
// of this test.
func approveOverNDJSON(notify channel.Notifier) func(context.Context, tools.ToolCall) (bool, error) {
	return func(ctx context.Context, call tools.ToolCall) (bool, error) {
		payload, ok := call.In.Value.(string)
		if !ok {
			return false, errors.New("approve: tool call input is not a string")
		}
		q := channel.Question{
			ID:        "approve-" + call.Name,
			Recipient: approvalRecipient,
			Payload:   payload,
		}
		if err := q.Validate(); err != nil {
			return false, err
		}
		ans, err := notify(ctx, q)
		if err != nil {
			return false, err
		}
		return ans.Approved, nil
	}
}

// approvalPeer runs the far side of the NDJSON wire. It reads each
// question line and writes an answer line, approving only when the
// payload lacks declineWord. reads counts every question it consumed,
// proving both answers crossed the real wire.
func approvalPeer(qr io.Reader, aw io.Writer, declineWord string, reads *atomic.Int64, done chan<- struct{}) {
	defer close(done)
	sc := bufio.NewScanner(qr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(aw)
	for sc.Scan() {
		var q ndjsonLine
		if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
			return
		}
		reads.Add(1)
		reply := ndjsonLine{
			Type:       "answer",
			QuestionID: q.ID,
			Approved:   !strings.Contains(q.Payload, declineWord),
			Payload:    "operator answered " + q.ID,
		}
		if err := enc.Encode(reply); err != nil {
			return
		}
	}
}

// TestNDJSONNotifierGatesToolApproval proves the shipped NDJSON
// transport drives tools.ScopeOptions.Approve end to end: an
// approving answer runs the tool, a declining answer returns
// ErrToolDeclined and never reaches the tool, and both answers
// crossed the real wire.
//
// The calls run sequentially. NewNDJSONNotifier is single-flight and
// returns ErrNotifierBusy to a concurrent second caller, so a
// parallel variant would prove nothing about the approval gate.
func TestNDJSONNotifierGatesToolApproval(t *testing.T) {
	qr, qw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, qw)

	var reads atomic.Int64
	peerDone := make(chan struct{})
	go approvalPeer(qr, aw, "rollback", &reads, peerDone)
	t.Cleanup(func() {
		_ = qr.Close()
		_ = aw.Close()
		<-peerDone
	})

	tool := &deployTool{}
	registry := tools.New()
	if err := registry.Add(tool); err != nil {
		t.Fatalf("tools.Registry.Add() unexpected error: %v", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist:         []string{approvalToolName},
		Approve:           approveOverNDJSON(notify),
		ApprovalThreshold: tools.ExecutionClassWrite,
	})

	out, err := registry.RunScoped(context.Background(), approvalToolName,
		tools.InOut{Value: "ship release 42"}, scope)
	if err != nil {
		t.Fatalf("RunScoped() unexpected error on an approved call: %v", err)
	}
	if got, ok := out.Value.(string); !ok || got != "ship release 42" {
		t.Fatalf("RunScoped() result = %v, want %q", out.Value, "ship release 42")
	}
	if got := tool.calls.Load(); got != 1 {
		t.Fatalf("tool ran %d times after one approved call, want 1", got)
	}

	_, err = registry.RunScoped(context.Background(), approvalToolName,
		tools.InOut{Value: "rollback release 41"}, scope)
	if !errors.Is(err, tools.ErrToolDeclined) {
		t.Fatalf("RunScoped() error = %v, want errors.Is match for tools.ErrToolDeclined", err)
	}
	if got := tool.calls.Load(); got != 1 {
		t.Fatalf("tool ran %d times after a declined call, want it to stay at 1", got)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("peer read %d questions, want 2: both answers must cross the real wire", got)
	}
}
