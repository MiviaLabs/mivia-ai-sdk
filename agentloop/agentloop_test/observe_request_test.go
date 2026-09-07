package agentloop_test

// ObserveRequest hook tests: the observer runs after reserveWork and
// before every Completer.Chat call, including the prompt-too-long
// recovery retry's call. A non-nil error fails the iteration before
// the call runs and refunds the never-consumed reservation with zero
// Usage, on both the primary and the recovery route.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// errObserve is the sentinel the observer fixtures return.
var errObserve = errors.New("host: observe rejected the request")

// observeLog records every ObserveRequest call with its request.
// errAt is the 1-based call number that fails with err; zero never
// fails.
type observeLog struct {
	mu    sync.Mutex
	calls int
	reqs  []provider.Request
	errAt int
	err   error
}

func (o *observeLog) hook(ctx context.Context, req provider.Request) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.reqs = append(o.reqs, req)
	if o.errAt == o.calls {
		return o.err
	}
	return nil
}

func (o *observeLog) stats() (int, []provider.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls, append([]provider.Request(nil), o.reqs...)
}

// newObserveFixture wires one Loop with an ObserveRequest hook and a
// working summarizer, so the recovery path can compact before the
// retry's observer call. errAt is the 1-based observer call that
// fails; zero never fails.
func newObserveFixture(t *testing.T, w plan.Window, errs []error, responses []provider.Response, errAt int, budget *agentloop.WorkBudget) (*agentloop.Loop, *scriptedCompleter, *observeLog) {
	t.Helper()
	reg := tools.New()
	reg.Add(&schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`)})
	sc := &scriptedCompleter{errs: errs, responses: responses}
	sum := &summaryScript{}
	summarizer, err := plan.NewSummarizer(sum)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}
	log := &observeLog{errAt: errAt, err: errObserve}
	loop, err := agentloop.New(agentloop.Options{
		Completer:      sc,
		Tools:          reg,
		Bounds:         agentloop.Bounds{MaxIterations: 4},
		Window:         &w,
		Summarizer:     summarizer,
		Calibrated:     plan.Calibrate(scaleEstimator{div: 1}, 1.0),
		ObserveRequest: log.hook,
		WorkBudget:     budget,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop, sc, log
}

// TestObserveRequestErrorFailsBeforeChat proves an observer error
// fails the iteration before the Completer call runs, on both routes.
// Primary: a hard fail with zero iterations and no Completer call.
// Recovery: the error returns through the fromRecovery route, so the
// pre-failure Result travels with it. With WorkBudget wired, Refund
// saw zero Usage on both routes.
func TestObserveRequestErrorFailsBeforeChat(t *testing.T) {
	t.Run("primary route hard-fails before chat", func(t *testing.T) {
		completer := &scriptedCompleter{responses: []provider.Response{
			{Message: provider.Message{Role: provider.RoleAssistant, Content: "never"}},
		}}
		reg := tools.New()
		mustAdd(t, reg, &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"})
		log := &budgetLog{}
		obs := &observeLog{errAt: 1, err: errObserve}
		loop, err := agentloop.New(agentloop.Options{
			Completer:      completer,
			Tools:          reg,
			ObserveRequest: obs.hook,
			WorkBudget:     log.hook(),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
		if !errors.Is(err, errObserve) {
			t.Fatalf("Run() error = %v, want errors.Is errObserve", err)
		}
		if !strings.Contains(err.Error(), "agentloop: iteration 1: observe request:") {
			t.Fatalf("Run() error = %v, want the iteration count and the observe-request wrap", err)
		}
		if got := completer.callCount(); got != 0 {
			t.Fatalf("completer calls = %d, want 0: the observer error must fail before the call", got)
		}
		if !isZeroResult(res) {
			t.Fatalf("Result = %+v, want the zero Result: a primary-route observer error hard-fails", res)
		}
		if len(log.events) != 2 || log.events[0] != "reserve" || log.events[1] != "refund" {
			t.Fatalf("events = %v, want [reserve refund]", log.events)
		}
		if len(log.usages) != 1 || log.usages[0] != (provider.Usage{}) {
			t.Fatalf("refund usage = %+v, want zero Usage: the call never consumed the reservation", log.usages)
		}
	})

	t.Run("recovery route returns through fromRecovery", func(t *testing.T) {
		big := strings.Repeat("o", 2000)
		msgs := []provider.Message{
			{Role: provider.RoleSystem, Content: "s"},
			{Role: provider.RoleUser, Content: big},
			{Role: provider.RoleUser, Content: "l"},
		}
		w := plan.Window{MaxTokens: 4000, Compaction: plan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
		log := &budgetLog{}
		// errAt 2: the first observed request is the primary one, the
		// second is the recovery retry, which fails.
		loop, sc, obs := newObserveFixture(t, w,
			[]error{provider.ErrPromptTooLong}, []provider.Response{provider.Response{}},
			2, log.hook())
		res, err := loop.Run(context.Background(), msgs)
		if !errors.Is(err, errObserve) {
			t.Fatalf("Run() error = %v, want errors.Is errObserve", err)
		}
		if !strings.Contains(err.Error(), "agentloop: iteration 1: observe request:") {
			t.Fatalf("Run() error = %v, want the count the adjacent reserveWork received", err)
		}
		if got := sc.callCount(); got != 1 {
			t.Fatalf("completer calls = %d, want 1: only the rejected call ran, never the retry", got)
		}
		if got, _ := obs.stats(); got != 2 {
			t.Fatalf("observer calls = %d, want 2: primary request, then the retry request", got)
		}
		if res.Iterations != 0 || len(res.History) != len(msgs) {
			t.Fatalf("Result = {History: %d msgs, Iterations: %d}, want {%d, 0}: the fromRecovery route carries the pre-failure Result, not hardFail's zero value", len(res.History), res.Iterations, len(msgs))
		}
		if len(log.events) != 4 || log.events[1] != "refund" || log.events[3] != "refund" {
			t.Fatalf("events = %v, want [reserve refund reserve refund]", log.events)
		}
		if len(log.usages) != 2 || log.usages[0] != (provider.Usage{}) || log.usages[1] != (provider.Usage{}) {
			t.Fatalf("refund usages = %+v, want zero Usage on both routes", log.usages)
		}
	})
}

// TestObserveRequestSeesRetryRequest proves the observer is called
// once per Completer call: after one ErrPromptTooLong rejection, the
// second observed request carries the rebuilt recovery history, not
// the original one.
func TestObserveRequestSeesRetryRequest(t *testing.T) {
	big := strings.Repeat("o", 2000)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "s"},
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleUser, Content: "l"},
	}
	w := plan.Window{MaxTokens: 4000, Compaction: plan.Compaction{TriggerPercent: 90, TargetPercent: 5}}
	loop, sc, obs := newObserveFixture(t, w,
		[]error{provider.ErrPromptTooLong},
		[]provider.Response{provider.Response{}, {Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}}},
		0, nil)
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	calls, reqs := obs.stats()
	if calls != 2 {
		t.Fatalf("observer calls = %d, want 2: one per Completer call, including the retry", calls)
	}
	if len(reqs[0].Messages) != len(msgs) {
		t.Fatalf("first observed request = %d messages, want %d", len(reqs[0].Messages), len(msgs))
	}
	retried := reqs[1].Messages
	if got := summaryNamed(retried); got != 1 {
		t.Fatalf("second observed request summary count = %d, want 1: the rebuilt history", got)
	}
	foundNotice := false
	for _, m := range retried {
		if m.Content == agentloop.CompactionNotice {
			foundNotice = true
		}
		if strings.Contains(m.Content, big) {
			t.Fatalf("second observed request still carries the dropped %d-byte message", len(big))
		}
	}
	if !foundNotice {
		t.Fatalf("second observed request missing the compaction notice: %+v", retried)
	}
	_, creqs := completerRequests(sc)
	if len(creqs[1].Messages) != len(retried) {
		t.Fatalf("observed retry = %d messages, completer saw %d", len(retried), len(creqs[1].Messages))
	}
}
