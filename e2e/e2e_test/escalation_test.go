package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// wireQuestion mirrors the transport's question line; wireAnswer
// mirrors its answer line. The transport owns these shapes.
type wireQuestion struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Payload   string `json:"payload"`
}

type wireAnswer struct {
	Type       string `json:"type"`
	QuestionID string `json:"question_id"`
	Approved   bool   `json:"approved"`
	Payload    string `json:"payload"`
}

// pipeHuman wires one Ask round trip over an in-memory NDJSON pair.
// It returns the Notifier for Options.Ask and serves exactly one
// question: it reads the question line, checks it, then writes the
// fixed answer.
func pipeHuman(t *testing.T, approve bool, payload string) channel.Notifier {
	t.Helper()
	questionRead, questionWrite := io.Pipe()
	answerRead, answerWrite := io.Pipe()
	go func() {
		defer questionWrite.Close()
		defer answerWrite.Close()
		line, err := bufio.NewReader(questionRead).ReadString('\n')
		if err != nil {
			t.Errorf("human read: %v", err)
			return
		}
		var q wireQuestion
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Errorf("human decode: %v", err)
			return
		}
		if q.Type != "question" || q.ID != "decide" ||
			q.Recipient != "human-1" || q.Payload != "approve the transfer" {
			t.Errorf("question line = %+v, want the decide step for human-1", q)
			return
		}
		ans, err := json.Marshal(wireAnswer{
			Type: "answer", QuestionID: q.ID,
			Approved: approve, Payload: payload,
		})
		if err != nil {
			t.Errorf("human encode: %v", err)
			return
		}
		if _, err := answerWrite.Write(append(ans, '\n')); err != nil {
			t.Errorf("human write: %v", err)
		}
	}()
	return channel.NewNDJSONNotifier(answerRead, questionWrite)
}

// TestEscalationRoundTripOverChannel proves an escalated tool error
// routes through a real NDJSON transport to a human, and the
// approved payload becomes the ack restatement.
func TestEscalationRoundTripOverChannel(t *testing.T) {
	ctx := context.Background()
	plan, m := decidePlanMachine(t)
	reg := tools.New()
	if err := reg.Add(e2e.EscalateTool{ToolName: "decide"}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent:   e2eAgent(t, "escalation-agent", plan),
		Machine: m,
		Tools:   reg,
		Ask:     pipeHuman(t, true, "human approved"),
		AskTo:   "human-1",
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-escalate", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "decided" {
		t.Fatalf("status = %q, want %q", status, "decided")
	}
}

// TestEscalationDeclinedFailsRun proves a declined answer fails the
// run with an error naming the step and the human.
func TestEscalationDeclinedFailsRun(t *testing.T) {
	ctx := context.Background()
	plan, m := decidePlanMachine(t)
	reg := tools.New()
	if err := reg.Add(e2e.EscalateTool{ToolName: "decide"}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent:   e2eAgent(t, "escalation-agent", plan),
		Machine: m,
		Tools:   reg,
		Ask:     pipeHuman(t, false, ""),
		AskTo:   "human-1",
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	_, _, err = runner.Run(ctx, "thread-decline", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want a decline failure")
	}
	if !strings.Contains(err.Error(), "declined") || !strings.Contains(err.Error(), "human-1") {
		t.Fatalf("Run error %q lacks the decline and the human", err)
	}
}

// decidePlanMachine returns the one-step plan and machine both
// escalation scenarios share. The step payload rides the question
// to the human.
func decidePlanMachine(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "decide", To: "decided", Payload: "approve the transfer"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "decided", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return plan, m
}
