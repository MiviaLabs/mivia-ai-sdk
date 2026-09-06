package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

type scaleEstimator struct{ div int }

func (e scaleEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total / e.div, nil
}

type echoTool struct {
	name   string
	schema []byte
	result string
}

func (t *echoTool) Name() string { return t.name }

func (t *echoTool) ParameterSchema() []byte { return t.schema }

func (t *echoTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (t *echoTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: t.result}, nil
}

type fixedSummaryCompleter struct{}

func (fixedSummaryCompleter) Name() string { return "fixed-summary" }

func (fixedSummaryCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	summaryJSON := `{"Objective":"Ship","State":"Compacted","Decisions":["d1"],"OpenWork":["w1"],"Risks":["r1"]}`
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: summaryJSON},
	}, nil
}

func (fixedSummaryCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, fmt.Errorf("ChatStream not supported")
}

func newAnthropicFixtureServer(t *testing.T) *httptest.Server {
	var turns int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn := atomic.AddInt32(&turns, 1)
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_turn1",
				"type":        "message",
				"role":        "assistant",
				"model":       "claude-opus-5",
				"stop_reason": "tool_use",
				"content": []map[string]any{
					{"type": "tool_use", "id": "call_1", "name": "search", "input": map[string]any{"q": "test"}},
				},
				"usage": map[string]any{"input_tokens": 200, "output_tokens": 80},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_turn2",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-opus-5",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "final answer"}},
			"usage":       map[string]any{"input_tokens": 150, "output_tokens": 10},
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func buildControlLoop(t *testing.T, serverURL string, client *http.Client) *agentloop.Loop {
	anthropicClient, err := anthropic.New(anthropic.Options{
		APIKey:     "test-api-key",
		BaseURL:    serverURL,
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("anthropic.New: %v", err)
	}

	reg := tools.New()
	if err := reg.Add(&echoTool{name: "search", schema: []byte(`{"type":"object"}`), result: strings.Repeat("r", 20)}); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	summarizer, err := contextsummary.NewSummarizer(fixedSummaryCompleter{})
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}

	window := &contextplan.Window{
		MaxTokens: 400,
		Reserve:   100,
		Compaction: contextplan.Compaction{
			TriggerPercent: 80,
			TargetPercent:  20,
		},
	}

	loop, err := agentloop.New(agentloop.Options{
		Completer:     anthropicClient,
		Tools:         reg,
		MaxIterations: 5,
		Window:        window,
		Summarizer:    summarizer,
		Calibrated:    contextplan.Calibrate(scaleEstimator{div: 1}, 1.0),
	})
	if err != nil {
		t.Fatalf("agentloop.New: %v", err)
	}
	return loop
}

func TestAnthropicAgentLoopCompactionControl(t *testing.T) {
	ts := newAnthropicFixtureServer(t)
	loop := buildControlLoop(t, ts.URL, ts.Client())

	initialMsgs := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("o", 50)},
		{Role: provider.RoleUser, Content: strings.Repeat("m", 100)},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 50)},
	}

	res, err := loop.Run(context.Background(), initialMsgs)
	if err != nil {
		t.Fatalf("loop.Run: %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Errorf("res.Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if res.Iterations != 2 {
		t.Errorf("res.Iterations = %d, want 2", res.Iterations)
	}

	foundSummary := false
	for _, m := range res.History {
		if m.Name == contextsummary.SummaryMessageName {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Errorf("history lacks injected summary message: %+v", res.History)
	}
}
